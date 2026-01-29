package docker

import (
	"context"
	"io"
)

// DockerClient defines the interface for Docker container operations
type DockerClient interface {
	Close() error
	ListContainers(ctx context.Context, projectName string) ([]ContainerInfo, error)
	GetContainer(ctx context.Context, id string) (*ContainerInfo, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout int) error
	RestartContainer(ctx context.Context, id string, timeout int) error
	GetContainerLogs(ctx context.Context, id string, tail string, follow bool) (io.ReadCloser, error)
	GetContainerStats(ctx context.Context, id string) (*ContainerStats, error)
	WatchEvents(ctx context.Context) (<-chan ContainerEvent, <-chan error)
}

// ComposeExecutor defines the interface for Docker Compose operations
type ComposeExecutor interface {
	Up(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error)
	Down(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error)
	Pull(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error)
	Restart(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error)
	Update(ctx context.Context, projectDir string, outputCh chan<- ComposeOutput) (*ComposeResult, error)
}

// Verify that concrete types implement the interfaces
var (
	_ DockerClient    = (*Client)(nil)
	_ ComposeExecutor = (*ComposeClient)(nil)
)
