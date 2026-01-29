package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Event represents a server-sent event
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Client represents a connected SSE client
type Client struct {
	ID       string
	Events   chan Event
	Done     chan struct{}
	LastSeen time.Time
}

// Broker manages SSE connections and event distribution
type Broker struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan Event
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewBroker creates a new SSE broker
func NewBroker() *Broker {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Broker{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Event, 256),
		ctx:        ctx,
		cancel:     cancel,
	}

	go b.run()
	return b
}

// run is the main broker loop
func (b *Broker) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client.ID] = client
			b.mu.Unlock()
			log.Printf("SSE client connected: %s (total: %d)", client.ID, len(b.clients))

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client.ID]; ok {
				delete(b.clients, client.ID)
				close(client.Events)
			}
			b.mu.Unlock()
			log.Printf("SSE client disconnected: %s (total: %d)", client.ID, len(b.clients))

		case event := <-b.broadcast:
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.Events <- event:
				default:
					// Client buffer full, skip this event
					log.Printf("SSE client %s buffer full, skipping event", client.ID)
				}
			}
			b.mu.RUnlock()

		case <-b.ctx.Done():
			b.mu.Lock()
			for _, client := range b.clients {
				close(client.Events)
			}
			b.clients = make(map[string]*Client)
			b.mu.Unlock()
			return
		}
	}
}

// Close shuts down the broker
func (b *Broker) Close() {
	b.cancel()
}

// Broadcast sends an event to all connected clients
func (b *Broker) Broadcast(eventType string, data interface{}) {
	select {
	case b.broadcast <- Event{Type: eventType, Data: data}:
	default:
		log.Printf("Broadcast channel full, dropping event: %s", eventType)
	}
}

// BroadcastJSON sends a JSON-serializable event to all clients
func (b *Broker) BroadcastJSON(eventType string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	b.Broadcast(eventType, string(jsonData))
	return nil
}

// ClientCount returns the number of connected clients
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ServeHTTP handles SSE connections
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")

	// Disable write deadline for SSE connections (they are long-lived)
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("Warning: could not disable write deadline: %v", err)
	}

	// Create flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create client
	clientID := fmt.Sprintf("%d", time.Now().UnixNano())
	client := &Client{
		ID:       clientID,
		Events:   make(chan Event, 64),
		Done:     make(chan struct{}),
		LastSeen: time.Now(),
	}

	// Register client
	b.register <- client

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"clientId\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	// Clean up on disconnect
	defer func() {
		b.unregister <- client
	}()

	// Keep-alive ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Stream events
	for {
		select {
		case event, ok := <-client.Events:
			if !ok {
				return
			}

			data, err := formatEventData(event.Data)
			if err != nil {
				log.Printf("Failed to format event data: %v", err)
				continue
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()

		case <-ticker.C:
			// Send keep-alive
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			return
		}
	}
}

// formatEventData formats event data for SSE
func formatEventData(data interface{}) (string, error) {
	switch d := data.(type) {
	case string:
		return d, nil
	case []byte:
		return string(d), nil
	default:
		jsonData, err := json.Marshal(d)
		if err != nil {
			return "", err
		}
		return string(jsonData), nil
	}
}

// ContainerStatusEvent represents a container status change
type ContainerStatusEvent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	State   string `json:"state"`
	Health  string `json:"health"`
	Project string `json:"project"`
	Service string `json:"service"`
}

// ContainerStatsEvent represents container resource usage
type ContainerStatsEvent struct {
	ID            string  `json:"id"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsage   uint64  `json:"memoryUsage"`
	MemoryLimit   uint64  `json:"memoryLimit"`
	MemoryPercent float64 `json:"memoryPercent"`
}

// LogLineEvent represents a log line
type LogLineEvent struct {
	ContainerID string    `json:"containerId"`
	Container   string    `json:"container"`
	Line        string    `json:"line"`
	Stream      string    `json:"stream"`
	Timestamp   time.Time `json:"timestamp"`
}

// ProjectStatusEvent represents a project status change
type ProjectStatusEvent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Running int    `json:"running"`
	Total   int    `json:"total"`
}

// ComposeOutputEvent represents compose command output
type ComposeOutputEvent struct {
	ProjectID string `json:"projectId"`
	Operation string `json:"operation"`
	Line      string `json:"line"`
	Stream    string `json:"stream"`
}

// ComposeCompleteEvent represents compose command completion
type ComposeCompleteEvent struct {
	ProjectID string `json:"projectId"`
	Operation string `json:"operation"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
}
