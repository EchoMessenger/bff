.PHONY: build run docker-build test lint clean

BIN_DIR := bin
BIN_NAME := bff
OUTPUT := $(BIN_DIR)/$(BIN_NAME)

build:
	mkdir -p $(BIN_DIR)
	go build -o $(OUTPUT) ./cmd/bff

run: build
	./ $(OUTPUT)

docker-build:
	docker build -t ghcr.io/echomessenger/bff:latest .

test:
	go test -v ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found, skipping"; exit 0; }
	golangci-lint run ./...

clean:
	rm -rf $(BIN_DIR)
	go clean
