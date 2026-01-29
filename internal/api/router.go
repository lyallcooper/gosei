package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/lyall/gosei/internal/api/handler"
	"github.com/lyall/gosei/internal/docker"
	"github.com/lyall/gosei/internal/project"
	"github.com/lyall/gosei/internal/sse"
	"github.com/lyall/gosei/web"
)

// Config holds API configuration
type Config struct {
	DockerClient  docker.DockerClient
	ComposeClient docker.ComposeExecutor
	Scanner       *project.Scanner
	SSEBroker     *sse.Broker
	Version       string
}

// NewRouter creates a new HTTP router
func NewRouter(cfg *Config) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)

	// Create handlers
	projectHandler := handler.NewProjectHandler(cfg.DockerClient, cfg.ComposeClient, cfg.Scanner, cfg.SSEBroker)
	containerHandler := handler.NewContainerHandler(cfg.DockerClient, cfg.SSEBroker)
	systemHandler := handler.NewSystemHandler(cfg.Version)
	pageHandler := handler.NewPageHandler(cfg.DockerClient, cfg.Scanner, cfg.Version)

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(web.StaticFS()))))

	// Page routes
	r.Get("/", pageHandler.Dashboard)
	r.Get("/projects/{id}", pageHandler.ProjectDetail)
	r.Get("/containers/{id}", pageHandler.ContainerDetail)
	r.Get("/containers/{id}/logs", pageHandler.ContainerLogs)

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Projects
		r.Get("/projects", projectHandler.List)
		r.Get("/projects/{id}", projectHandler.Get)
		r.Post("/projects/{id}/up", projectHandler.Up)
		r.Post("/projects/{id}/down", projectHandler.Down)
		r.Post("/projects/{id}/pull", projectHandler.Pull)
		r.Post("/projects/{id}/restart", projectHandler.Restart)
		r.Post("/projects/{id}/update", projectHandler.Update)
		r.Post("/projects/refresh", projectHandler.Refresh)

		// Containers
		r.Get("/containers", containerHandler.List)
		r.Get("/containers/{id}", containerHandler.Get)
		r.Post("/containers/{id}/start", containerHandler.Start)
		r.Post("/containers/{id}/stop", containerHandler.Stop)
		r.Post("/containers/{id}/restart", containerHandler.Restart)
		r.Get("/containers/{id}/logs", containerHandler.Logs)
		r.Get("/containers/{id}/stats", containerHandler.Stats)

		// System
		r.Get("/system/health", systemHandler.Health)
		r.Get("/system/version", systemHandler.Version)

		// SSE events
		r.Get("/events", cfg.SSEBroker.ServeHTTP)
	})

	// HTMX partials
	r.Route("/partials", func(r chi.Router) {
		r.Get("/projects", pageHandler.ProjectsPartial)
		r.Get("/projects/{id}", pageHandler.ProjectDetailPartial)
		r.Get("/containers/{id}/logs-content", pageHandler.ContainerLogsContent)
	})

	return r
}
