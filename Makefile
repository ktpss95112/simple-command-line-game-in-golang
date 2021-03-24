all: server/server client/client-linux

server/server: server/server.go
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server/server server/server.go
	strip server/server

client/client-linux: client/client.go
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o client/client-linux client/client.go
	strip client/client-linux

clean:
	rm -f client/client-linux server/server
