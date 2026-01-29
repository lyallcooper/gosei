package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lyall/gosei/internal/api"
	"github.com/lyall/gosei/internal/docker"
	"github.com/lyall/gosei/internal/project"
	"github.com/lyall/gosei/internal/sse"
)

var (
	// Version is set at build time
	Version = "0.1.0"
)

func main() {
	// Parse flags
	host := flag.String("host", getEnv("GOSEI_HOST", "127.0.0.1"), "Host to bind to")
	port := flag.String("port", getEnv("GOSEI_PORT", "8080"), "Port to listen on")
	projectsDir := flag.String("projects-dir", getEnv("GOSEI_PROJECTS_DIR", "."), "Directory containing compose projects")
	mockMode := flag.Bool("mock", getEnvBool("GOSEI_MOCK", false), "Run with mock Docker client (no Docker required)")
	flag.Parse()

	// Validate projects directory
	if _, err := os.Stat(*projectsDir); os.IsNotExist(err) {
		log.Fatalf("Projects directory does not exist: %s", *projectsDir)
	}

	log.Printf("Starting Gosei v%s", Version)
	log.Printf("Projects directory: %s", *projectsDir)

	// Initialize Docker client (real or mock)
	var dockerClient docker.DockerClient
	var composeClient docker.ComposeExecutor

	if *mockMode {
		log.Println("Running in MOCK MODE - no Docker connection required")
		mockDocker := docker.NewMockClient()
		dockerClient = mockDocker
		composeClient = docker.NewMockComposeClient(mockDocker)
	} else {
		realClient, err := docker.NewClient()
		if err != nil {
			log.Fatalf("Failed to create Docker client: %v", err)
		}
		dockerClient = realClient
		composeClient = docker.NewComposeClient(realClient)
	}
	defer dockerClient.Close()

	// Initialize project scanner
	scanner := project.NewScanner(*projectsDir)

	// Initial scan
	projects, err := scanner.Scan(context.Background())
	if err != nil {
		log.Printf("Warning: Failed to scan projects: %v", err)
	} else {
		log.Printf("Found %d projects", len(projects))
	}

	// Initialize SSE broker
	broker := sse.NewBroker()
	defer broker.Close()

	// Start watching Docker events
	go watchDockerEvents(dockerClient, broker, scanner)

	// Create router
	router := api.NewRouter(&api.Config{
		DockerClient:  dockerClient,
		ComposeClient: composeClient,
		Scanner:       scanner,
		SSEBroker:     broker,
		Version:       Version,
	})

	// Create HTTP server
	addr := fmt.Sprintf("%s:%s", *host, *port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server listening on http://%s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// getEnv returns an environment variable value or a default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool returns an environment variable as bool or a default
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}

// watchDockerEvents watches for Docker events and broadcasts them via SSE
func watchDockerEvents(client docker.DockerClient, broker *sse.Broker, scanner *project.Scanner) {
	ctx := context.Background()

	for {
		events, errs := client.WatchEvents(ctx)

		for {
			select {
			case event, ok := <-events:
				if !ok {
					goto reconnect
				}

				// Broadcast container status change
				broker.BroadcastJSON("container:status", sse.ContainerStatusEvent{
					ID:      event.ID[:12],
					Name:    event.Name,
					Status:  event.Action,
					State:   mapActionToState(event.Action),
					Project: event.Project,
					Service: event.Service,
				})

				// Update project status if this is a compose container
				if event.Project != "" {
					updateProjectStatus(ctx, client, scanner, broker, event.Project)
				}

			case err, ok := <-errs:
				if !ok {
					goto reconnect
				}
				if err != nil {
					log.Printf("Docker events error: %v", err)
					goto reconnect
				}
			}
		}

	reconnect:
		log.Println("Docker events disconnected, reconnecting in 5s...")
		time.Sleep(5 * time.Second)
	}
}

// mapActionToState maps Docker event actions to container states
func mapActionToState(action string) string {
	switch action {
	case "start":
		return "running"
	case "stop", "die", "kill":
		return "exited"
	case "pause":
		return "paused"
	case "unpause":
		return "running"
	case "restart":
		return "restarting"
	default:
		return action
	}
}

// updateProjectStatus updates and broadcasts project status
func updateProjectStatus(ctx context.Context, client docker.DockerClient, scanner *project.Scanner, broker *sse.Broker, projectName string) {
	// Find project by name
	projects := scanner.ListProjects()
	var proj *project.Project
	for _, p := range projects {
		if p.Name == projectName {
			proj = p
			break
		}
	}

	if proj == nil {
		return
	}

	// Get container status
	containers, err := client.ListContainers(ctx, projectName)
	if err != nil {
		return
	}

	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
	}

	status := "stopped"
	if running > 0 && running == proj.Total {
		status = "running"
	} else if running > 0 {
		status = "partial"
	}

	scanner.UpdateProjectStatus(proj.ID, running, status)

	// Broadcast update
	broker.BroadcastJSON("project:status", sse.ProjectStatusEvent{
		ID:      proj.ID,
		Name:    proj.Name,
		Status:  status,
		Running: running,
		Total:   proj.Total,
	})
}
