package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lyall/gosei/internal/docker"
	"github.com/lyall/gosei/internal/project"
	"github.com/lyall/gosei/internal/sse"
)

// ProjectHandler handles project-related API requests
type ProjectHandler struct {
	docker  docker.DockerClient
	compose docker.ComposeExecutor
	scanner *project.Scanner
	broker  *sse.Broker
}

// NewProjectHandler creates a new project handler
func NewProjectHandler(dc docker.DockerClient, cc docker.ComposeExecutor, s *project.Scanner, b *sse.Broker) *ProjectHandler {
	return &ProjectHandler{
		docker:  dc,
		compose: cc,
		scanner: s,
		broker:  b,
	}
}

// ProjectResponse represents a project in API responses
type ProjectResponse struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Path       string                 `json:"path"`
	Status     string                 `json:"status"`
	Running    int                    `json:"running"`
	Total      int                    `json:"total"`
	Services   []project.ServiceInfo  `json:"services"`
	Containers []docker.ContainerInfo `json:"containers,omitempty"`
}

// List returns all projects
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	projects := h.scanner.ListProjects()

	// Update project status from running containers
	for _, p := range projects {
		h.updateProjectStatus(r.Context(), p)
	}

	responses := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		responses[i] = projectToResponse(p)
	}

	writeJSON(w, http.StatusOK, responses)
}

// Get returns a specific project
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p, ok := h.scanner.GetProject(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	h.updateProjectStatus(r.Context(), p)

	// Get containers for this project
	containers, err := h.docker.ListContainers(r.Context(), p.Name)
	if err != nil {
		log.Printf("Failed to list containers for project %s: %v", p.Name, err)
	}

	resp := projectToResponse(p)
	resp.Containers = containers

	writeJSON(w, http.StatusOK, resp)
}

// Up runs docker compose up for a project
func (h *ProjectHandler) Up(w http.ResponseWriter, r *http.Request) {
	h.runComposeOperation(w, r, "up", h.compose.Up)
}

// Down runs docker compose down for a project
func (h *ProjectHandler) Down(w http.ResponseWriter, r *http.Request) {
	h.runComposeOperation(w, r, "down", h.compose.Down)
}

// Pull runs docker compose pull for a project
func (h *ProjectHandler) Pull(w http.ResponseWriter, r *http.Request) {
	h.runComposeOperation(w, r, "pull", h.compose.Pull)
}

// Restart runs docker compose restart for a project
func (h *ProjectHandler) Restart(w http.ResponseWriter, r *http.Request) {
	h.runComposeOperation(w, r, "restart", h.compose.Restart)
}

// Update pulls and recreates containers for a project
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	h.runComposeOperation(w, r, "update", h.compose.Update)
}

// Refresh rescans the projects directory
func (h *ProjectHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	projects, err := h.scanner.Scan(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to scan projects: "+err.Error())
		return
	}

	responses := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		h.updateProjectStatus(r.Context(), p)
		responses[i] = projectToResponse(p)
	}

	writeJSON(w, http.StatusOK, responses)
}

// composeOp represents a compose operation function
type composeOp func(ctx context.Context, projectDir string, outputCh chan<- docker.ComposeOutput) (*docker.ComposeResult, error)

// runComposeOperation runs a compose operation and streams output via SSE
func (h *ProjectHandler) runComposeOperation(w http.ResponseWriter, r *http.Request, operation string, op composeOp) {
	id := chi.URLParam(r, "id")

	p, ok := h.scanner.GetProject(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Project not found")
		return
	}

	// Create output channel
	outputCh := make(chan docker.ComposeOutput, 100)

	// Start streaming output to SSE
	go func() {
		for output := range outputCh {
			h.broker.BroadcastJSON("compose:output", sse.ComposeOutputEvent{
				ProjectID: id,
				Operation: operation,
				Line:      output.Line,
				Stream:    output.Stream,
			})
		}
	}()

	// Run the operation in a goroutine
	go func() {
		defer close(outputCh)

		// Use background context since this runs after the HTTP response is sent
		result, err := op(context.Background(), p.Path, outputCh)

		// Broadcast completion
		success := err == nil && result != nil && result.Success
		message := "Operation completed"
		if err != nil {
			message = err.Error()
		} else if result != nil && !result.Success {
			message = result.Message
		}

		h.broker.BroadcastJSON("compose:complete", sse.ComposeCompleteEvent{
			ProjectID: id,
			Operation: operation,
			Success:   success,
			Message:   message,
		})

		// Update project status
		if p, ok := h.scanner.GetProject(id); ok {
			ctx := context.Background()
			h.updateProjectStatus(ctx, p)

			h.broker.BroadcastJSON("project:status", sse.ProjectStatusEvent{
				ID:      p.ID,
				Name:    p.Name,
				Status:  p.Status,
				Running: p.Running,
				Total:   p.Total,
			})
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":    "started",
		"operation": operation,
		"projectId": id,
	})
}

// updateProjectStatus updates a project's status based on running containers
func (h *ProjectHandler) updateProjectStatus(ctx context.Context, p *project.Project) {
	containers, err := h.docker.ListContainers(ctx, p.Name)
	if err != nil {
		p.Status = "unknown"
		return
	}

	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
	}

	p.Running = running
	if running == 0 {
		p.Status = "stopped"
	} else if running == p.Total {
		p.Status = "running"
	} else {
		p.Status = "partial"
	}

	h.scanner.UpdateProjectStatus(p.ID, running, p.Status)
}

// projectToResponse converts a project to an API response
func projectToResponse(p *project.Project) ProjectResponse {
	return ProjectResponse{
		ID:       p.ID,
		Name:     p.Name,
		Path:     p.Path,
		Status:   p.Status,
		Running:  p.Running,
		Total:    p.Total,
		Services: p.Services,
	}
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
