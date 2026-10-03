package ws

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
	jobSubs map[string]map[string]bool // jobID -> set of clientIDs
	logger  *zap.Logger

	register   chan *Client
	unregister chan *Client
}

func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		jobSubs:    make(map[string]map[string]bool),
		logger:     logger,
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()
			h.logger.Debug("client connected", zap.String("clientId", client.ID))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
				// Remove from all job subscriptions
				for jobID, subs := range h.jobSubs {
					delete(subs, client.ID)
					if len(subs) == 0 {
						delete(h.jobSubs, jobID)
					}
				}
			}
			h.mu.Unlock()
			h.logger.Debug("client disconnected", zap.String("clientId", client.ID))
		}
	}
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// SubscribeToJob subscribes a client to a specific job's updates.
func (h *Hub) SubscribeToJob(clientID, jobID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.jobSubs[jobID] == nil {
		h.jobSubs[jobID] = make(map[string]bool)
	}
	h.jobSubs[jobID][clientID] = true
}

// BroadcastToJob sends a message to all clients subscribed to a job.
func (h *Hub) BroadcastToJob(jobID string, msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal ws message", zap.Error(err))
		return
	}

	h.mu.RLock()
	subs, ok := h.jobSubs[jobID]
	if !ok || len(subs) == 0 {
		h.mu.RUnlock()
		return
	}

	for clientID := range subs {
		if client, exists := h.clients[clientID]; exists {
			select {
			case client.Send <- data:
			default:
				// Client buffer full, skip
			}
		}
	}
	h.mu.RUnlock()
}

// BroadcastAll sends a message to all connected clients.
func (h *Hub) BroadcastAll(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal ws message", zap.Error(err))
		return
	}

	h.mu.RLock()
	for _, client := range h.clients {
		select {
		case client.Send <- data:
		default:
		}
	}
	h.mu.RUnlock()
}

// HandleClientMessage processes incoming WebSocket messages from clients.
func (h *Hub) HandleClientMessage(clientID string, raw []byte) {
	var msg struct {
		Type     string `json:"type"`
		JobID    string `json:"jobId,omitempty"`
		DeployID string `json:"deployId,omitempty"`
		Service  string `json:"service,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		h.logger.Warn("invalid ws message", zap.Error(err))
		return
	}

	switch msg.Type {
	case "subscribe_job":
		if msg.JobID != "" {
			h.SubscribeToJob(clientID, msg.JobID)
			h.logger.Debug("client subscribed to job",
				zap.String("clientId", clientID),
				zap.String("jobId", msg.JobID),
			)
		}
	case "subscribe_deploy":
		if msg.DeployID != "" {
			h.SubscribeToJob(clientID, msg.DeployID)
			h.logger.Debug("client subscribed to deploy",
				zap.String("clientId", clientID),
				zap.String("deployId", msg.DeployID),
			)
		}
	case "cancel_job":
		// Handled at the API layer
	case "ping":
		h.mu.RLock()
		if client, ok := h.clients[clientID]; ok {
			pong, _ := json.Marshal(map[string]string{"type": "pong"})
			select {
			case client.Send <- pong:
			default:
			}
		}
		h.mu.RUnlock()
	}
}

// StartStatsBroadcaster periodically broadcasts system stats to all clients.
func (h *Hub) StartStatsBroadcaster(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		stats := collectSystemStats()
		h.BroadcastAll(map[string]interface{}{
			"type":   "system_stats",
			"cpu":    stats.CPU,
			"ram":    stats.RAM,
			"temp":   stats.Temp,
			"uptime": stats.Uptime,
		})
	}
}

type systemStats struct {
	CPU    float64 `json:"cpu"`
	RAM    float64 `json:"ram"`
	Temp   float64 `json:"temp"`
	Uptime string  `json:"uptime"`
}

func collectSystemStats() systemStats {
	stats := systemStats{}

	if runtime.GOOS != "linux" {
		return stats
	}

	// CPU usage from /proc/stat
	stats.CPU = readCPUUsage()

	// RAM from /proc/meminfo
	stats.RAM = readRAMUsage()

	// Temperature
	stats.Temp = readCPUTemp()

	// Uptime
	stats.Uptime = readUptime()

	return stats
}

func readCPUUsage() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 5 {
		return 0
	}
	// Simple approximation: user + system / total
	var vals [4]float64
	for i := 1; i < 5 && i < len(fields); i++ {
		v, _ := strconv.ParseFloat(fields[i], 64)
		vals[i-1] = v
	}
	total := vals[0] + vals[1] + vals[2] + vals[3]
	if total == 0 {
		return 0
	}
	idle := vals[3]
	return ((total - idle) / total) * 100
}

func readRAMUsage() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, available float64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseFloat(fields[1], 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemAvailable:":
			available = val
		}
	}
	if total == 0 {
		return 0
	}
	return ((total - available) / total) * 100
}

func readCPUTemp() float64 {
	data, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0
	}
	return v / 1000
}

func readUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "unknown"
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "unknown"
	}
	d := time.Duration(secs) * time.Second
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
