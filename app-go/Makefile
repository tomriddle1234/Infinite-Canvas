GO ?= go
BINARY ?= infinite-canvas-go

.PHONY: tidy build run test

tidy:
	$(GO) mod tidy

build:
	$(GO) build -o bin/$(BINARY).exe ./cmd/server

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./...
