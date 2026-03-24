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

// GetContainerPort is not needed for LXD since port mapping is handled differently
func (c *ContainerService) GetContainerPort(ctx context.Context, containerID string, containerPort int) (int, error) {
	return 0, fmt.Errorf("port mapping handled differently in LXD")
}

// StartContainer starts an LXD container for a backend project
func (c *ContainerService) StartContainer(ctx context.Context, projectID, projectName, image string, containerPort int, envVars map[string]string) (*state.Container, error) {
	containerName := fmt.Sprintf("opendeploy-%s", projectName)

	// Check if container with this specific name already exists
	existingContainer, _ := c.db.GetContainerByName(containerName)
	if existingContainer != nil {
		// Check if the container still exists in LXD
		status, err := c.GetContainerStatus(ctx, existingContainer.ContainerID)
		if err == nil && status != "stopped" && status != "unknown" {
			// Container exists, stop it first
			c.logger.Info("stopping existing container before starting new one",
				zap.String("projectId", projectID),
				zap.String("containerName", existingContainer.Name),
			)
			c.StopContainerByName(ctx, containerName)
		}

		// Remove the old container from LXD
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "-f", existingContainer.ContainerID},
			Timeout: 15 * time.Second,
		})

		// Remove from database
		c.db.DeleteContainer(existingContainer.ID)
	}

	// Find an available host port
	hostPort, err := c.FindAvailablePort(8000, 9000)
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	// Launch LXD container
	c.logger.Info("launching lxd container",
		zap.String("projectId", projectID),
		zap.String("containerName", containerName),
		zap.String("image", image),
		zap.Int("hostPort", hostPort),
		zap.Int("containerPort", containerPort),
	)

	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_launch",
		Command: "lxc",
		Args:    []string{"launch", image, containerName},
		Timeout: 60 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to start container: %w", err)
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
		// If output is empty, use containerName as containerID for LXD
		containerID = containerName
	}

	// Add port proxy device
	proxyDeviceName := fmt.Sprintf("proxy-%d", hostPort)
	setupResult, setupErr := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_proxy",
		Command: "lxc",
		Args: []string{
			"config", "device", "add", containerID, proxyDeviceName, "proxy",
			fmt.Sprintf("listen=tcp:0.0.0.0:%d", hostPort),
			fmt.Sprintf("connect=tcp:127.0.0.1:%d", containerPort),
		},
		Timeout: 10 * time.Second,
	})

	if setupErr != nil || !setupResult.Success {
		// Clean up the container if proxy setup failed
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_cleanup",
			Command: "lxc",
			Args:    []string{"delete", "-f", containerID},
			Timeout: 10 * time.Second,
		})
		return nil, fmt.Errorf("failed to setup port proxy: %w", setupErr)
	}

	c.logger.Info("container port proxy configured",
		zap.String("containerId", containerID),
		zap.Int("containerPort", containerPort),
		zap.Int("hostPort", hostPort),
	)

	// Create container record with port mapping
	portMappings := map[string]string{
		"host":      fmt.Sprintf("%d", hostPort),
		"container": fmt.Sprintf("%d", containerPort),
	}
	portJSON, _ := json.Marshal(portMappings)

	container := &state.Container{
		ProjectID:    projectID,
		Name:         containerName,
		Image:        image,
		ContainerID:  containerID,
		Status:       "running",
		PortMappings: string(portJSON),
	}

	if err := c.db.CreateContainer(container); err != nil {
		c.logger.Error("failed to save container to database", zap.Error(err))
	}

	c.logger.Info("container started successfully",
		zap.String("projectId", projectID),
		zap.String("containerName", containerName),
		zap.Int("hostPort", hostPort),
		zap.Int("containerPort", containerPort),
	)

	return container, nil
}

// StopContainer stops a running container without removing it
func (c *ContainerService) StopContainer(ctx context.Context, projectID string) error {
	container, err := c.db.GetContainerByProjectID(projectID)
	if err != nil || container == nil {
		return nil // No container to stop
	}

	c.logger.Info("stopping container",
		zap.String("projectId", projectID),
		zap.String("containerName", container.Name),
	)

	// Stop container (but don't remove it so it can be restarted)
	_, err = c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_stop",
		Command: "lxc",
		Args:    []string{"stop", container.ContainerID},
		Timeout: 30 * time.Second,
	})

	// Update status in database
	container.Status = "stopped"
	c.db.UpdateContainer(container)

	return err
}

func (c *ContainerService) StopContainerByName(ctx context.Context, containerName string) error {
	container, err := c.db.GetContainerByName(containerName)
	if err != nil || container == nil {
		return nil // No container to stop
	}

	c.logger.Info("stopping container by name",
		zap.String("containerName", container.Name),
	)

	// Stop container (but don't remove it so it can be restarted)
	_, err = c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_stop",
		Command: "lxc",
		Args:    []string{"stop", container.ContainerID},
		Timeout: 30 * time.Second,
	})

	// Update status in database
	container.Status = "stopped"
	c.db.UpdateContainer(container)

	return err
}

func (c *ContainerService) RestartContainerByName(ctx context.Context, containerName string) error {
	container, err := c.db.GetContainerByName(containerName)
	if err != nil || container == nil {
		return fmt.Errorf("container not found")
	}

	c.logger.Info("restarting container by name",
		zap.String("containerName", container.Name),
	)

	// Check current status first
	status, _ := c.GetContainerStatus(ctx, container.ContainerID)

	var result *exec.ExecResult
	if status == "stopped" || status == "created" {
		// Container is stopped, use start instead of restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_start",
			Command: "lxc",
			Args:    []string{"start", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	} else {
		// Container is running, use restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_restart",
			Command: "lxc",
			Args:    []string{"restart", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	}

	if err != nil || !result.Success {
		return fmt.Errorf("failed to restart container")
	}

	// Start supervisord inside the container (it dies when container stops)
	// Wait briefly for container networking, then start supervisord
	time.Sleep(2 * time.Second)
	c.runner.Run(ctx, exec.RunOpts{
		JobType: "start_supervisord",
		Command: "lxc",
		Args:    []string{"exec", container.ContainerID, "--", "/bin/sh", "-c", "pgrep supervisord || supervisord -c /etc/supervisord.conf"},
		Timeout: 15 * time.Second,
	})

	// Ensure app service is started (in case it was FATAL from a previous failure)
	time.Sleep(1 * time.Second)
	c.runner.Run(ctx, exec.RunOpts{
		JobType: "start_app_svc",
		Command: "lxc",
		Args:    []string{"exec", container.ContainerID, "--", "/bin/sh", "-c", "supervisorctl reread && supervisorctl update && supervisorctl start app 2>/dev/null || true"},
		Timeout: 15 * time.Second,
	})

	// Update status in database
	container.Status = "running"
	c.db.UpdateContainer(container)

	return nil
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
	if status == "stopped" || status == "created" {
		// Container is stopped, use start instead of restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_start",
			Command: "lxc",
			Args:    []string{"start", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	} else {
		// Container is running, use restart
		result, err = c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_restart",
			Command: "lxc",
			Args:    []string{"restart", container.ContainerID},
			Timeout: 30 * time.Second,
		})
	}

	if err != nil || !result.Success {
		return fmt.Errorf("failed to restart container: %v", err)
	}

	// Start supervisord inside the container (it dies when container stops)
	time.Sleep(2 * time.Second)
	c.runner.Run(ctx, exec.RunOpts{
		JobType: "start_supervisord",
		Command: "lxc",
		Args:    []string{"exec", container.ContainerID, "--", "/bin/sh", "-c", "pgrep supervisord || supervisord -c /etc/supervisord.conf"},
		Timeout: 15 * time.Second,
	})

	// Ensure app service is started (in case it was FATAL from a previous failure)
	time.Sleep(1 * time.Second)
	c.runner.Run(ctx, exec.RunOpts{
		JobType: "start_app_svc",
		Command: "lxc",
		Args:    []string{"exec", container.ContainerID, "--", "/bin/sh", "-c", "supervisorctl reread && supervisorctl update && supervisorctl start app 2>/dev/null || true"},
		Timeout: 15 * time.Second,
	})

	container.Status = "running"
	c.db.UpdateContainer(container)

	return nil
}

// GetContainerStatus checks if a container is running
func (c *ContainerService) GetContainerStatus(ctx context.Context, containerID string) (string, error) {
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_info",
		Command: "lxc",
		Args:    []string{"list", "--format", "csv", "--columns", "s", containerID},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return "stopped", nil
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			output := strings.TrimSpace(line.Text)
			// LXD status is usually just: "RUNNING", "STOPPED", etc. in CSV output
			if strings.Contains(strings.ToUpper(output), "RUNNING") {
				return "running", nil
			} else if strings.Contains(strings.ToUpper(output), "STOPPED") {
				return "stopped", nil
			}
		}
	}

	return "unknown", nil
}

func (c *ContainerService) GetContainerLogs(ctx context.Context, containerID string, lines int) ([]string, error) {
	// LXD doesn't provide direct container log access via command line
	// This is a limitation of the current LXD approach
	if lines <= 0 {
		lines = 100
	}

	// For now, return an empty slice since LXD doesn't expose container logs directly
	return []string{}, nil
}

// RemoveContainer removes all containers for a project and their database records
func (c *ContainerService) RemoveContainer(ctx context.Context, projectID string) error {
	// Find all containers for this project (including full-stack variants)
	allProjectIDs := []string{projectID, projectID + "-frontend", projectID + "-backend"}

	var allContainers []state.Container
	for _, pid := range allProjectIDs {
		containers, err := c.db.ListContainersByProject(pid)
		if err == nil && len(containers) > 0 {
			allContainers = append(allContainers, containers...)
		}
	}

	if len(allContainers) == 0 {
		return nil
	}

	for _, container := range allContainers {
		lxdName := container.ContainerID
		c.logger.Info("removing container",
			zap.String("projectId", projectID),
			zap.String("containerName", container.Name),
			zap.String("lxdName", lxdName),
		)

		// Stop the container
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_stop",
			Command: "lxc",
			Args:    []string{"stop", lxdName, "--force"},
			Timeout: 30 * time.Second,
		})

		// Delete the container
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "--force", lxdName},
			Timeout: 30 * time.Second,
		})

		// Remove from database
		c.db.DeleteContainer(container.ID)
	}

	return nil
}

func (c *ContainerService) ListContainers(projectID string) ([]state.Container, error) {
	return c.db.ListContainersByProject(projectID)
}

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
		JobType: "lxd_inspect",
		Command: "lxc",
		Args:    []string{"list", "--format", "json", containerID},
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

	var state []interface{}
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to parse container state: %w", err)
	}

	// Return the first item from the array (the container info)
	if len(state) > 0 {
		if containerInfo, ok := state[0].(map[string]interface{}); ok {
			return containerInfo, nil
		}
	}

	return nil, fmt.Errorf("container not found in response")
}
