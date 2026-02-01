package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type ProjectType string

const (
	ProjectNode   ProjectType = "node"
	ProjectPython ProjectType = "python"
	ProjectGo     ProjectType = "go"
	ProjectStatic ProjectType = "static"
)

type CommitInfo struct {
	Hash      string `json:"hash"`
	Subject   string `json:"subject"`
	Author    string `json:"author"`
	Email     string `json:"email"`
	Timestamp int64  `json:"timestamp"`
}

type DeployService struct {
	runner      *exec.Runner
	db          *state.DB
	cfg         config.DeployConfig
	logger      *zap.Logger
	docker      *DockerService
	broadcaster exec.Broadcaster
}

func NewDeployService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *DeployService {
	docker := NewDockerService(runner, cfg, logger)
	return &DeployService{runner: runner, db: db, cfg: cfg, logger: logger, docker: docker}
}

// SetBroadcaster sets the WebSocket broadcaster for deploy phase updates.
func (d *DeployService) SetBroadcaster(b exec.Broadcaster) {
	d.broadcaster = b
}

func (d *DeployService) broadcastPhase(deployID, phase, message string) {
	if d.broadcaster != nil {
		d.broadcaster.BroadcastToJob(deployID, map[string]interface{}{
			"type":    "progress",
			"jobId":   deployID,
			"phase":   phase,
			"message": message,
		})
	}
}

func (d *DeployService) Clone(ctx context.Context, repoURL, branch, projectID, jobID string) (*exec.ExecResult, error) {
	if !isValidRepoURL(repoURL) {
		return nil, fmt.Errorf("invalid repository URL")
	}

	targetDir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	// Remove existing directory if present
	os.RemoveAll(targetDir)

	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID,
		JobType:  "git_clone",
		Command:  d.cfg.GitBinary,
		Args:     []string{"clone", "--branch", branch, "--depth", "1", repoURL, targetDir},
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})
	return result, err
}

func (d *DeployService) Pull(ctx context.Context, projectID, branch, jobID string) (*exec.ExecResult, error) {
	targetDir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID,
		JobType:  "git_pull",
		Command:  d.cfg.GitBinary,
		Args:     []string{"-C", targetDir, "pull", "origin", branch},
		MergeEnv: true,
		Timeout:  5 * time.Minute,
	})
	return result, err
}

func (d *DeployService) GetLatestCommit(projectID string) (*CommitInfo, error) {
	targetDir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	result, err := d.runner.Run(context.Background(), exec.RunOpts{
		JobType:  "git_log",
		Command:  d.cfg.GitBinary,
		Args:     []string{"-C", targetDir, "log", "-1", "--format=%H|%s|%an|%ae|%at"},
		MergeEnv: true,
		Timeout:  10 * time.Second,
	})
	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to get commit info")
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.Contains(line.Text, "|") {
			parts := strings.SplitN(line.Text, "|", 5)
			if len(parts) == 5 {
				var ts int64
				fmt.Sscanf(parts[4], "%d", &ts)
				return &CommitInfo{
					Hash:      parts[0],
					Subject:   parts[1],
					Author:    parts[2],
					Email:     parts[3],
					Timestamp: ts,
				}, nil
			}
		}
	}
	return nil, fmt.Errorf("no commit found")
}

func (d *DeployService) DetectProjectType(projectID string) ProjectType {
	dir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	if fileExists(filepath.Join(dir, "package.json")) {
		return ProjectNode
	}
	if fileExists(filepath.Join(dir, "requirements.txt")) || fileExists(filepath.Join(dir, "pyproject.toml")) {
		return ProjectPython
	}
	if fileExists(filepath.Join(dir, "go.mod")) {
		return ProjectGo
	}
	return ProjectStatic
}

func (d *DeployService) BuildNode(ctx context.Context, projectID, buildCmd, outputDir string, envVars map[string]string, jobID string) (*exec.ExecResult, error) {
	dir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	// Step 1: npm install
	installResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID + "-install",
		JobType:  "npm_install",
		Command:  d.cfg.NpmBinary,
		Args:     []string{"install"},
		WorkDir:  dir,
		Env:      envVars,
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})
	if err != nil || !installResult.Success {
		return installResult, fmt.Errorf("npm install failed")
	}

	// Step 2: Build
	if buildCmd == "" {
		buildCmd = "build"
	}
	buildResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID + "-build",
		JobType:  "npm_build",
		Command:  d.cfg.NpmBinary,
		Args:     []string{"run", buildCmd},
		WorkDir:  dir,
		Env:      envVars,
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})
	if err != nil || !buildResult.Success {
		return buildResult, fmt.Errorf("npm build failed")
	}

	// Step 3: Verify output directory
	if outputDir == "" {
		// Try common output directories
		for _, candidate := range []string{"dist", "build", ".next", "out"} {
			if fileExists(filepath.Join(dir, candidate)) {
				outputDir = candidate
				break
			}
		}
	}
	outPath := filepath.Join(dir, outputDir)
	if !fileExists(outPath) {
		return buildResult, fmt.Errorf("build output directory '%s' not found", outputDir)
	}

	return buildResult, nil
}

func (d *DeployService) BuildPython(ctx context.Context, projectID string, envVars map[string]string, jobID string) (*exec.ExecResult, error) {
	dir := filepath.Join(d.cfg.WorkspaceRoot, projectID)

	// Step 1: Create virtualenv
	_, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID + "-venv",
		JobType:  "python_venv",
		Command:  d.cfg.PythonBinary,
		Args:     []string{"-m", "venv", ".venv"},
		WorkDir:  dir,
		MergeEnv: true,
		Timeout:  2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("venv creation failed: %w", err)
	}

	// Step 2: Install dependencies
	pipPath := filepath.Join(dir, ".venv", "bin", "pip")
	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID + "-pip",
		JobType:  "pip_install",
		Command:  pipPath,
		Args:     []string{"install", "-r", "requirements.txt"},
		WorkDir:  dir,
		Env:      envVars,
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})
	if err != nil || !result.Success {
		return result, fmt.Errorf("pip install failed")
	}

	return result, nil
}

func (d *DeployService) Deploy(ctx context.Context, project *state.Project) (string, error) {
	deployID := uuid.New().String()

	// Get env vars from the database
	envVars, _ := d.db.GetEnvMap(project.ID)
	if envVars == nil {
		envVars = make(map[string]string)
	}
	// Also merge any legacy env vars from the project record
	if project.EnvVars != "" && project.EnvVars != "{}" {
		var legacyVars map[string]string
		json.Unmarshal([]byte(project.EnvVars), &legacyVars)
		for k, v := range legacyVars {
			if _, exists := envVars[k]; !exists {
				envVars[k] = v
			}
		}
	}

	// Create deploy record
	deploy := &state.Deploy{
		ID:        deployID,
		ProjectID: project.ID,
		Status:    "running",
	}
	d.db.CreateDeploy(deploy)

	// Run in background goroutine
	go func() {
		var err error
		projectDir := filepath.Join(d.cfg.WorkspaceRoot, project.ID)

		// Clone or pull
		d.broadcastPhase(deployID, "clone", "Cloning repository...")
		var gitResult *exec.ExecResult
		if fileExists(filepath.Join(projectDir, ".git")) {
			gitResult, err = d.Pull(ctx, project.ID, project.Branch, deployID)
		} else {
			gitResult, err = d.Clone(ctx, project.RepoURL, project.Branch, project.ID, deployID)
		}
		if err != nil {
			d.failDeploy(deploy, err.Error())
			return
		}
		if gitResult != nil && !gitResult.Success {
			errMsg := fmt.Sprintf("git failed with exit code %d", gitResult.ExitCode)
			for _, line := range gitResult.Lines {
				if line.Stream == "stderr" {
					errMsg = line.Text
					break
				}
			}
			d.failDeploy(deploy, errMsg)
			return
		}

		// Get commit info
		commit, _ := d.GetLatestCommit(project.ID)
		if commit != nil {
			deploy.CommitHash = commit.Hash
			deploy.CommitMessage = commit.Subject
			deploy.CommitAuthor = commit.Author
		}

		// Detect framework
		d.broadcastPhase(deployID, "detect", "Detecting project type...")
		framework := d.docker.DetectFramework(projectDir)
		d.logger.Info("detected framework",
			zap.String("projectId", project.ID),
			zap.String("framework", string(framework)),
		)

		// Determine project type if not set
		projectType := ProjectType(project.ProjectType)
		if projectType == "" {
			projectType = d.DetectProjectType(project.ID)
		}

		// Use Docker builds if enabled
		d.broadcastPhase(deployID, "build", "Building project...")
		if d.cfg.DockerEnabled {
			_, err = d.docker.BuildInDocker(ctx, project.ID, deployID, framework, project.BuildCommand, project.OutputDir, envVars)
			if err != nil {
				d.failDeploy(deploy, err.Error())
				return
			}
		} else {
			// Fallback to direct builds
			switch projectType {
			case ProjectNode:
				_, err = d.BuildNode(ctx, project.ID, project.BuildCommand, project.OutputDir, envVars, deployID)
			case ProjectPython:
				_, err = d.BuildPython(ctx, project.ID, envVars, deployID)
			case ProjectStatic:
				// No build needed
			}

			if err != nil {
				d.failDeploy(deploy, err.Error())
				return
			}
		}

		// Success
		now := time.Now()
		deploy.Status = "success"
		deploy.EndedAt = &now
		deploy.ExitCode = 0
		d.db.UpdateDeploy(deploy)

		// If it's a backend framework, create a systemd service
		if IsBackendFramework(framework) {
			d.broadcastPhase(deployID, "service", "Creating system service...")
			svcErr := d.CreateServiceForFramework(project.ID, project.Name, framework, envVars)
			if svcErr != nil {
				d.logger.Error("failed to create service",
					zap.String("projectId", project.ID),
					zap.Error(svcErr),
				)
			} else {
				d.logger.Info("service created",
					zap.String("projectId", project.ID),
					zap.String("service", fmt.Sprintf("opendeploy-app-%s", project.Name)),
				)
			}
		}

		d.logger.Info("deploy completed",
			zap.String("deployId", deployID),
			zap.String("projectId", project.ID),
			zap.String("framework", string(framework)),
		)

		d.broadcastPhase(deployID, "done", "Deploy complete!")
	}()

	return deployID, nil
}

func (d *DeployService) failDeploy(deploy *state.Deploy, errMsg string) {
	now := time.Now()
	deploy.Status = "failed"
	deploy.EndedAt = &now
	deploy.ExitCode = 1
	d.db.UpdateDeploy(deploy)
	d.logger.Error("deploy failed", zap.String("deployId", deploy.ID), zap.String("error", errMsg))
}

func (d *DeployService) StartAppService(name, workDir, command string, envVars map[string]string) error {
	envLines := ""
	for k, v := range envVars {
		envLines += fmt.Sprintf("Environment=%s=%s\n", k, v)
	}

	unit := fmt.Sprintf(`[Unit]
Description=OpenDeploy App: %s
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3
%s
[Install]
WantedBy=multi-user.target
`, name, workDir, command, envLines)

	serviceName := fmt.Sprintf("opendeploy-app-%s", name)
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	if err := os.WriteFile(servicePath, []byte(unit), 0644); err != nil {
		// Try with sudo via temp file
		tmpPath := fmt.Sprintf("/tmp/%s.service", serviceName)
		if writeErr := os.WriteFile(tmpPath, []byte(unit), 0644); writeErr != nil {
			return fmt.Errorf("writing service file: %w", err)
		}
		ctx := context.Background()
		d.runner.Run(ctx, exec.RunOpts{
			JobType: "systemctl",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/cp", tmpPath, servicePath},
			Timeout: 10 * time.Second,
		})
		os.Remove(tmpPath)
	}

	// Reload and start
	ctx := context.Background()
	d.runner.Run(ctx, exec.RunOpts{
		JobType: "systemctl",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "daemon-reload"},
		Timeout: 10 * time.Second,
	})
	d.runner.Run(ctx, exec.RunOpts{
		JobType: "systemctl",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "enable", serviceName},
		Timeout: 10 * time.Second,
	})
	_, err := d.runner.Run(ctx, exec.RunOpts{
		JobType: "systemctl",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "start", serviceName},
		Timeout: 15 * time.Second,
	})
	return err
}

// CreateServiceForFramework generates and starts a systemd service for the given framework.
func (d *DeployService) CreateServiceForFramework(projectID, projectName string, framework FrameworkType, envVars map[string]string) error {
	if !IsBackendFramework(framework) {
		return nil // Static sites don't need a service
	}

	backendDir := filepath.Join(d.cfg.OutputRoot, "backend", projectID)
	startCmd := GetStartCommand(framework, backendDir)
	if startCmd == "" {
		return fmt.Errorf("no start command for framework: %s", framework)
	}

	// Add PORT env var if not set
	if _, ok := envVars["PORT"]; !ok {
		envVars["PORT"] = fmt.Sprintf("%d", GetDefaultPort(framework))
	}

	return d.StartAppService(projectName, backendDir, startCmd, envVars)
}

var repoURLRegex = regexp.MustCompile(`^(https?://|git@)[a-zA-Z0-9._\-/:]+\.git$|^https?://[a-zA-Z0-9._\-/]+$`)

func isValidRepoURL(url string) bool {
	return repoURLRegex.MatchString(url)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
