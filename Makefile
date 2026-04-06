.DEFAULT_GOAL := help

.PHONY: help clean test build build-local build-linux-amd64 build-darwin-arm64 build-macos release-builds

BINARY := blogwatcher
PACKAGE := ./cmd/blogwatcher
DIST_DIR := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')
LDFLAGS := -s -w -X github.com/rdslw/blogwatcher/internal/version.Version=$(VERSION)

help: ## Show available make targets
	@printf "Available targets:\n"
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

clean: ## Remove generated build artifacts
	rm -rf $(DIST_DIR)
	rm -f ./$(BINARY)

test: ## Run the Go test suite
	go test ./...

build: build-local ## Build for the current machine into dist/

build-local:
	go generate ./internal/skill/
	mkdir -p $(DIST_DIR)
	go build -ldflags='$(LDFLAGS)' -o $(DIST_DIR)/$(BINARY) $(PACKAGE)

build-linux-amd64: ## Build the release Linux amd64 binary into dist/
	go generate ./internal/skill/
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='$(LDFLAGS)' -o $(DIST_DIR)/$(BINARY)-linux-amd64 $(PACKAGE)

build-darwin-arm64: ## Build the release macOS arm64 binary into dist/
	go generate ./internal/skill/
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags='$(LDFLAGS)' -o $(DIST_DIR)/$(BINARY)-darwin-arm64 $(PACKAGE)

build-macos: build-darwin-arm64 ## alias for the MacOS arm64 build

release: build-linux-amd64 build-darwin-arm64 ## Build the release binaries into dist/
