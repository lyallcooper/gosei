# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Run Commands

```bash
# Build binary to bin/gosei
make build

# Build and run locally (set GOSEI_PROJECTS_DIR to compose projects location)
make run GOSEI_PROJECTS_DIR=/path/to/projects

# Run tests
make test

# Build Docker image
make docker-build

# Run via Docker Compose
make docker-run
```

**Configuration**: Environment variables `GOSEI_HOST` (default: 127.0.0.1), `GOSEI_PORT` (default: 8080), `GOSEI_PROJECTS_DIR` (default: .)

## Code Style

**Comments**: Only use comments to explain *why* code does something, not *what* it does. Comments should aid readability and maintainability, not add noise.

## Architecture Overview

Gosei is a Docker Compose management dashboard. It scans a directory for compose files, displays project/container status, and provides controls for compose operations—all with real-time updates via Server-Sent Events.

### Data Flow

```
Filesystem Scan → Project Scanner → In-Memory State
                                         ↓
Docker Daemon ←→ Docker Client ←→ API Handlers → HTTP/SSE → Browser
```

### Key Packages

- **cmd/gosei**: Entry point, server initialization, Docker event watcher
- **internal/docker**: Docker SDK wrapper (`client.go`) and compose CLI executor (`compose.go`)
- **internal/project**: Filesystem scanner that discovers compose.yaml files
- **internal/sse**: Pub-sub broker for real-time event distribution
- **internal/api**: Chi router and HTTP handlers (pages, API, SSE endpoint)
- **web**: Embedded templates and static assets via `//go:embed`

### Design Decisions

1. **Compose operations shell out to `docker compose` CLI** rather than reimplementing the Compose spec. The Docker SDK is used only for container-level operations.

2. **No persistent storage**. All state comes from scanning the filesystem and querying Docker. Projects are identified by SHA256 hash of their directory path.

3. **Async operations return HTTP 202**. Long-running compose commands (up/down/pull) return immediately; progress streams via SSE events (`compose:output`, `compose:complete`).

4. **HTMX partial updates**. Routes under `/partials/*` return HTML fragments for in-place DOM updates. The frontend JavaScript coordinates SSE events with htmx refreshes.

5. **Projects directory is read-only**. Gosei reads compose files but never modifies them.

### SSE Event Types

- `container:status` - Container state changes from Docker events
- `project:status` - Aggregated project running/stopped status
- `compose:output` - Streaming stdout/stderr from compose commands
- `compose:complete` - Operation finished with success/failure
