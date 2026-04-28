GOCACHE ?= /tmp/go-build
ADDR ?= :8080
PREFIX ?= $(HOME)/.local
BINARY ?= magazine-builder
BUILD_DIR ?= bin

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
	env GOCACHE=$(GOCACHE) go build -o $(BUILD_DIR)/$(BINARY) .

run:
	env GOCACHE=$(GOCACHE) go run . -addr $(ADDR)

install: build
	mkdir -p $(PREFIX)/bin
	env GOCACHE=$(GOCACHE) go build -o $(PREFIX)/bin/$(BINARY) .
