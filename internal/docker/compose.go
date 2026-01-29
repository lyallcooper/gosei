package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ComposeClient handles Docker Compose operations
type ComposeClient struct {
	dockerClient *Client
}

// NewComposeClient creates a new Compose client
func NewComposeClient(dockerClient *Client) *ComposeClient {
	return &ComposeClient{dockerClient: dockerClient}
}

// ComposeOutput represents output from a compose command
type ComposeOutput struct {
	Line   string `json:"line"`
	Stream string `json:"stream"` // "stdout" or "stderr"
}

// ComposeResult represents the result of a compose operation
type ComposeResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Up runs docker compose up for a project
func (c *ComposeClient) Up(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	return c.runCompose(ctx, projectDir, []string{"up", "-d", "--remove-orphans"}, outputCh)
}

// Down runs docker compose down for a project
func (c *ComposeClient) Down(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	return c.runCompose(ctx, projectDir, []string{"down", "--remove-orphans"}, outputCh)
}

// Pull runs docker compose pull for a project
func (c *ComposeClient) Pull(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	return c.runCompose(ctx, projectDir, []string{"pull"}, outputCh)
}

// Restart runs docker compose restart for a project
func (c *ComposeClient) Restart(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	return c.runCompose(ctx, projectDir, []string{"restart"}, outputCh)
}

// Update pulls new images and recreates containers
func (c *ComposeClient) Update(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	// First pull
	result, err := c.runCompose(ctx, projectDir, []string{"pull"}, outputCh)
	if err != nil {
		return result, err
	}
	if !result.Success {
		return result, nil
	}

	// Then recreate with up
	return c.runCompose(ctx, projectDir, []string{"up", "-d", "--remove-orphans", "--force-recreate"}, outputCh)
}

// runCompose executes a docker compose command
func (c *ComposeClient) runCompose(ctx context.Context, projectDir string, args []string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	// Find compose file
	composeFile, err := findComposeFile(projectDir)
	if err != nil {
		return &ComposeResult{Success: false, Message: err.Error()}, err
	}

	// Build command
	cmdArgs := []string{"compose", "-f", composeFile}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = projectDir

	// Set up pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &ComposeResult{Success: false, Message: err.Error()}, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &ComposeResult{Success: false, Message: err.Error()}, err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return &ComposeResult{Success: false, Message: err.Error()}, err
	}

	// Stream output
	done := make(chan struct{})
	go streamOutput(stdout, "stdout", outputCh, done)
	go streamOutput(stderr, "stderr", outputCh, done)

	// Wait for streaming to complete
	<-done
	<-done

	// Wait for command to finish
	err = cmd.Wait()
	if err != nil {
		return &ComposeResult{
			Success: false,
			Message: fmt.Sprintf("Command failed: %s", err.Error()),
		}, nil
	}

	return &ComposeResult{
		Success: true,
		Message: "Operation completed successfully",
	}, nil
}

// streamOutput reads from a reader and sends output to a channel
func streamOutput(r io.Reader, stream string, outputCh chan<- ComposeOutput, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if outputCh != nil {
			outputCh <- ComposeOutput{
				Line:   line,
				Stream: stream,
			}
		}
	}
}

// findComposeFile finds the compose file in a directory
func findComposeFile(dir string) (string, error) {
	// Check for compose files in order of preference
	composeFiles := []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yaml",
		"docker-compose.yml",
	}

	for _, name := range composeFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no compose file found in %s", dir)
}

// GetComposeServices returns the list of services defined in a compose file
func (c *ComposeClient) GetComposeServices(ctx context.Context, projectDir string) ([]string, error) {
	composeFile, err := findComposeFile(projectDir)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "config", "--services")
	cmd.Dir = projectDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get services: %w", err)
	}

	var services []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		service := strings.TrimSpace(scanner.Text())
		if service != "" {
			services = append(services, service)
		}
	}

	return services, nil
}

// GetComposePs returns the status of services in a compose project
func (c *ComposeClient) GetComposePs(ctx context.Context, projectDir string) ([]map[string]string, error) {
	composeFile, err := findComposeFile(projectDir)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "ps", "--format", "json")
	cmd.Dir = projectDir

	output, err := cmd.Output()
	if err != nil {
		// If the project isn't running, return empty
		return []map[string]string{}, nil
	}

	// Parse JSON output (each line is a JSON object)
	var results []map[string]string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && line != "[]" {
			// Simple parsing - docker compose ps --format json outputs one JSON object per line
			result := make(map[string]string)
			result["raw"] = line
			results = append(results, result)
		}
	}

	return results, nil
}
