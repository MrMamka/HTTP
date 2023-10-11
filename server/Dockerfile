FROM golang:latest

WORKDIR /http/server

COPY . .

RUN go build -o server server.go

ENTRYPOINT ["./server"]
