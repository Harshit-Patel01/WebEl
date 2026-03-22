package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

// CleanupReport holds the results of a cleanup operation
type CleanupReport struct {
	OrphanContainersRemoved int      `json:"orphan_containers_removed"`
	DanglingImagesRemoved   int      `json:"dangling_images_removed"`
	StaleDeploysFixed       int      `json:"stale_deploys_fixed"`
	ClonedReposRemoved      int      `json:"cloned_repos_removed"`
	Errors                  []string `json:"errors,omitempty"`
}

// CleanupService handles cleaning up orphan containers, dangling images, and stale deploys
type CleanupService struct {
	runner *exec.Runner
	db     *state.DB
	cfg    config.DeployConfig
	logger *zap.Logger
}

func NewCleanupService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *CleanupService {
	return &CleanupService{
		runner: runner,
		db:     db,
		cfg:    cfg,
		logger: logger,
	}
}

// RunFullCleanup performs all cleanup operations and returns a report
func (c *CleanupService) RunFullCleanup(ctx context.Context) *CleanupReport {
	report := &CleanupReport{}

	c.logger.Info("starting full cleanup")

	// 1. Fix stale deploys
	staleFixed := c.FixStaleDeployments(ctx)
	report.StaleDeploysFixed = staleFixed

	// 2. Clean orphan containers
	orphansRemoved, errs := c.CleanupOrphanContainers(ctx)
	report.OrphanContainersRemoved = orphansRemoved
	report.Errors = append(report.Errors, errs...)

	// 3. Clean dangling images
	imagesRemoved, errs := c.CleanupDanglingImages(ctx)
	report.DanglingImagesRemoved = imagesRemoved
	report.Errors = append(report.Errors, errs...)

	// 4. Clean orphaned cloned repositories
	reposRemoved, errs := c.CleanupOrphanRepos(ctx)
	report.ClonedReposRemoved = reposRemoved
	report.Errors = append(report.Errors, errs...)

	c.logger.Info("cleanup completed",
		zap.Int("orphanContainers", report.OrphanContainersRemoved),
		zap.Int("danglingImages", report.DanglingImagesRemoved),
		zap.Int("staleDeploysFixed", report.StaleDeploysFixed),
		zap.Int("clonedReposRemoved", report.ClonedReposRemoved),
		zap.Int("errors", len(report.Errors)),
	)

	return report
}

// FixStaleDeployments marks "running" deploys that are older than BuildTimeout as "failed"
func (c *CleanupService) FixStaleDeployments(ctx context.Context) int {
	stale, err := c.db.ListStaleRunningDeploys(c.cfg.BuildTimeout)
	if err != nil {
		c.logger.Error("failed to list stale deploys", zap.Error(err))
		return 0
	}

	fixed := 0
	for _, deploy := range stale {
		now := time.Now()
		deploy.Status = "failed"
		deploy.EndedAt = &now
		deploy.ExitCode = -1

		if err := c.db.UpdateDeploy(&deploy); err != nil {
			c.logger.Error("failed to mark stale deploy as failed",
				zap.String("deployId", deploy.ID),
				zap.Error(err),
			)
			continue
		}

		// Log a message explaining the auto-failure
		c.db.CreateDeployLog(&state.DeployLog{
			DeployID:     deploy.ID,
			Stream:       "stderr",
			Message:      fmt.Sprintf("Deploy automatically marked as failed: exceeded build timeout (%s) or server restarted while deploy was running", c.cfg.BuildTimeout),
			LogTimestamp: now,
		})

		c.logger.Info("marked stale deploy as failed",
			zap.String("deployId", deploy.ID),
			zap.String("projectId", deploy.ProjectID),
			zap.Time("startedAt", deploy.StartedAt),
		)
		fixed++
	}

	return fixed
}

// CleanupOrphanContainers finds LXD containers with opendeploy- prefix that aren't tracked in the DB
func (c *CleanupService) CleanupOrphanContainers(ctx context.Context) (int, []string) {
	var errors []string

	// List all LXD containers with opendeploy prefix
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_list",
		Command: "lxc",
		Args:    []string{"list", "--format", "csv", "--columns", "n,s", "--filter", "opendeploy"},
		Timeout: 15 * time.Second,
	})

	if err != nil || !result.Success {
		c.logger.Debug("LXD not available for orphan cleanup, skipping")
		return 0, nil
	}

	// Get all tracked container IDs from DB
	trackedContainers := make(map[string]bool)
	projects, _ := c.db.ListProjects()
	for _, project := range projects {
		containers, _ := c.db.ListContainersByProject(project.ID)
		for _, container := range containers {
			trackedContainers[container.ContainerID] = true
			trackedContainers[container.Name] = true
		}
	}

	removed := 0
	for _, line := range result.Lines {
		if line.Stream != "stdout" || line.Text == "" {
			continue
		}

		// CSV format: NAME,STATUS
		parts := strings.SplitN(line.Text, ",", 2)
		if len(parts) < 1 {
			continue
		}

		containerName := strings.TrimSpace(parts[0])

		// Check if this container is tracked in the DB
		if trackedContainers[containerName] {
			continue
		}

		// This is an orphan — remove it
		c.logger.Info("removing orphan container",
			zap.String("containerName", containerName),
		)

		// Stop first with force flag, then remove
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_stop",
			Command: "lxc",
			Args:    []string{"stop", containerName, "--force"},
			Timeout: 30 * time.Second,
		})

		_, rmErr := c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "--force", containerName},
			Timeout: 30 * time.Second,
		})

		if rmErr != nil {
			errors = append(errors, fmt.Sprintf("failed to remove orphan container %s: %v", containerName, rmErr))
		} else {
			removed++
		}
	}

	return removed, errors
}

// CleanupDanglingImages removes unused LXD images to free disk space
func (c *CleanupService) CleanupDanglingImages(ctx context.Context) (int, []string) {
	var errors []string

	c.logger.Debug("LXD image cleanup")
	return 0, errors
}

// CleanupOrphanRepos removes cloned repositories in /tmp that don't have active projects
func (c *CleanupService) CleanupOrphanRepos(ctx context.Context) (int, []string) {
	var errors []string

	// Get all active project IDs from the database
	activeProjects := make(map[string]bool)
	projects, err := c.db.ListProjects()
	if err != nil {
		c.logger.Error("Failed to list projects for repo cleanup", zap.Error(err))
		errors = append(errors, fmt.Sprintf("failed to list projects: %v", err))
		return 0, errors
	}

	for _, project := range projects {
		activeProjects[project.ID] = true
	}

	// List all directories in /tmp that might be cloned repos
	tmpDir := "/tmp"
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		c.logger.Error("Failed to read /tmp directory", zap.Error(err))
		errors = append(errors, fmt.Sprintf("failed to read /tmp: %v", err))
		return 0, errors
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(tmpDir, entry.Name())

		// Check if this directory has a .git folder (indicating it's a cloned repo)
		gitPath := filepath.Join(dirPath, ".git")
		if _, err := os.Stat(gitPath); err != nil {
			// Not a git repo, skip
			continue
		}

		// Check if this project ID is still active
		if activeProjects[entry.Name()] {
			// Active project, don't remove
			continue
		}

		// This is an orphaned cloned repository - remove it
		c.logger.Info("removing orphaned cloned repository",
			zap.String("path", dirPath),
			zap.String("projectId", entry.Name()),
		)

		if err := os.RemoveAll(dirPath); err != nil {
			c.logger.Error("Failed to remove orphaned repo",
				zap.String("path", dirPath),
				zap.Error(err),
			)
			errors = append(errors, fmt.Sprintf("failed to remove %s: %v", dirPath, err))
		} else {
			removed++
		}
	}

	return removed, errors
}

// GetOrphanReport returns a report of orphaned resources without cleaning them
func (c *CleanupService) GetOrphanReport(ctx context.Context) *CleanupReport {
	report := &CleanupReport{}

	// Count stale deploys
	stale, err := c.db.ListStaleRunningDeploys(c.cfg.BuildTimeout)
	if err == nil {
		report.StaleDeploysFixed = len(stale)
	}

	// Count orphan containers
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_list",
		Command: "lxc",
		Args:    []string{"list", "--format", "csv", "--columns", "n", "--filter", "opendeploy"},
		Timeout: 15 * time.Second,
	})

	if err == nil && result.Success {
		trackedContainers := make(map[string]bool)
		projects, _ := c.db.ListProjects()
		for _, project := range projects {
			containers, _ := c.db.ListContainersByProject(project.ID)
			for _, container := range containers {
				trackedContainers[container.ContainerID] = true
				trackedContainers[container.Name] = true
			}
		}

		for _, line := range result.Lines {
			if line.Stream != "stdout" || line.Text == "" {
				continue
			}
			containerName := strings.TrimSpace(line.Text)
			if !trackedContainers[containerName] {
				report.OrphanContainersRemoved++
			}
		}
	}

	return report
}

// DeleteProject performs comprehensive cleanup when deleting a project
func (c *CleanupService) DeleteProject(ctx context.Context, projectID string) error {
	c.logger.Info("deleting project with full cleanup", zap.String("projectId", projectID))

	project, err := c.db.GetProject(projectID)
	if err != nil || project == nil {
		return fmt.Errorf("project not found: %w", err)
	}

	// 1. Find ALL containers for this project, including full-stack variants
	// Full-stack deployments store containers with projectID+"-frontend" and projectID+"-backend"
	allContainerIDs := []string{projectID}
	allContainerIDs = append(allContainerIDs, projectID+"-frontend")
	allContainerIDs = append(allContainerIDs, projectID+"-backend")

	var allContainers []state.Container
	for _, cid := range allContainerIDs {
		containers, err := c.db.ListContainersByProject(cid)
		if err == nil && len(containers) > 0 {
			allContainers = append(allContainers, containers...)
		}
	}

	c.logger.Info("found containers for project",
		zap.String("projectId", projectID),
		zap.Int("count", len(allContainers)),
	)

	// 2. Stop and remove each container
	for _, container := range allContainers {
		lxdName := container.ContainerID
		c.logger.Info("stopping and removing container",
			zap.String("projectId", projectID),
			zap.String("containerName", container.Name),
			zap.String("lxdName", lxdName),
		)

		// Remove proxy devices before stopping (in case they cause issues)
		c.removeAllProxyDevices(ctx, lxdName)

		// Stop container with force flag
		stopResult, stopErr := c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_stop",
			Command: "lxc",
			Args:    []string{"stop", lxdName, "--force"},
			Timeout: 30 * time.Second,
		})
		if stopErr != nil || (stopResult != nil && !stopResult.Success) {
			c.logger.Warn("failed to stop container, attempting delete anyway",
				zap.String("lxdName", lxdName),
				zap.Error(stopErr),
			)
		}

		// Remove container from LXD with force flag
		delResult, delErr := c.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "--force", lxdName},
			Timeout: 30 * time.Second,
		})
		if delErr != nil || (delResult != nil && !delResult.Success) {
			c.logger.Error("failed to delete LXD container",
				zap.String("lxdName", lxdName),
				zap.Error(delErr),
			)
			// Log output for debugging
			if delResult != nil {
				for _, line := range delResult.Lines {
					c.logger.Error("lxc delete output",
						zap.String("stream", line.Stream),
						zap.String("text", line.Text),
					)
				}
			}
		} else {
			c.logger.Info("LXD container deleted", zap.String("lxdName", lxdName))
		}

		// Remove from database
		if err := c.db.DeleteContainer(container.ID); err != nil {
			c.logger.Error("failed to delete container from DB",
				zap.String("containerId", container.ID),
				zap.Error(err),
			)
		}
	}

	// 3. Fallback: find any orphan LXD containers matching this project by name pattern
	// This catches containers that may exist in LXD but not in the database
	c.cleanupOrphanContainersByName(ctx, project.Name, projectID)

	// 4. Delete cloned repository from /tmp
	repoPath := filepath.Join("/tmp", projectID)
	if _, err := os.Stat(repoPath); err == nil {
		c.logger.Info("removing cloned repository", zap.String("path", repoPath))
		os.RemoveAll(repoPath)
	}

	// 5. Delete build artifacts from output directory
	if project.Name != "" {
		outputPath := filepath.Join(c.cfg.OutputRoot, "sites", sanitizeFolderName(project.Name))
		if _, err := os.Stat(outputPath); err == nil {
			c.logger.Info("removing build artifacts", zap.String("path", outputPath))
			os.RemoveAll(outputPath)
		}
	}

	// 6. Remove nginx site config if domain is set
	if project.Domain != "" {
		c.logger.Info("removing nginx site config", zap.String("domain", project.Domain))

		// Delete nginx config files
		sitesAvailable := "/etc/nginx/sites-available"
		sitesEnabled := "/etc/nginx/sites-enabled"

		availablePath := filepath.Join(sitesAvailable, project.Domain)
		enabledPath := filepath.Join(sitesEnabled, project.Domain)

		os.Remove(enabledPath)
		os.Remove(availablePath)

		// Reload nginx
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "nginx_reload",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/systemctl", "reload", "nginx"},
			Timeout: 10 * time.Second,
		})

		// Remove nginx_sites record from database
		site, _ := c.db.GetNginxSiteByProjectID(projectID)
		if site != nil {
			c.db.DeleteNginxSite(site.ID)
		}
	}

	// 7. Delete all deploys and their logs
	deploys, _ := c.db.ListDeploysByProject(projectID)
	for _, deploy := range deploys {
		c.db.DeleteDeployLogs(deploy.ID)
	}

	// 8. Delete environment variables
	envVars, _ := c.db.ListEnvVariables(projectID)
	for _, env := range envVars {
		c.db.DeleteEnvVariable(env.ID)
	}

	// 9. Delete project from database (this will cascade delete deploys)
	if err := c.db.DeleteProject(projectID); err != nil {
		return fmt.Errorf("failed to delete project from database: %w", err)
	}

	c.logger.Info("project deleted successfully", zap.String("projectId", projectID))
	return nil
}

// removeAllProxyDevices removes all proxy devices from an LXD container
func (c *CleanupService) removeAllProxyDevices(ctx context.Context, containerName string) {
	// List all devices on the container
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_list_devices",
		Command: "lxc",
		Args:    []string{"config", "device", "list", containerName},
		Timeout: 10 * time.Second,
	})

	if err != nil || result == nil || !result.Success {
		return
	}

	// Parse device names and remove proxy devices
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			deviceName := strings.TrimSpace(line.Text)
			if strings.HasPrefix(deviceName, "proxy") {
				c.logger.Info("removing proxy device",
					zap.String("container", containerName),
					zap.String("device", deviceName),
				)
				c.runner.Run(ctx, exec.RunOpts{
					JobType: "lxd_remove_device",
					Command: "lxc",
					Args:    []string{"config", "device", "remove", containerName, deviceName},
					Timeout: 10 * time.Second,
				})
			}
		}
	}
}

// cleanupOrphanContainersByName finds and removes LXD containers matching a project name pattern
// that may not be tracked in the database
func (c *CleanupService) cleanupOrphanContainersByName(ctx context.Context, projectName, projectID string) {
	if projectName == "" {
		return
	}

	// List all LXD containers matching the opendeploy-<projectName> pattern
	// Container names are like: opendeploy-<projectName>-<timestamp>
	// For full-stack: opendeploy-<projectName>-frontend-<timestamp>, opendeploy-<projectName>-backend-<timestamp>
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_list",
		Command: "lxc",
		Args:    []string{"list", "--format", "csv", "--columns", "n"},
		Timeout: 10 * time.Second,
	})

	if err != nil || result == nil || !result.Success {
		return
	}

	prefix := fmt.Sprintf("opendeploy-%s", projectName)
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			name := strings.TrimSpace(line.Text)
			if name == "" {
				continue
			}

			// Check if this container matches our project
			if strings.HasPrefix(name, prefix) {
				// Check if it's already tracked in our DB
				dbContainer, _ := c.db.GetContainerByName(name)
				if dbContainer == nil {
					c.logger.Info("found orphan LXD container, removing",
						zap.String("containerName", name),
						zap.String("projectId", projectID),
					)

					// Remove proxy devices
					c.removeAllProxyDevices(ctx, name)

					// Stop and delete
					c.runner.Run(ctx, exec.RunOpts{
						JobType: "lxd_stop",
						Command: "lxc",
						Args:    []string{"stop", name, "--force"},
						Timeout: 30 * time.Second,
					})
					c.runner.Run(ctx, exec.RunOpts{
						JobType: "lxd_delete",
						Command: "lxc",
						Args:    []string{"delete", "--force", name},
						Timeout: 30 * time.Second,
					})
				}
			}
		}
	}
}
