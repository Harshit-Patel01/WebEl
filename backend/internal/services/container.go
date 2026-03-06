package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type ContainerService struct {
	runner *exec.Runner
	db     *state.DB
	cfg    config.DeployConfig
	logger *zap.Logger
}

func NewContainerService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *ContainerService {
	return &ContainerService{
		runner: runner,
		db:     db,
		cfg:    cfg,
		logger: logger,
	}
}

// FindAvailablePort finds an available port in the configured pool range
// It checks both OS-level listeners and existing container port mappings in the database
func (c *ContainerService) FindAvailablePort(startPort, endPort int) (int, error) {
	// Use config defaults if not specified
	if startPort == 0 {
		startPort = c.cfg.PortPoolStart
		if startPort == 0 {
			startPort = 8000
		}
	}
	if endPort == 0 {
		endPort = c.cfg.PortPoolEnd
		if endPort == 0 {
			endPort = 9000
		}
	}

	// Get all existing container port mappings from database
	usedPorts := make(map[int]bool)
	containers, err := c.db.ListContainersByProject("")
	if err == nil {
		for _, container := range containers {
			if container.PortMappings != "" {
				var mapping struct {
					Host      string `json:"host"`
					Container string `json:"container"`
				}
				if json.Unmarshal([]byte(container.PortMappings), &mapping) == nil {
					var port int
					if _, err := fmt.Sscanf(mapping.Host, "%d", &port); err == nil {
						usedPorts[port] = true
					}
				}
			}
		}
	}

	// Find first available port checking both OS listeners and DB mappings
	for port := startPort; port <= endPort; port++ {
		// Skip if already tracked in database
		if usedPorts[port] {
			continue
		}

		// Check if port is available at OS level
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", startPort, endPort)
}

// StartContainer starts a Docker container for a backend project
func (c *ContainerService) StartContainer(ctx context.Context, projectID, projectName, image string, containerPort int, envVars map[string]string) (*state.Container, error) {
	containerName := fmt.Sprintf("opendeploy-%s", projectName)

	// Stop and remove existing container if it exists
	c.StopContainer(ctx, projectID)

	// Find an available host port
	hostPort, err := c.FindAvailablePort(8000, 9000)
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Build docker run command
	args := []string{
		"run",
		"-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"--health-cmd", "exit 0", // Basic health check
		"--health-interval", "10s",
		"--health-timeout", "5s",
		"--health-retries", "3",
	}

	// Add port mapping: hostPort:containerPort
	if containerPort > 0 {
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, containerPort))
	}

	// Add environment variables
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, image)

	c.logger.Info("starting container",
		zap.String("projectId", projectID),
		zap.String("containerName", containerName),
		zap.String("image", image),
		zap.Int("hostPort", hostPort),
		zap.Int("containerPort", containerPort),
	)

	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_run",
		Command: c.cfg.DockerBinary,
		Args:    args,
		Timeout: 30 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to start container")
	}

	// Get container ID from output
	var containerID string
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			containerID = strings.TrimSpace(line.Text)
			break
		}
	}

	if containerID == "" {
		return nil, fmt.Errorf("failed to get container ID")
	}

	// Create container record
	portMappings := map[string]string{"host": fmt.Sprintf("%d", hostPort), "container": fmt.Sprintf("%d", containerPort)}
	portJSON, _ := json.Marshal(portMappings)

	container := &state.Container{
		ProjectID:    projectID,
		Name:         containerName,
		Image:        image,
		ContainerID:  containerID,
		Status:       "starting",
		PortMappings: string(portJSON),
	}

	if err := c.db.CreateContainer(container); err != nil {
		c.logger.Error("failed to save container to database", zap.Error(err))
	}

	// Wait for container to be healthy
	c.logger.Info("waiting for container to be healthy", zap.String("containerId", containerID))

	healthy := false
	for i := 0; i < 30; i++ { // Wait up to 30 seconds
		time.Sleep(1 * time.Second)

		status, err := c.GetContainerStatus(ctx, containerID)
		if err != nil {
			c.logger.Warn("failed to check container status", zap.Error(err))
			continue
		}

		if status == "running" {
			// Check if container is still running (not crashed immediately)
			time.Sleep(2 * time.Second)
			status2, _ := c.GetContainerStatus(ctx, containerID)
			if status2 == "running" {
				healthy = true
				break
			}
		}
	}

	if healthy {
		container.Status = "running"
		c.db.UpdateContainer(container)

		c.logger.Info("container started successfully and is healthy",
			zap.String("projectId", projectID),
			zap.String("containerName", containerName),
			zap.Int("hostPort", hostPort),
			zap.Int("containerPort", containerPort),
		)
	} else {
		container.Status = "unhealthy"
		c.db.UpdateContainer(container)

		// Get logs to help debug
		logs, _ := c.GetContainerLogs(ctx, containerID, 50)
		c.logger.Error("container failed health check",
			zap.String("projectId", projectID),
			zap.String("containerName", containerName),
			zap.Strings("recentLogs", logs),
		)

		return container, fmt.Errorf("container started but failed health check")
	}

	return container, nil
}

// StopContainer stops a running container
func (c *ContainerService) StopContainer(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return nil // No container to stop
	}

	c.logger.Info("stopping container",
		zap.String("projectId", projectID),
		zap.String("containerName", container.Name),
	)

	// Stop container
	_, err = c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_stop",
		Command: c.cfg.DockerBinary,
		Args:    []string{"stop", container.ContainerID},
		Timeout: 30 * time.Second,
	})

	// Remove container
	c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_rm",
		Command: c.cfg.DockerBinary,
		Args:    []string{"rm", "-f", container.ContainerID},
		Timeout: 15 * time.Second,
	})

	// Update status in database
	container.Status = "stopped"
	c.db.UpdateContainer(container)

	return err
}

// RestartContainer restarts a container
func (c *ContainerService) RestartContainer(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return fmt.Errorf("container not found")
	}

	c.logger.Info("restarting container",
		zap.String("projectId", projectID),
		zap.String("containerName", container.Name),
	)

	// Check current status first
	status, _ := c.GetContainerStatus(ctx, container.ContainerID)

	var result *exec.ExecResult
	if status == "exited" || status == "stopped" || status == "created" {
		// Container is stopped, use start instead of restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "docker_start",
			Command: c.cfg.DockerBinary,
			Args:    []string{"start", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	} else {
		// Container is running or paused, use restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "docker_restart",
			Command: c.cfg.DockerBinary,
			Args:    []string{"restart", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	}

	if err != nil || !result.Success {
		return fmt.Errorf("failed to restart container: %v", err)
	}

	container.Status = "running"
	c.db.UpdateContainer(container)

	return nil
}

// GetContainerStatus checks if a container is running
func (c *ContainerService) GetContainerStatus(ctx context.Context, containerID string) (string, error) {
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_inspect",
		Command: c.cfg.DockerBinary,
		Args:    []string{"inspect", "--format", "{{.State.Status}}", containerID},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return "stopped", nil
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			return strings.TrimSpace(line.Text), nil
		}
	}

	return "unknown", nil
}

// GetContainerLogs retrieves logs from a container
func (c *ContainerService) GetContainerLogs(ctx context.Context, containerID string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 100
	}

	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_logs",
		Command: c.cfg.DockerBinary,
		Args:    []string{"logs", "--tail", fmt.Sprintf("%d", lines), containerID},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to get container logs")
	}

	var logs []string
	for _, line := range result.Lines {
		logs = append(logs, line.Text)
	}

	return logs, nil
}

// RemoveContainer removes a container and its database record
func (c *ContainerService) RemoveContainer(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return nil
	}

	// Stop and remove from Docker
	c.StopContainer(ctx, projectID)

	// Remove from database
	return c.db.DeleteContainer(container.ID)
}

// ListContainers lists all containers for a project
func (c *ContainerService) ListContainers(projectID string) ([]state.Container, error) {
	return c.db.ListContainersByProject(projectID)
}

// SyncContainerStatus syncs the container status from Docker to database
func (c *ContainerService) SyncContainerStatus(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return nil
	}

	status, err := c.GetContainerStatus(ctx, container.ContainerID)
	if err != nil {
		return err
	}

	if container.Status != status {
		container.Status = status
		return c.db.UpdateContainer(container)
	}

	return nil
}

// MonitorContainerHealth continuously monitors container health and restarts if needed
func (c *ContainerService) MonitorContainerHealth(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return fmt.Errorf("container not found")
	}

	status, err := c.GetContainerStatus(ctx, container.ContainerID)
	if err != nil {
		return err
	}

	// If container is not running, try to restart it
	if status != "running" {
		c.logger.Warn("container is not running, attempting restart",
			zap.String("projectId", projectID),
			zap.String("status", status),
		)

		err := c.RestartContainer(ctx, projectID)
		if err != nil {
			c.logger.Error("failed to restart unhealthy container",
				zap.String("projectId", projectID),
				zap.Error(err),
			)
			return err
		}

		c.logger.Info("successfully restarted unhealthy container",
			zap.String("projectId", projectID),
		)
	}

	return nil
}

// GetContainerHealth returns detailed health information about a container
func (c *ContainerService) GetContainerHealth(ctx context.Context, containerID string) (map[string]interface{}, error) {
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_inspect",
		Command: c.cfg.DockerBinary,
		Args:    []string{"inspect", "--format", "{{json .State}}", containerID},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to inspect container")
	}

	var stateJSON string
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			stateJSON = line.Text
			break
		}
	}

	var state map[string]interface{}
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to parse container state: %w", err)
	}

	return state, nil
}
