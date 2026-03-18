package services

import (
	"fmt"
	"sync"

	"github.com/opendeploy/opendeploy/internal/state"
)

// PortAllocator manages port allocation from a configured pool
type PortAllocator struct {
	db        *state.DB
	poolStart int
	poolEnd   int
	mu        sync.Mutex
}

// NewPortAllocator creates a new port allocator
func NewPortAllocator(db *state.DB, poolStart, poolEnd int) *PortAllocator {
	return &PortAllocator{
		db:        db,
		poolStart: poolStart,
		poolEnd:   poolEnd,
	}
}

// AllocatePort allocates a unique port for a project
// Returns the allocated port or an error if no ports are available
func (pa *PortAllocator) AllocatePort(projectID string) (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	// Get all currently allocated ports
	allocatedPorts, err := pa.getAllocatedPorts()
	if err != nil {
		return 0, fmt.Errorf("failed to get allocated ports: %w", err)
	}

	// Find first available port in the pool
	for port := pa.poolStart; port <= pa.poolEnd; port++ {
		if !allocatedPorts[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in pool (range: %d-%d)", pa.poolStart, pa.poolEnd)
}

// getAllocatedPorts returns a map of all currently allocated ports
func (pa *PortAllocator) getAllocatedPorts() (map[int]bool, error) {
	allocated := make(map[int]bool)

	// Get ports from projects
	projects, err := pa.db.ListProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.LocalPort > 0 {
			allocated[p.LocalPort] = true
		}
	}

	// Get ports from containers
	containers, err := pa.db.ListAllContainers()
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		// Parse port mappings to extract host port
		hostPort, _, err := parsePortMapping(c.PortMappings)
		if err == nil && hostPort > 0 {
			allocated[hostPort] = true
		}
	}

	// Get ports from tunnel routes
	tunnelConfig, err := pa.db.GetTunnelConfig()
	if err == nil && tunnelConfig != nil {
		routes, err := pa.db.ListTunnelRoutes(tunnelConfig.TunnelID)
		if err == nil {
			for _, r := range routes {
				if r.LocalPort > 0 {
					allocated[r.LocalPort] = true
				}
			}
		}
	}

	// Reserve port 80 (nginx) and 3000 (dashboard)
	allocated[80] = true
	allocated[3000] = true

	return allocated, nil
}

// ReleasePort releases a port back to the pool (currently a no-op since we track via DB)
func (pa *PortAllocator) ReleasePort(port int) error {
	// Port is automatically released when project/container is deleted
	return nil
}

// GetNextAvailablePort returns the next available port without allocating it
func (pa *PortAllocator) GetNextAvailablePort() (int, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	allocatedPorts, err := pa.getAllocatedPorts()
	if err != nil {
		return 0, fmt.Errorf("failed to get allocated ports: %w", err)
	}

	for port := pa.poolStart; port <= pa.poolEnd; port++ {
		if !allocatedPorts[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in pool")
}
