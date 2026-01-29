package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// MockClient provides a mock Docker client for development without Docker
type MockClient struct {
	mu         sync.RWMutex
	containers map[string]*ContainerInfo
	eventCh    chan ContainerEvent
	eventSubs  []chan ContainerEvent
}

// NewMockClient creates a new mock Docker client with demo containers
func NewMockClient() *MockClient {
	m := &MockClient{
		containers: make(map[string]*ContainerInfo),
		eventCh:    make(chan ContainerEvent, 100),
	}
	m.initDemoContainers()
	return m
}

func (m *MockClient) initDemoContainers() {
	now := time.Now()

	demoContainers := []ContainerInfo{
		{
			ID:          "abc123def456",
			Name:        "webapp-web-1",
			Image:       "nginx:alpine",
			ImageID:     "sha256:a1b2c3d4e5f6",
			Status:      "Up 2 hours",
			State:       "running",
			Health:      "healthy",
			Created:     now.Add(-2 * time.Hour),
			Ports:       []PortMapping{{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}},
			Labels:      map[string]string{"com.docker.compose.project": "webapp", "com.docker.compose.service": "web"},
			ProjectName: "webapp",
			ServiceName: "web",
			WorkingDir:  "/projects/webapp",
		},
		{
			ID:          "bcd234efg567",
			Name:        "webapp-api-1",
			Image:       "node:18-alpine",
			ImageID:     "sha256:b2c3d4e5f6a7",
			Status:      "Up 2 hours",
			State:       "running",
			Health:      "",
			Created:     now.Add(-2 * time.Hour),
			Ports:       []PortMapping{{HostIP: "0.0.0.0", HostPort: "3000", ContainerPort: "3000", Protocol: "tcp"}},
			Labels:      map[string]string{"com.docker.compose.project": "webapp", "com.docker.compose.service": "api"},
			ProjectName: "webapp",
			ServiceName: "api",
			WorkingDir:  "/projects/webapp",
		},
		{
			ID:          "cde345fgh678",
			Name:        "webapp-db-1",
			Image:       "postgres:15",
			ImageID:     "sha256:c3d4e5f6a7b8",
			Status:      "Up 2 hours",
			State:       "running",
			Health:      "healthy",
			Created:     now.Add(-2 * time.Hour),
			Ports:       []PortMapping{{HostIP: "127.0.0.1", HostPort: "5432", ContainerPort: "5432", Protocol: "tcp"}},
			Labels:      map[string]string{"com.docker.compose.project": "webapp", "com.docker.compose.service": "db"},
			ProjectName: "webapp",
			ServiceName: "db",
			WorkingDir:  "/projects/webapp",
		},
		{
			ID:          "def456ghi789",
			Name:        "monitoring-prometheus-1",
			Image:       "prom/prometheus",
			ImageID:     "sha256:d4e5f6a7b8c9",
			Status:      "Up 1 hour",
			State:       "running",
			Health:      "",
			Created:     now.Add(-1 * time.Hour),
			Ports:       []PortMapping{{HostIP: "0.0.0.0", HostPort: "9090", ContainerPort: "9090", Protocol: "tcp"}},
			Labels:      map[string]string{"com.docker.compose.project": "monitoring", "com.docker.compose.service": "prometheus"},
			ProjectName: "monitoring",
			ServiceName: "prometheus",
			WorkingDir:  "/projects/monitoring",
		},
		{
			ID:          "efg567hij890",
			Name:        "monitoring-grafana-1",
			Image:       "grafana/grafana",
			ImageID:     "sha256:e5f6a7b8c9d0",
			Status:      "Up 1 hour",
			State:       "running",
			Health:      "",
			Created:     now.Add(-1 * time.Hour),
			Ports:       []PortMapping{{HostIP: "0.0.0.0", HostPort: "3001", ContainerPort: "3000", Protocol: "tcp"}},
			Labels:      map[string]string{"com.docker.compose.project": "monitoring", "com.docker.compose.service": "grafana"},
			ProjectName: "monitoring",
			ServiceName: "grafana",
			WorkingDir:  "/projects/monitoring",
		},
	}

	for _, c := range demoContainers {
		cpy := c
		m.containers[c.ID] = &cpy
	}
}

// Close closes the mock client
func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.eventCh)
	return nil
}

// ListContainers returns containers, optionally filtered by project
func (m *MockClient) ListContainers(ctx context.Context, projectName string) ([]ContainerInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ContainerInfo
	for _, c := range m.containers {
		if projectName == "" || c.ProjectName == projectName {
			result = append(result, *c)
		}
	}
	return result, nil
}

// GetContainer returns a specific container by ID
func (m *MockClient) GetContainer(ctx context.Context, id string) (*ContainerInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Handle both full ID and short ID lookups
	for cid, c := range m.containers {
		if cid == id || strings.HasPrefix(cid, id) {
			cpy := *c
			return &cpy, nil
		}
	}
	return nil, fmt.Errorf("container not found: %s", id)
}

// StartContainer starts a container
func (m *MockClient) StartContainer(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.findContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %s", id)
	}

	c.State = "running"
	c.Status = "Up Less than a second"

	m.emitEvent(c, "start")
	return nil
}

// StopContainer stops a container
func (m *MockClient) StopContainer(ctx context.Context, id string, timeout int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.findContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %s", id)
	}

	c.State = "exited"
	c.Status = "Exited (0) Less than a second ago"

	m.emitEvent(c, "stop")
	return nil
}

// RestartContainer restarts a container
func (m *MockClient) RestartContainer(ctx context.Context, id string, timeout int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.findContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %s", id)
	}

	c.State = "running"
	c.Status = "Up Less than a second"

	m.emitEvent(c, "restart")
	return nil
}

// GetContainerLogs returns fake log output
func (m *MockClient) GetContainerLogs(ctx context.Context, id string, tail string, follow bool) (io.ReadCloser, error) {
	m.mu.RLock()
	c := m.findContainerRLocked(id)
	m.mu.RUnlock()

	if c == nil {
		return nil, fmt.Errorf("container not found: %s", id)
	}

	if follow {
		return newMockLogStream(ctx, c.Name), nil
	}

	return newMockLogBuffer(c.Name, 100), nil
}

// GetContainerStats returns randomized but realistic stats
func (m *MockClient) GetContainerStats(ctx context.Context, id string) (*ContainerStats, error) {
	m.mu.RLock()
	c := m.findContainerRLocked(id)
	m.mu.RUnlock()

	if c == nil {
		return nil, fmt.Errorf("container not found: %s", id)
	}

	if c.State != "running" {
		return &ContainerStats{ID: c.ID}, nil
	}

	// Generate realistic random stats
	memoryLimit := uint64(512 * 1024 * 1024) // 512MB
	memoryUsage := uint64(100+rand.Intn(300)) * 1024 * 1024
	if memoryUsage > memoryLimit {
		memoryUsage = memoryLimit - uint64(rand.Intn(50))*1024*1024
	}

	return &ContainerStats{
		ID:            c.ID,
		CPUPercent:    5.0 + rand.Float64()*15.0,
		MemoryUsage:   memoryUsage,
		MemoryLimit:   memoryLimit,
		MemoryPercent: float64(memoryUsage) / float64(memoryLimit) * 100,
		NetworkRx:     uint64(rand.Intn(10000000)),
		NetworkTx:     uint64(rand.Intn(5000000)),
	}, nil
}

// WatchEvents returns channels for container events
func (m *MockClient) WatchEvents(ctx context.Context) (<-chan ContainerEvent, <-chan error) {
	eventCh := make(chan ContainerEvent, 10)
	errCh := make(chan error, 1)

	m.mu.Lock()
	m.eventSubs = append(m.eventSubs, eventCh)
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		m.mu.Lock()
		for i, ch := range m.eventSubs {
			if ch == eventCh {
				m.eventSubs = append(m.eventSubs[:i], m.eventSubs[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		close(eventCh)
		close(errCh)
	}()

	return eventCh, errCh
}

// SetContainerState allows external code (like MockComposeClient) to change container state
func (m *MockClient) SetContainerState(id, state, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.findContainer(id)
	if c != nil {
		c.State = state
		c.Status = status
		action := "start"
		if state == "exited" {
			action = "stop"
		}
		m.emitEvent(c, action)
	}
}

// SetAllContainersState sets state for all containers in a project
func (m *MockClient) SetAllContainersState(projectName, state, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.containers {
		if c.ProjectName == projectName {
			c.State = state
			c.Status = status
			action := "start"
			if state == "exited" {
				action = "stop"
			}
			m.emitEvent(c, action)
		}
	}
}

func (m *MockClient) findContainer(id string) *ContainerInfo {
	for cid, c := range m.containers {
		if cid == id || strings.HasPrefix(cid, id) {
			return c
		}
	}
	return nil
}

func (m *MockClient) findContainerRLocked(id string) *ContainerInfo {
	for cid, c := range m.containers {
		if cid == id || strings.HasPrefix(cid, id) {
			return c
		}
	}
	return nil
}

func (m *MockClient) emitEvent(c *ContainerInfo, action string) {
	event := ContainerEvent{
		ID:        c.ID,
		Action:    action,
		Name:      c.Name,
		Image:     c.Image,
		Project:   c.ProjectName,
		Service:   c.ServiceName,
		Timestamp: time.Now(),
	}

	for _, ch := range m.eventSubs {
		select {
		case ch <- event:
		default:
		}
	}
}

// mockLogBuffer provides static log content
type mockLogBuffer struct {
	*bytes.Buffer
}

func newMockLogBuffer(containerName string, lines int) *mockLogBuffer {
	var buf bytes.Buffer
	now := time.Now()

	messages := []string{
		"Server started successfully",
		"Listening on port 8080",
		"Connection established",
		"Request received: GET /api/health",
		"Response sent: 200 OK",
		"Processing request...",
		"Database query executed in 12ms",
		"Cache hit for key: user_123",
		"Background job completed",
		"Metrics exported successfully",
	}

	for i := 0; i < lines; i++ {
		ts := now.Add(-time.Duration(lines-i) * time.Second).Format(time.RFC3339Nano)
		msg := messages[i%len(messages)]
		buf.WriteString(fmt.Sprintf("%s %s | %s\n", ts, containerName, msg))
	}

	return &mockLogBuffer{Buffer: &buf}
}

func (m *mockLogBuffer) Close() error {
	return nil
}

// Verify MockClient implements DockerClient
var _ DockerClient = (*MockClient)(nil)

// mockLogStream provides streaming log output
type mockLogStream struct {
	ctx           context.Context
	containerName string
	reader        *io.PipeReader
	writer        *io.PipeWriter
}

func newMockLogStream(ctx context.Context, containerName string) *mockLogStream {
	r, w := io.Pipe()
	s := &mockLogStream{
		ctx:           ctx,
		containerName: containerName,
		reader:        r,
		writer:        w,
	}
	go s.generate()
	return s
}

func (s *mockLogStream) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *mockLogStream) Close() error {
	s.writer.Close()
	return s.reader.Close()
}

func (s *mockLogStream) generate() {
	defer s.writer.Close()

	messages := []string{
		"Handling incoming request",
		"Query executed successfully",
		"Response time: 45ms",
		"Connection pool: 5 active",
		"Health check passed",
		"Metrics collected",
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ts := time.Now().Format(time.RFC3339Nano)
			msg := messages[rand.Intn(len(messages))]
			line := fmt.Sprintf("%s %s | %s\n", ts, s.containerName, msg)
			if _, err := s.writer.Write([]byte(line)); err != nil {
				return
			}
		}
	}
}
