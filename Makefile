BINARY_NAME=goroute
BUILD_DIR=bin
VERSION?=0.1.0
LDFLAGS=-ldflags="-s -w -X main.Version=$(VERSION)"

.PHONY: all build test clean run docker install

all: test build

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/goroute

install:
	go install $(LDFLAGS) ./cmd/goroute

test:
	go test -v -race ./...

bench:
	go test -v -run=TestMemoryAndConcurrency ./tests

run: build
	./$(BUILD_DIR)/$(BINARY_NAME) serve

clean:
	rm -rf $(BUILD_DIR) coverage.txt

docker:
	docker build -t goroute:latest .
