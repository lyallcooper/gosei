package handler

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lyall/gosei/internal/docker"
	"github.com/lyall/gosei/internal/project"
	"github.com/lyall/gosei/web"
)

// PageHandler handles page rendering
type PageHandler struct {
	docker    docker.DockerClient
	scanner   *project.Scanner
	version   string
	templates *template.Template
}

// NewPageHandler creates a new page handler
func NewPageHandler(dc docker.DockerClient, s *project.Scanner, version string) *PageHandler {
	// Parse templates
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseFS(web.TemplatesFS(), "templates/**/*.html")
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	return &PageHandler{
		docker:    dc,
		scanner:   s,
		version:   version,
		templates: tmpl,
	}
}

// templateFuncs returns custom template functions
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"statusClass": func(status string) string {
			switch status {
			case "running":
				return "status-running"
			case "partial":
				return "status-partial"
			case "stopped":
				return "status-stopped"
			default:
				return "status-unknown"
			}
		},
		"statusIcon": func(status string) string {
			switch status {
			case "running":
				return "●"
			case "partial", "restarting":
				return "◐"
			case "stopped", "exited", "dead", "created":
				return "○"
			default:
				return "○"
			}
		},
		"stateClass": func(state string) string {
			switch state {
			case "running":
				return "state-running"
			case "exited", "dead", "stopped":
				return "state-exited"
			case "paused":
				return "state-paused"
			case "restarting", "created":
				return "state-restarting"
			default:
				return "state-exited"
			}
		},
		"formatBytes": func(bytes uint64) string {
			const unit = 1024
			if bytes < unit {
				return fmt.Sprintf("%d B", bytes)
			}
			div, exp := uint64(unit), 0
			for n := bytes / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
		},
		"formatPercent": func(percent float64) string {
			return fmt.Sprintf("%.1f%%", percent)
		},
	}
}

// PageData holds common page data
type PageData struct {
	Title      string
	Version    string
	Projects   []*project.Project
	Project    *project.Project
	Container  *docker.ContainerInfo
	Containers []docker.ContainerInfo
	ShowLogs   bool
}

func (h *PageHandler) updateProjectStatuses(ctx context.Context, projects []*project.Project) {
	for _, p := range projects {
		containers, err := h.docker.ListContainers(ctx, p.Name)
		if err != nil {
			continue
		}
		running := 0
		for _, c := range containers {
			if c.State == "running" {
				running++
			}
		}
		p.Running = running
		switch {
		case running == 0:
			p.Status = "stopped"
		case running == p.Total:
			p.Status = "running"
		default:
			p.Status = "partial"
		}
	}
}

// Dashboard renders the main dashboard
func (h *PageHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	projects := h.scanner.ListProjects()
	h.updateProjectStatuses(r.Context(), projects)

	h.render(w, "base.html", PageData{
		Title:    "Dashboard",
		Version:  h.version,
		Projects: projects,
	})
}

// ProjectDetail renders a project detail page
func (h *PageHandler) ProjectDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p, ok := h.scanner.GetProject(id)
	if !ok {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	containers, _ := h.docker.ListContainers(r.Context(), p.Name)

	data := PageData{
		Title:      p.Name,
		Version:    h.version,
		Project:    p,
		Containers: containers,
	}

	h.render(w, "base.html", data)
}

// ContainerDetail renders a container detail page
func (h *PageHandler) ContainerDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	container, err := h.docker.GetContainer(r.Context(), id)
	if err != nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	data := PageData{
		Title:     container.Name,
		Version:   h.version,
		Container: container,
	}

	h.render(w, "base.html", data)
}

// ContainerLogs renders the container logs page
func (h *PageHandler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	container, err := h.docker.GetContainer(r.Context(), id)
	if err != nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	data := PageData{
		Title:     container.Name + " Logs",
		Version:   h.version,
		Container: container,
		ShowLogs:  true,
	}

	h.render(w, "base.html", data)
}

// ProjectsPartial renders just the projects list
func (h *PageHandler) ProjectsPartial(w http.ResponseWriter, r *http.Request) {
	projects := h.scanner.ListProjects()
	h.updateProjectStatuses(r.Context(), projects)
	h.renderPartial(w, "partials/project-list.html", PageData{Projects: projects})
}

// ProjectDetailPartial renders just the project detail
func (h *PageHandler) ProjectDetailPartial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p, ok := h.scanner.GetProject(id)
	if !ok {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	containers, _ := h.docker.ListContainers(r.Context(), p.Name)

	data := PageData{
		Project:    p,
		Containers: containers,
	}

	h.renderPartial(w, "partials/project-detail.html", data)
}

// ProjectContainersPartial renders just the containers section for a project
func (h *PageHandler) ProjectContainersPartial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	p, ok := h.scanner.GetProject(id)
	if !ok {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	containers, _ := h.docker.ListContainers(r.Context(), p.Name)

	h.renderPartial(w, "partials/containers-section.html", PageData{
		Project:    p,
		Containers: containers,
	})
}

// ContainerActionsPartial renders just the container actions
func (h *PageHandler) ContainerActionsPartial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	container, err := h.docker.GetContainer(r.Context(), id)
	if err != nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	h.renderPartial(w, "partials/container-actions.html", PageData{Container: container})
}

// ContainerLogsContent renders just the logs content
func (h *PageHandler) ContainerLogsContent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	container, err := h.docker.GetContainer(r.Context(), id)
	if err != nil {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	// Get last 100 lines
	logs, err := h.docker.GetContainerLogs(r.Context(), id, "100", false)
	if err != nil {
		http.Error(w, "Failed to get logs", http.StatusInternalServerError)
		return
	}
	defer logs.Close()

	lines := parseLogLines(logs)

	data := struct {
		Container *docker.ContainerInfo
		Lines     []LogLine
	}{
		Container: container,
		Lines:     lines,
	}

	h.renderPartial(w, "partials/logs-content.html", data)
}

func (h *PageHandler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Failed to render template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *PageHandler) renderPartial(w http.ResponseWriter, name string, data any) {
	h.render(w, name, data)
}
