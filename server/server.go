package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	CRLF        = "\r\n"
	OkStatus    = "200 OK"
	HTTPVersion = "HTTP/1.1 "
)

type HTTPRequest struct {
	Type          string
	Dir           string
	CreateDir     bool
	RemoveDir     bool
	IsWrongDomain bool
	BodyLen       int
	Body          []byte
}

type RawResponse struct {
	body     []byte
	mimeType string
	status   string
}

type HTTPServer struct {
	serverAddress    string
	socket           net.Listener
	serverDomain     string
	workingDirectory string
}

func (s *HTTPServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	clientAddress := conn.RemoteAddr().String()
	fmt.Printf("Handle connection from %s\n", clientAddress)

	req := s.parseRequest(conn)
	rawResp := s.createRawResponse(req)
	resp := s.createResponse(*rawResp)
	s.sendResponse(conn, resp)
}

// Parse request to HTTPRequest
func (s *HTTPServer) parseRequest(conn net.Conn) *HTTPRequest {
	req := new(HTTPRequest)
	reader := bufio.NewReader(conn)

	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, CRLF)
	fmt.Printf("Got line: \"%s\"\n", line)

	splitLine := strings.Split(line, " ")
	req.Type = splitLine[0]
	req.Dir = splitLine[1]

	for {
		line, _ = reader.ReadString('\n')
		line = strings.TrimRight(line, CRLF)

		if line == "" {
			break
		}

		s.parseReqHeader(line, req)

		fmt.Printf("Got line: \"%s\"\n", line)
	}

	req.Body = make([]byte, req.BodyLen)
	_, err := io.ReadFull(reader, req.Body)
	if err != nil {
		fmt.Printf("Error while reading body %v\n", err)
	}

	return req
}

func (s *HTTPServer) parseReqHeader(line string, req *HTTPRequest) {
	splitLine := strings.SplitN(line, " ", 2) // handling headers
	switch splitLine[0] {
	case "Content-Length:":
		req.BodyLen, _ = strconv.Atoi(splitLine[1])
	case "Create-Directory:":
		if splitLine[1] == "True" {
			req.CreateDir = true
		}
	case "Remove-Directory:":
		if splitLine[1] == "True" {
			req.RemoveDir = true
		}
	case "Host:":
		if s.serverDomain != "" && s.serverDomain != splitLine[1] {
			req.IsWrongDomain = true
		}
	}
}

// Creating and sending response
func (s *HTTPServer) createRawResponse(req *HTTPRequest) (rawResp *RawResponse) {
	rawResp = new(RawResponse)

	if req.IsWrongDomain {
		rawResp.status = "400 Bad Request"
		return
	}

	switch req.Type {
	case "GET":
		rawResp = s.handleGetRequest(req)
	case "POST":
		rawResp = s.handlePostRequest(req)
	case "PUT":
		rawResp = s.handlePutRequest(req)
	case "DELETE":
		rawResp = s.handleDeleteRequest(req)
	default:
		rawResp.body = []byte{}
	}

	return
}

func (s *HTTPServer) createResponse(rawResp RawResponse) []byte {
	resp := []byte(HTTPVersion)
	if rawResp.status == "" {
		resp = append(resp, []byte(OkStatus)...)
	} else {
		resp = append(resp, []byte(rawResp.status)...)
	}
	resp = append(resp, []byte(CRLF)...)

	resp = append(resp, []byte("Server: HWServer")...)
	resp = append(resp, []byte(CRLF)...)

	resp = append(resp, []byte("Content-Length: ")...)
	resp = append(resp, []byte(strconv.Itoa(len(rawResp.body)))...)
	resp = append(resp, []byte(CRLF)...)

	resp = append(resp, []byte("Content-Type: ")...)
	resp = append(resp, []byte(rawResp.mimeType)...) // ???
	resp = append(resp, []byte(CRLF)...)

	resp = append(resp, []byte(CRLF)...)
	resp = append(resp, rawResp.body...)

	return resp
}

func (s *HTTPServer) sendResponse(conn net.Conn, resp []byte) {
	_, err := conn.Write(resp)
	if err != nil {
		fmt.Printf("Error writing response: %v\n", err)
		return
	}

	fmt.Println("Response has sent")
}

// Handling requests
func (s *HTTPServer) handleGetRequest(req *HTTPRequest) (rawResp *RawResponse) {
	rawResp = new(RawResponse)
	absolutePath := filepath.Join(s.workingDirectory, req.Dir)

	fi, err := os.Stat(absolutePath)
	if err != nil {
		fmt.Printf("Error while getting file %v\n", err)
		rawResp.status = "404 Not Found"
		rawResp.body = []byte(fmt.Sprintf("File %s not found", absolutePath))
		return
	}

	if fi.Mode().IsDir() {
		cmd := exec.Command("ls", "-l", "-A", "--time-style=+%Y-%m-%d %H:%M:%S", absolutePath)
		rawResp.body, err = cmd.Output()
		if err != nil {
			fmt.Printf("Error execing command for dir: %v\n", err)
			return
		}
		//rawResp.body = []byte(strings.SplitN(string(rawResp.body), "\n", 2)[1])
		return
	}

	rawResp.body, err = os.ReadFile(absolutePath)
	if err != nil {
		fmt.Printf("Error while reading file: %v\n", err)
		return
	}

	rawResp.mimeType = mime.TypeByExtension(filepath.Ext(absolutePath))
	if rawResp.mimeType == "" {
		rawResp.mimeType = "application/octet-stream"
	}

	return
}

func (s *HTTPServer) handlePostRequest(req *HTTPRequest) (rawResp *RawResponse) {
	rawResp = new(RawResponse)
	absolutePath := filepath.Join(s.workingDirectory, req.Dir)

	if _, err := os.Stat(absolutePath); err == nil {
		rawResp.status = "409 Conflict"
		rawResp.body = []byte(fmt.Sprintf("File %s already exists", absolutePath))
		return
	}

	if req.CreateDir {
		err := os.Mkdir(absolutePath, os.ModePerm)
		if err != nil {
			fmt.Printf("Error while creting directory: %v\n", err)
		}
		return
	}

	err := os.WriteFile(absolutePath, req.Body, os.ModePerm)
	if err != nil {
		fmt.Printf("Error while creting file or writing to file: %v\n", err)
	}

	return
}

func (s *HTTPServer) handlePutRequest(req *HTTPRequest) (rawResp *RawResponse) {
	rawResp = new(RawResponse)
	absolutePath := filepath.Join(s.workingDirectory, req.Dir)

	fi, err := os.Stat(absolutePath)
	if err != nil {
		fmt.Printf("Error while putting file %v\n", err)
		rawResp.status = "404 Not Found"
		rawResp.body = []byte(fmt.Sprintf("File %s not found", absolutePath))
		return
	}

	if fi.Mode().IsDir() {
		rawResp.status = "409 Conflict"
		rawResp.body = []byte(fmt.Sprintf("File %s is a directory", absolutePath))
		return
	}

	err = os.WriteFile(absolutePath, req.Body, os.ModePerm)
	if err != nil {
		fmt.Printf("Error while writing to file: %v\n", err)
	}

	return
}

func (s *HTTPServer) handleDeleteRequest(req *HTTPRequest) (rawResp *RawResponse) {
	rawResp = new(RawResponse)
	absolutePath := filepath.Join(s.workingDirectory, req.Dir)

	fi, err := os.Stat(absolutePath)
	if err != nil {
		fmt.Printf("Error while putting file %v\n", err)
		rawResp.status = "404 Not Found"
		rawResp.body = []byte(fmt.Sprintf("File %s not found", absolutePath))
		return
	}

	if fi.Mode().IsDir() {
		if !req.RemoveDir {
			rawResp.status = "406 Not Acceptable"
			rawResp.body = []byte(fmt.Sprintf("File %s is a directory", absolutePath))
			return
		}

		err = os.RemoveAll(absolutePath)
		if err != nil {
			fmt.Printf("Error while removing directory: %v\n", err)
			return
		}

		return
	}

	err = os.Remove(absolutePath)
	if err != nil {
		fmt.Printf("Error while removing directory: %v\n", err)
		return
	}

	return
}

func main() {
	var host, port, serverDomain, workingDirectory string
	// from args
	for i := 1; i < len(os.Args); i++ {
		arg := strings.SplitN(os.Args[i], "=", 2)
		switch arg[0] {
		case "--host":
			host = arg[1]
		case "--port":
			port = arg[1]
		case "--working-directory":
			workingDirectory = arg[1]
		case "--server-domain":
			serverDomain = arg[1]
		default:
			log.Fatalf("Unknown argument: %s\n", os.Args[i])
		}
	}
	// from env
	if host == "" {
		host = os.Getenv("SERVER_HOST")
	}
	if port == "" {
		port = os.Getenv("SERVER_PORT")
	}
	if serverDomain == "" {
		serverDomain = os.Getenv("SERVER_DOMAIN")
	}
	if workingDirectory == "" {
		workingDirectory = os.Getenv("SERVER_WORKING_DIRECTORY")
	}
	// default
	if host == "" {
		host = "0.0.0.0"
	}
	if port == "" {
		port = "8080"
	}
	if workingDirectory == "" {
		log.Fatalf("No argument \"working directory\" got\n")
	}

	serverAddress := fmt.Sprintf("%s:%s", host, port)

	fmt.Printf("Starting server on %s, domain %s, working directory %s\n", serverAddress, serverDomain, workingDirectory)

	serverSocket, err := net.Listen("tcp", serverAddress)
	if err != nil {
		log.Fatalf("Failed to start server: %v\n", err)
	}
	defer serverSocket.Close()

	server := &HTTPServer{
		serverAddress:    serverAddress,
		socket:           serverSocket,
		serverDomain:     serverDomain,
		workingDirectory: workingDirectory,
	}

	fmt.Printf("Listening at %s\n", serverAddress)

	for {
		conn, err := serverSocket.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v\n", err)
			continue
		}

		server.handleConnection(conn)
	}
}
