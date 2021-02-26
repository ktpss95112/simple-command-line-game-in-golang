all: server/server client/client-linux

server/server: server/server.go
	CGO_ENABLED=0 go build -o server/server server/server.go

client/client-linux: client/client.go
	CGO_ENABLED=0 go build -o client/client-linux client/client.go

clean:
	rm -f client/client-linux server/server
