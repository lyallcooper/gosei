package project

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Project represents a Docker Compose project
type Project struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	ComposeFile string            `json:"composeFile"`
	Services    []ServiceInfo     `json:"services"`
	Status      string            `json:"status"` // "running", "partial", "stopped", "unknown"
	Running     int               `json:"running"`
	Total       int               `json:"total"`
	LastUpdated time.Time         `json:"lastUpdated"`
	EnvFiles    []string          `json:"envFiles"`
	Labels      map[string]string `json:"labels"`
}

// ServiceInfo represents a service defined in compose file
type ServiceInfo struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Build       *BuildInfo        `json:"build,omitempty"`
	Ports       []string          `json:"ports"`
	Volumes     []string          `json:"volumes"`
	Environment map[string]string `json:"environment"`
	DependsOn   []string          `json:"dependsOn"`
	Labels      map[string]string `json:"labels"`
}

// BuildInfo represents build configuration for a service
type BuildInfo struct {
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile"`
}

// Scanner scans directories for Docker Compose projects
type Scanner struct {
	baseDir  string
	projects map[string]*Project
	mu       sync.RWMutex
}

// NewScanner creates a new project scanner
func NewScanner(baseDir string) *Scanner {
	return &Scanner{
		baseDir:  baseDir,
		projects: make(map[string]*Project),
	}
}

// Scan scans the base directory for compose projects
func (s *Scanner) Scan(ctx context.Context) ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing projects
	s.projects = make(map[string]*Project)

	// Walk the directory looking for compose files
	err := filepath.WalkDir(s.baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip directories we can't read
		}

		// Skip hidden directories
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}

		// Check for compose files
		if !d.IsDir() && isComposeFile(d.Name()) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			project, err := s.parseProject(path)
			if err != nil {
				// Log error but continue scanning
				return nil
			}

			s.projects[project.ID] = project
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	// Convert map to slice and sort by name
	projects := make([]*Project, 0, len(s.projects))
	for _, p := range s.projects {
		projects = append(projects, p)
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	return projects, nil
}

// GetProject returns a project by ID
func (s *Scanner) GetProject(id string) (*Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	return p, ok
}

// GetProjectByPath returns a project by its path
func (s *Scanner) GetProjectByPath(path string) (*Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.projects {
		if p.Path == path {
			return p, true
		}
	}
	return nil, false
}

// ListProjects returns all projects
func (s *Scanner) ListProjects() []*Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	projects := make([]*Project, 0, len(s.projects))
	for _, p := range s.projects {
		projects = append(projects, p)
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	return projects
}

// RefreshProject refreshes a single project's information
func (s *Scanner) RefreshProject(id string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.projects[id]
	if !ok {
		return nil, fmt.Errorf("project not found: %s", id)
	}

	project, err := s.parseProject(existing.ComposeFile)
	if err != nil {
		return nil, err
	}

	s.projects[id] = project
	return project, nil
}

// parseProject parses a compose file and creates a Project
func (s *Scanner) parseProject(composeFilePath string) (*Project, error) {
	data, err := os.ReadFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose composeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	projectDir := filepath.Dir(composeFilePath)
	projectName := filepath.Base(projectDir)

	// Generate a stable ID based on the path
	id := generateProjectID(projectDir)

	// Parse services
	services := make([]ServiceInfo, 0, len(compose.Services))
	for name, svc := range compose.Services {
		serviceInfo := ServiceInfo{
			Name:        name,
			Image:       svc.Image,
			Ports:       svc.Ports,
			Volumes:     svc.Volumes,
			Environment: parseEnvironment(svc.Environment),
			DependsOn:   parseDependsOn(svc.DependsOn),
			Labels:      parseLabels(svc.Labels),
		}

		if svc.Build != nil {
			serviceInfo.Build = parseBuild(svc.Build)
		}

		services = append(services, serviceInfo)
	}

	// Sort services by name
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	// Find .env files
	envFiles := findEnvFiles(projectDir)

	return &Project{
		ID:          id,
		Name:        projectName,
		Path:        projectDir,
		ComposeFile: composeFilePath,
		Services:    services,
		Status:      "unknown",
		Total:       len(services),
		LastUpdated: time.Now(),
		EnvFiles:    envFiles,
	}, nil
}

// UpdateProjectStatus updates the running status of a project
func (s *Scanner) UpdateProjectStatus(id string, running int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if project, ok := s.projects[id]; ok {
		project.Running = running
		project.Status = status
		project.LastUpdated = time.Now()
	}
}

// composeFile represents the structure of a docker-compose.yml
type composeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]composeService `yaml:"services"`
	Networks map[string]interface{}    `yaml:"networks"`
	Volumes  map[string]interface{}    `yaml:"volumes"`
}

// composeService represents a service in docker-compose.yml
type composeService struct {
	Image       string      `yaml:"image"`
	Build       interface{} `yaml:"build"` // Can be string or object
	Ports       []string    `yaml:"ports"`
	Volumes     []string    `yaml:"volumes"`
	Environment interface{} `yaml:"environment"` // Can be list or map
	DependsOn   interface{} `yaml:"depends_on"`  // Can be list or map
	Labels      interface{} `yaml:"labels"`      // Can be list or map
	Command     interface{} `yaml:"command"`
	Restart     string      `yaml:"restart"`
}

// isComposeFile checks if a filename is a compose file
func isComposeFile(name string) bool {
	composeNames := []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
	}

	for _, cn := range composeNames {
		if name == cn {
			return true
		}
	}
	return false
}

// generateProjectID generates a stable ID from the project path
func generateProjectID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%x", hash[:8])
}

// parseEnvironment parses the environment field which can be a list or map
func parseEnvironment(env interface{}) map[string]string {
	result := make(map[string]string)
	if env == nil {
		return result
	}

	switch e := env.(type) {
	case []interface{}:
		for _, item := range e {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				} else {
					result[parts[0]] = ""
				}
			}
		}
	case map[string]interface{}:
		for k, v := range e {
			if v == nil {
				result[k] = ""
			} else {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	return result
}

// parseDependsOn parses the depends_on field which can be a list or map
func parseDependsOn(deps interface{}) []string {
	var result []string
	if deps == nil {
		return result
	}

	switch d := deps.(type) {
	case []interface{}:
		for _, item := range d {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
	case map[string]interface{}:
		for k := range d {
			result = append(result, k)
		}
	}

	sort.Strings(result)
	return result
}

// parseLabels parses the labels field which can be a list or map
func parseLabels(labels interface{}) map[string]string {
	result := make(map[string]string)
	if labels == nil {
		return result
	}

	switch l := labels.(type) {
	case []interface{}:
		for _, item := range l {
			if str, ok := item.(string); ok {
				parts := strings.SplitN(str, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				} else {
					result[parts[0]] = ""
				}
			}
		}
	case map[string]interface{}:
		for k, v := range l {
			if v == nil {
				result[k] = ""
			} else {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	return result
}

// parseBuild parses the build field which can be a string or object
func parseBuild(build interface{}) *BuildInfo {
	if build == nil {
		return nil
	}

	switch b := build.(type) {
	case string:
		return &BuildInfo{Context: b}
	case map[string]interface{}:
		info := &BuildInfo{}
		if ctx, ok := b["context"].(string); ok {
			info.Context = ctx
		}
		if df, ok := b["dockerfile"].(string); ok {
			info.Dockerfile = df
		}
		return info
	}

	return nil
}

// findEnvFiles finds .env files in a project directory
func findEnvFiles(dir string) []string {
	var envFiles []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return envFiles
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == ".env" || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env") {
			envFiles = append(envFiles, name)
		}
	}

	sort.Strings(envFiles)
	return envFiles
}
