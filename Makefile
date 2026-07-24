SHELL:=/usr/bin/env bash

# nb. homebrew-releaser assumes the program name is == the repository name
BIN_NAME:=msync
BIN_VERSION:=$(shell ./.version.sh)

default: help
.PHONY: help  # via https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
help: ## Print help
	@grep -E '^[a-zA-Z_/-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: all
all: clean build-darwin-amd64 build-darwin-arm64 ## Build for macOS (amd64, arm64)

.PHONY: clean
clean: ## Remove build products (./out)
	rm -rf ./out

.PHONY: build
build: ## Build for the current platform & architecture to ./out
	mkdir -p out
	env CGO_ENABLED=0 go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME} .

.PHONY: build-darwin-amd64
build-darwin-amd64: ## Build for macOS/amd64 to ./out
	mkdir -p out
	env CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME}-${BIN_VERSION}-darwin-amd64 .

.PHONY: build-darwin-arm64
build-darwin-arm64: ## Build for macOS/arm64 to ./out
	mkdir -p out
	env CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-X main.version=${BIN_VERSION}" -o ./out/${BIN_NAME}-${BIN_VERSION}-darwin-arm64 .

.PHONY: test
test: ## Run the full test suite
	go test -race -v ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: install
install: ## Build & install msync to /usr/local/bin
	env CGO_ENABLED=0 go build -ldflags="-X main.version=${BIN_VERSION}" -o /usr/local/bin/${BIN_NAME} .
