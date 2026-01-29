package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Client wraps the Docker SDK client with convenience methods
type Client struct {
	cli *client.Client
	mu  sync.RWMutex
}

// ContainerInfo represents container information for the UI
type ContainerInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	ImageID     string            `json:"imageId"`
	Status      string            `json:"status"`
	State       string            `json:"state"`
	Health      string            `json:"health"`
	Created     time.Time         `json:"created"`
	Ports       []PortMapping     `json:"ports"`
	Labels      map[string]string `json:"labels"`
	ProjectName string            `json:"projectName"`
	ServiceName string            `json:"serviceName"`
	ComposeFile string            `json:"composeFile"`
	WorkingDir  string            `json:"workingDir"`
}

// PortMapping represents a port mapping
type PortMapping struct {
	HostIP        string `json:"hostIp"`
	HostPort      string `json:"hostPort"`
	ContainerPort string `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// ContainerStats represents container resource usage
type ContainerStats struct {
	ID            string  `json:"id"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsage   uint64  `json:"memoryUsage"`
	MemoryLimit   uint64  `json:"memoryLimit"`
	MemoryPercent float64 `json:"memoryPercent"`
	NetworkRx     uint64  `json:"networkRx"`
	NetworkTx     uint64  `json:"networkTx"`
}

// NewClient creates a new Docker client wrapper
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to docker daemon: %w", err)
	}

	return &Client{cli: cli}, nil
}

// Close closes the Docker client
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cli.Close()
}

// ListContainers returns all containers, optionally filtered by project
func (c *Client) ListContainers(ctx context.Context, projectName string) ([]ContainerInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	opts := container.ListOptions{All: true}

	if projectName != "" {
		opts.Filters = filters.NewArgs()
		opts.Filters.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))
	}

	containers, err := c.cli.ContainerList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, ctr := range containers {
		info := c.containerToInfo(ctr)
		result = append(result, info)
	}

	return result, nil
}

// GetContainer returns information about a specific container
func (c *Client) GetContainer(ctx context.Context, id string) (*ContainerInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	inspect, err := c.cli.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	info := c.inspectToInfo(inspect)
	return &info, nil
}

// StartContainer starts a container
func (c *Client) StartContainer(ctx context.Context, id string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// StopContainer stops a container with a timeout
func (c *Client) StopContainer(ctx context.Context, id string, timeout int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stopTimeout := timeout
	if err := c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &stopTimeout}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// RestartContainer restarts a container
func (c *Client) RestartContainer(ctx context.Context, id string, timeout int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	restartTimeout := timeout
	if err := c.cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &restartTimeout}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

// GetContainerLogs returns a stream of container logs
func (c *Client) GetContainerLogs(ctx context.Context, id string, tail string, follow bool) (io.ReadCloser, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Timestamps: true,
	}

	logs, err := c.cli.ContainerLogs(ctx, id, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get container logs: %w", err)
	}

	return logs, nil
}

// GetContainerStats returns stats for a container
func (c *Client) GetContainerStats(ctx context.Context, id string) (*ContainerStats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats, err := c.cli.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %w", err)
	}
	defer stats.Body.Close()

	var statsJSON container.StatsResponse
	if err := decodeStats(stats.Body, &statsJSON); err != nil {
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}

	return calculateStats(id, &statsJSON), nil
}

// StreamContainerStats streams container stats
func (c *Client) StreamContainerStats(ctx context.Context, id string) (<-chan *ContainerStats, <-chan error) {
	statsCh := make(chan *ContainerStats)
	errCh := make(chan error, 1)

	go func() {
		defer close(statsCh)
		defer close(errCh)

		c.mu.RLock()
		resp, err := c.cli.ContainerStats(ctx, id, true)
		c.mu.RUnlock()

		if err != nil {
			errCh <- fmt.Errorf("failed to start stats stream: %w", err)
			return
		}
		defer resp.Body.Close()

		decoder := newStatsDecoder(resp.Body)
		for {
			var statsJSON container.StatsResponse
			if err := decoder.Decode(&statsJSON); err != nil {
				if err == io.EOF || ctx.Err() != nil {
					return
				}
				errCh <- fmt.Errorf("failed to decode stats: %w", err)
				return
			}

			select {
			case statsCh <- calculateStats(id, &statsJSON):
			case <-ctx.Done():
				return
			}
		}
	}()

	return statsCh, errCh
}

// WatchEvents watches for Docker events and returns a channel
func (c *Client) WatchEvents(ctx context.Context) (<-chan ContainerEvent, <-chan error) {
	eventCh := make(chan ContainerEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		c.mu.RLock()
		msgs, errs := c.cli.Events(ctx, events.ListOptions{
			Filters: filters.NewArgs(filters.Arg("type", "container")),
		})
		c.mu.RUnlock()

		for {
			select {
			case msg := <-msgs:
				event := ContainerEvent{
					ID:        msg.Actor.ID,
					Action:    string(msg.Action),
					Name:      msg.Actor.Attributes["name"],
					Image:     msg.Actor.Attributes["image"],
					Project:   msg.Actor.Attributes["com.docker.compose.project"],
					Service:   msg.Actor.Attributes["com.docker.compose.service"],
					Timestamp: time.Unix(msg.Time, msg.TimeNano),
				}
				select {
				case eventCh <- event:
				case <-ctx.Done():
					return
				}
			case err := <-errs:
				if err != nil && ctx.Err() == nil {
					errCh <- err
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventCh, errCh
}

// ContainerEvent represents a Docker container event
type ContainerEvent struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Project   string    `json:"project"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

// containerToInfo converts a Docker container to ContainerInfo
func (c *Client) containerToInfo(ctr types.Container) ContainerInfo {
	name := ""
	if len(ctr.Names) > 0 {
		name = ctr.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
	}

	health := ""
	if strings.Contains(ctr.Status, "health") {
		if strings.Contains(ctr.Status, "unhealthy") {
			health = "unhealthy"
		} else if strings.Contains(ctr.Status, "healthy") {
			health = "healthy"
		} else if strings.Contains(ctr.Status, "starting") {
			health = "starting"
		}
	}

	ports := make([]PortMapping, 0, len(ctr.Ports))
	for _, p := range ctr.Ports {
		ports = append(ports, PortMapping{
			HostIP:        p.IP,
			HostPort:      fmt.Sprintf("%d", p.PublicPort),
			ContainerPort: fmt.Sprintf("%d", p.PrivatePort),
			Protocol:      p.Type,
		})
	}

	return ContainerInfo{
		ID:          ctr.ID[:12],
		Name:        name,
		Image:       ctr.Image,
		ImageID:     ctr.ImageID,
		Status:      ctr.Status,
		State:       ctr.State,
		Health:      health,
		Created:     time.Unix(ctr.Created, 0),
		Ports:       ports,
		Labels:      ctr.Labels,
		ProjectName: ctr.Labels["com.docker.compose.project"],
		ServiceName: ctr.Labels["com.docker.compose.service"],
		ComposeFile: ctr.Labels["com.docker.compose.project.config_files"],
		WorkingDir:  ctr.Labels["com.docker.compose.project.working_dir"],
	}
}

// inspectToInfo converts a Docker container inspect result to ContainerInfo
func (c *Client) inspectToInfo(inspect types.ContainerJSON) ContainerInfo {
	name := inspect.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	health := ""
	if inspect.State.Health != nil {
		health = inspect.State.Health.Status
	}

	ports := make([]PortMapping, 0)
	if inspect.NetworkSettings != nil {
		for port, bindings := range inspect.NetworkSettings.Ports {
			for _, binding := range bindings {
				ports = append(ports, PortMapping{
					HostIP:        binding.HostIP,
					HostPort:      binding.HostPort,
					ContainerPort: port.Port(),
					Protocol:      port.Proto(),
				})
			}
		}
	}

	created, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	return ContainerInfo{
		ID:          inspect.ID[:12],
		Name:        name,
		Image:       inspect.Config.Image,
		ImageID:     inspect.Image,
		Status:      inspect.State.Status,
		State:       inspect.State.Status,
		Health:      health,
		Created:     created,
		Ports:       ports,
		Labels:      inspect.Config.Labels,
		ProjectName: inspect.Config.Labels["com.docker.compose.project"],
		ServiceName: inspect.Config.Labels["com.docker.compose.service"],
		ComposeFile: inspect.Config.Labels["com.docker.compose.project.config_files"],
		WorkingDir:  inspect.Config.Labels["com.docker.compose.project.working_dir"],
	}
}
