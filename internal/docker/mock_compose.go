package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// MockComposeClient provides mock Docker Compose operations
type MockComposeClient struct {
	dockerClient *MockClient
}

// NewMockComposeClient creates a new mock Compose client
func NewMockComposeClient(dockerClient *MockClient) *MockComposeClient {
	return &MockComposeClient{dockerClient: dockerClient}
}

// Up simulates docker compose up
func (c *MockComposeClient) Up(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	projectName := projectNameFromDir(projectDir)
	services := c.getProjectServices(projectName)

	c.sendOutput(outputCh, fmt.Sprintf("[+] Running %d/%d", 0, len(services)))
	time.Sleep(500 * time.Millisecond)

	for i, svc := range services {
		select {
		case <-ctx.Done():
			return &ComposeResult{Success: false, Message: "Operation cancelled"}, ctx.Err()
		default:
		}

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Starting", projectName, svc))
		time.Sleep(300 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Started   %.1fs", projectName, svc, 0.3+float64(i)*0.2))
		time.Sleep(200 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf("[+] Running %d/%d", i+1, len(services)))
	}

	// Update container states
	c.dockerClient.SetAllContainersState(projectName, "running", "Up Less than a second")

	return &ComposeResult{Success: true, Message: "Started successfully"}, nil
}

// Down simulates docker compose down
func (c *MockComposeClient) Down(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	projectName := projectNameFromDir(projectDir)
	services := c.getProjectServices(projectName)

	c.sendOutput(outputCh, fmt.Sprintf("[+] Running %d/%d", 0, len(services)))
	time.Sleep(500 * time.Millisecond)

	for i, svc := range services {
		select {
		case <-ctx.Done():
			return &ComposeResult{Success: false, Message: "Operation cancelled"}, ctx.Err()
		default:
		}

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Stopping", projectName, svc))
		time.Sleep(400 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Stopped   %.1fs", projectName, svc, 0.4+float64(i)*0.2))
		time.Sleep(200 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf("[+] Running %d/%d", i+1, len(services)))
	}

	// Remove network
	c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Network %s_default  Removed", projectName))

	// Update container states
	c.dockerClient.SetAllContainersState(projectName, "exited", "Exited (0) Less than a second ago")

	return &ComposeResult{Success: true, Message: "Stopped successfully"}, nil
}

// Pull simulates docker compose pull
func (c *MockComposeClient) Pull(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	projectName := projectNameFromDir(projectDir)
	services := c.getProjectServices(projectName)

	for _, svc := range services {
		select {
		case <-ctx.Done():
			return &ComposeResult{Success: false, Message: "Operation cancelled"}, ctx.Err()
		default:
		}

		c.sendOutput(outputCh, fmt.Sprintf("[+] Pulling %s", svc))
		time.Sleep(300 * time.Millisecond)

		// Simulate progress
		for pct := 0; pct <= 100; pct += 25 {
			c.sendOutput(outputCh, fmt.Sprintf("[+] %s Pulling  %d%%", svc, pct))
			time.Sleep(200 * time.Millisecond)
		}

		c.sendOutput(outputCh, fmt.Sprintf("[+] %s Pulled", svc))
	}

	return &ComposeResult{Success: true, Message: "Pulled successfully"}, nil
}

// Restart simulates docker compose restart
func (c *MockComposeClient) Restart(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	projectName := projectNameFromDir(projectDir)
	services := c.getProjectServices(projectName)

	c.sendOutput(outputCh, fmt.Sprintf("[+] Restarting %d services", len(services)))
	time.Sleep(500 * time.Millisecond)

	for i, svc := range services {
		select {
		case <-ctx.Done():
			return &ComposeResult{Success: false, Message: "Operation cancelled"}, ctx.Err()
		default:
		}

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Restarting", projectName, svc))
		time.Sleep(600 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Restarted   %.1fs", projectName, svc, 0.6+float64(i)*0.2))
		time.Sleep(200 * time.Millisecond)
	}

	// Emit restart events
	c.dockerClient.SetAllContainersState(projectName, "running", "Up Less than a second")

	return &ComposeResult{Success: true, Message: "Restarted successfully"}, nil
}

// Update simulates docker compose pull && up --force-recreate
func (c *MockComposeClient) Update(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error) {
	// First pull
	result, err := c.Pull(ctx, projectDir, outputCh)
	if err != nil || !result.Success {
		return result, err
	}

	c.sendOutput(outputCh, "")
	c.sendOutput(outputCh, "[+] Recreating containers...")
	time.Sleep(500 * time.Millisecond)

	// Then recreate
	projectName := projectNameFromDir(projectDir)
	services := c.getProjectServices(projectName)

	for i, svc := range services {
		select {
		case <-ctx.Done():
			return &ComposeResult{Success: false, Message: "Operation cancelled"}, ctx.Err()
		default:
		}

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Recreating", projectName, svc))
		time.Sleep(400 * time.Millisecond)

		c.sendOutput(outputCh, fmt.Sprintf(" \u2714 Container %s-%s-1  Recreated   %.1fs", projectName, svc, 0.4+float64(i)*0.2))
		time.Sleep(200 * time.Millisecond)
	}

	c.dockerClient.SetAllContainersState(projectName, "running", "Up Less than a second")

	return &ComposeResult{Success: true, Message: "Updated successfully"}, nil
}

func (c *MockComposeClient) sendOutput(outputCh chan<- ComposeOutput, line string) {
	if outputCh != nil {
		outputCh <- ComposeOutput{Line: line, Stream: "stdout"}
	}
}

func (c *MockComposeClient) getProjectServices(projectName string) []string {
	services := make(map[string]bool)

	containers, _ := c.dockerClient.ListContainers(context.Background(), projectName)
	for _, ctr := range containers {
		if ctr.ServiceName != "" {
			services[ctr.ServiceName] = true
		}
	}

	result := make([]string, 0, len(services))
	for svc := range services {
		result = append(result, svc)
	}

	if len(result) == 0 {
		// Default services if none found
		return []string{"app"}
	}

	return result
}

func projectNameFromDir(dir string) string {
	if dir == "" {
		return "unknown"
	}
	return filepath.Base(dir)
}

// Verify MockComposeClient implements ComposeExecutor
var _ ComposeExecutor = (*MockComposeClient)(nil)
