# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gosei ./cmd/gosei

# Runtime stage
FROM alpine:3.19

# Install docker CLI for compose operations
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose

WORKDIR /app

# Copy binary from builder
COPY --from=builder /gosei /app/gosei

# Default configuration
ENV GOSEI_HOST=0.0.0.0
ENV GOSEI_PORT=8080
ENV GOSEI_PROJECTS_DIR=/projects

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/api/system/health || exit 1

ENTRYPOINT ["/app/gosei"]
