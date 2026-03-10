# Makefile for nrllink

BINARY_NAME=nrllink
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Platform specific settings
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
    BINARY_NAME=$(BINARY_NAME)_linux
endif
ifeq ($(UNAME_S),Darwin)
    BINARY_NAME=$(BINARY_NAME)_macos
endif

.PHONY: all build clean test help run deps fmt vet

all: fmt vet build

## build: Build the application
build:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/udphub

## build-windows: Build for Windows
build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_windows_x86_64.exe -v ./cmd/udphub

## build-linux: Build for Linux
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_x86_64 -v ./cmd/udphub

## build-arm: Build for ARM (Raspberry Pi)
build-arm:
	GOOS=linux GOARCH=arm $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_arm -v ./cmd/udphub

## build-arm64: Build for ARM64
build-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_arm64 -v ./cmd/udphub

## clean: Clean build files
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)*
	rm -f nrllink*

## test: Run tests
test:
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out

## deps: Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

## fmt: Format code
fmt:
	$(GOCMD) fmt ./...

## vet: Run go vet
vet:
	$(GOCMD) vet ./...

## run: Run the application
run:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/udphub
	./$(BINARY_NAME) -c udphub.yaml

## install: Install the application
install:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/udphub

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
