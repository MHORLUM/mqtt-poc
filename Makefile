APP_NAME=mqttcli

.PHONY: all build run clean client server emqx stop

all: build

build:
	go build -o bin/server ./cmd/server
	go build -o bin/client ./cmd/client

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
