GOCACHE ?= /tmp/go-build
ADDR ?= :8080
PREFIX ?= $(HOME)/.local
BINARY ?= magazine-builder
BUILD_DIR ?= bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -ldflags "-X main.version=$(VERSION)"

.PHONY: fmt test check build run install

fmt:
	gofmt -w main.go
	npx prettier --write static/app.js static/app.css static/index.html

test:
	env GOCACHE=$(GOCACHE) go test ./...

check: test
	node --check static/app.js

build:
	mkdir -p $(BUILD_DIR)
	env GOCACHE=$(GOCACHE) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .

run:
	env GOCACHE=$(GOCACHE) go run . -addr $(ADDR)

install: build
	mkdir -p $(PREFIX)/bin
	env GOCACHE=$(GOCACHE) go build $(LDFLAGS) -o $(PREFIX)/bin/$(BINARY) .
