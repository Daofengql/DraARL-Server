# Makefile for draarl

BINARY_NAME=draarl
VERSION_FILE=$(strip $(shell cat VERSION 2>/dev/null))
VERSION?=$(if $(VERSION_FILE),$(VERSION_FILE),dev)
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X draarl/internal/buildinfo.Version=$(VERSION) -X draarl/internal/buildinfo.BuildTime=$(BUILD_TIME)"

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
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/draarl

## build-windows: Build for Windows
build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_windows_x86_64.exe -v ./cmd/draarl

## build-linux: Build for Linux
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_x86_64 -v ./cmd/draarl

## build-arm: Build for ARM (Raspberry Pi)
build-arm:
	GOOS=linux GOARCH=arm $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_arm -v ./cmd/draarl

## build-arm64: Build for ARM64
build-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)_linux_arm64 -v ./cmd/draarl

## clean: Clean build files
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)*
	rm -f draarl*

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
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/draarl
	./$(BINARY_NAME) -c config.yaml

## install: Install the application
install:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./cmd/draarl

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
