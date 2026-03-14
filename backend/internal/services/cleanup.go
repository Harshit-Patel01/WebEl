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

// CleanupOrphanContainers finds Docker containers with opendeploy- prefix that aren't tracked in the DB
func (c *CleanupService) CleanupOrphanContainers(ctx context.Context) (int, []string) {
	var errors []string

	// List all Docker containers with opendeploy prefix
	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_ps",
		Command: c.cfg.DockerBinary,
		Args:    []string{"ps", "-a", "--format", "{{.ID}}|{{.Names}}|{{.Status}}", "--filter", "name=opendeploy"},
		Timeout: 15 * time.Second,
	})

	if err != nil || !result.Success {
		// Docker might not be available
		c.logger.Debug("docker not available for orphan cleanup, skipping")
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

		parts := strings.SplitN(line.Text, "|", 3)
		if len(parts) < 2 {
			continue
		}

		containerID := strings.TrimSpace(parts[0])
		containerName := strings.TrimSpace(parts[1])

		// Check if this container is tracked in the DB
		if trackedContainers[containerID] || trackedContainers[containerName] {
			continue
		}

		// This is an orphan — remove it
		c.logger.Info("removing orphan container",
			zap.String("containerID", containerID),
			zap.String("containerName", containerName),
		)

		// Stop first, then remove
		c.runner.Run(ctx, exec.RunOpts{
			JobType: "docker_stop",
			Command: c.cfg.DockerBinary,
			Args:    []string{"stop", containerID},
			Timeout: 15 * time.Second,
		})

		_, rmErr := c.runner.Run(ctx, exec.RunOpts{
			JobType: "docker_rm",
			Command: c.cfg.DockerBinary,
			Args:    []string{"rm", "-f", containerID},
			Timeout: 15 * time.Second,
		})

		if rmErr != nil {
			errors = append(errors, fmt.Sprintf("failed to remove orphan container %s: %v", containerID, rmErr))
		} else {
			removed++
		}
	}

	return removed, errors
}

// CleanupDanglingImages removes <none> tagged Docker images to free disk space
func (c *CleanupService) CleanupDanglingImages(ctx context.Context) (int, []string) {
	var errors []string

	result, err := c.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_prune_images",
		Command: c.cfg.DockerBinary,
		Args:    []string{"image", "prune", "-f"},
		Timeout: 60 * time.Second,
	})

	if err != nil || !result.Success {
		c.logger.Debug("docker image prune not available, skipping")
		return 0, nil
	}

	// Count removed images from output
	removed := 0
	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.Contains(line.Text, "deleted:") {
			removed++
		}
	}

	return removed, errors
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
		JobType: "docker_ps",
		Command: c.cfg.DockerBinary,
		Args:    []string{"ps", "-a", "--format", "{{.ID}}|{{.Names}}", "--filter", "name=opendeploy"},
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
			parts := strings.SplitN(line.Text, "|", 2)
			if len(parts) < 2 {
				continue
			}
			containerID := strings.TrimSpace(parts[0])
			containerName := strings.TrimSpace(parts[1])
			if !trackedContainers[containerID] && !trackedContainers[containerName] {
				report.OrphanContainersRemoved++
			}
		}
	}

	return report
}
