APP_NAME=mqttcli

.PHONY: all build run clean client server emqx stop build-server-exe build-client-exe build-exe

all: build

build:
	go build -o bin/server ./cmd/server
	go build -o bin/client ./cmd/client

build-server-exe:
	@echo "Building server as Windows executable..."
	set GOOS=windows&& set GOARCH=amd64&& go build -o bin/server.exe ./cmd/server
	@echo "Server executable created: bin/server.exe"

build-client-exe:
	@echo "Building client as Windows executable..."
	set GOOS=windows&& set GOARCH=amd64&& go build -o bin/client.exe ./cmd/client
	@echo "Client executable created: bin/client.exe"

build-exe: build-server-exe build-client-exe
	@echo "Both executables built successfully!"

run: build
	@echo "Run server: ./bin/server"
	@echo "Run client: ./bin/client"

client:
	go run ./cmd/client

server:
	go run ./cmd/server

emqx:
	docker-compose up -d emqx

stop:
	docker-compose down

clean:
	rm -rf bin
