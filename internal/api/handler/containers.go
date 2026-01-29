package handler

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lyall/gosei/internal/docker"
	"github.com/lyall/gosei/internal/sse"
)

// ContainerHandler handles container-related API requests
type ContainerHandler struct {
	docker docker.DockerClient
	broker *sse.Broker
}

// NewContainerHandler creates a new container handler
func NewContainerHandler(dc docker.DockerClient, b *sse.Broker) *ContainerHandler {
	return &ContainerHandler{
		docker: dc,
		broker: b,
	}
}

// List returns all containers
func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	projectName := r.URL.Query().Get("project")

	containers, err := h.docker.ListContainers(r.Context(), projectName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list containers: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, containers)
}

// Get returns a specific container
func (h *ContainerHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	container, err := h.docker.GetContainer(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "Container not found: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, container)
}

// Start starts a container
func (h *ContainerHandler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.docker.StartContainer(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to start container: "+err.Error())
		return
	}

	// Get updated container info
	container, _ := h.docker.GetContainer(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "started",
		"container": container,
	})
}

// Stop stops a container
func (h *ContainerHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.docker.StopContainer(r.Context(), id, 30); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to stop container: "+err.Error())
		return
	}

	// Get updated container info
	container, _ := h.docker.GetContainer(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "stopped",
		"container": container,
	})
}

// Restart restarts a container
func (h *ContainerHandler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.docker.RestartContainer(r.Context(), id, 30); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to restart container: "+err.Error())
		return
	}

	// Get updated container info
	container, _ := h.docker.GetContainer(r.Context(), id)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "restarted",
		"container": container,
	})
}

// Logs streams container logs
func (h *ContainerHandler) Logs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	follow := r.URL.Query().Get("follow") == "true"

	// If following, use SSE
	if follow {
		h.streamLogs(w, r, id, tail)
		return
	}

	// Otherwise, return logs as JSON
	logs, err := h.docker.GetContainerLogs(r.Context(), id, tail, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get logs: "+err.Error())
		return
	}
	defer logs.Close()

	lines := parseLogLines(logs)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"containerId": id,
		"lines":       lines,
	})
}

// streamLogs streams logs via SSE
func (h *ContainerHandler) streamLogs(w http.ResponseWriter, r *http.Request, id string, tail string) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Disable write deadline for SSE connections
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE not supported")
		return
	}

	logs, err := h.docker.GetContainerLogs(r.Context(), id, tail, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get logs: "+err.Error())
		return
	}
	defer logs.Close()

	// Get container name
	container, _ := h.docker.GetContainer(r.Context(), id)
	containerName := id
	if container != nil {
		containerName = container.Name
	}

	reader := bufio.NewReader(logs)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Printf("Error reading logs: %v", err)
				}
				return
			}

			// Parse log line (Docker adds 8-byte header for multiplexed streams)
			logLine := parseDockerLogLine(line)
			if logLine == "" {
				continue
			}

			event := sse.LogLineEvent{
				ContainerID: id,
				Container:   containerName,
				Line:        logLine,
				Stream:      "stdout",
				Timestamp:   time.Now(),
			}

			data, _ := json.Marshal(event)
			w.Write([]byte("event: log\ndata: "))
			w.Write(data)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

// Stats returns container stats
func (h *ContainerHandler) Stats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	stats, err := h.docker.GetContainerStats(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get stats: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// LogLine represents a parsed log line
type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
}

// parseLogLines parses Docker log output into structured lines
func parseLogLines(r io.Reader) []LogLine {
	var lines []LogLine
	reader := bufio.NewReader(r)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		logLine := parseDockerLogLine(line)
		if logLine == "" {
			continue
		}

		// Try to parse timestamp from the line
		parts := strings.SplitN(logLine, " ", 2)
		var timestamp time.Time
		var message string

		if len(parts) == 2 {
			if t, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
				timestamp = t
				message = parts[1]
			} else {
				timestamp = time.Now()
				message = logLine
			}
		} else {
			timestamp = time.Now()
			message = logLine
		}

		lines = append(lines, LogLine{
			Timestamp: timestamp,
			Stream:    "stdout",
			Message:   strings.TrimSuffix(message, "\n"),
		})
	}

	return lines
}

// parseDockerLogLine removes Docker's 8-byte header from multiplexed log output
func parseDockerLogLine(line string) string {
	if len(line) < 8 {
		return strings.TrimSpace(line)
	}

	// Docker multiplexed log format has an 8-byte header
	// First byte is stream type (1=stdout, 2=stderr)
	// Bytes 4-7 are the frame size (big-endian)
	header := []byte(line[:8])

	// Check if this looks like a Docker log header
	// Stream type should be 0, 1, or 2
	if header[0] <= 2 && header[1] == 0 && header[2] == 0 && header[3] == 0 {
		return strings.TrimSpace(line[8:])
	}

	return strings.TrimSpace(line)
}
