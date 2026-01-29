.PHONY: build run dev clean docker docker-build docker-run test fmt lint

# Version
VERSION ?= 0.1.0

# Binary name
BINARY = gosei

# Build flags
LDFLAGS = -ldflags="-s -w -X main.Version=$(VERSION)"

# Default target
all: build

# Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/gosei

# Run the application
run: build
	./bin/$(BINARY) -projects-dir=$(GOSEI_PROJECTS_DIR)

# Development mode with auto-reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	air

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Build Docker image
docker-build:
	docker build -t gosei:$(VERSION) -t gosei:latest .

# Run with Docker Compose
docker-run:
	docker compose up -d

# Stop Docker Compose
docker-stop:
	docker compose down

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Generate embedded files (if needed)
generate:
	go generate ./...

# Show help
help:
	@echo "Gosei - Docker Compose Management"
	@echo ""
	@echo "Usage:"
	@echo "  make build        Build the binary"
	@echo "  make run          Build and run locally"
	@echo "  make dev          Run with hot-reload (requires air)"
	@echo "  make clean        Clean build artifacts"
	@echo "  make docker-build Build Docker image"
	@echo "  make docker-run   Run with Docker Compose"
	@echo "  make docker-stop  Stop Docker Compose"
	@echo "  make test         Run tests"
	@echo "  make fmt          Format code"
	@echo "  make lint         Lint code"
	@echo "  make deps         Download dependencies"
	@echo ""
	@echo "Environment:"
	@echo "  GOSEI_PROJECTS_DIR  Path to compose projects directory"
	@echo "  GOSEI_HOST          Host to bind to (default: 127.0.0.1)"
	@echo "  GOSEI_PORT          Port to listen on (default: 8080)"
