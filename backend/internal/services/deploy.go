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
	nginx       *NginxService
	container   *ContainerService
	broadcaster exec.Broadcaster
}

// DeployOptions contains optional runtime configuration for a deployment
type DeployOptions struct {
	Domain       string
	ZoneID       string
	ManualDomain bool
	EnableNginx  bool
}

func NewDeployService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *DeployService {
	docker := NewDockerService(runner, cfg, logger)
	return &DeployService{
		runner: runner,
		db:     db,
		cfg:    cfg,
		logger: logger,
		docker: docker,
	}
}

// SetNginxService sets the nginx service for creating site configs
func (d *DeployService) SetNginxService(nginx *NginxService) {
	d.nginx = nginx
}

// SetContainerService sets the container service for managing backend containers
func (d *DeployService) SetContainerService(container *ContainerService) {
	d.container = container
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

	targetDir := filepath.Join("/tmp", projectID)

	// Remove existing directory if present
	os.RemoveAll(targetDir)

	// Derive the deploy-level broadcast topic from the job ID
	broadcastID := strings.TrimSuffix(jobID, "-clone")

	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID,
		BroadcastJobID: broadcastID,
		JobType:        "git_clone",
		Command:        d.cfg.GitBinary,
		Args:           []string{"clone", "--branch", branch, "--depth", "1", "--progress", repoURL, targetDir},
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})
	return result, err
}

func (d *DeployService) Pull(ctx context.Context, projectID, branch, jobID string) (*exec.ExecResult, error) {
	targetDir := filepath.Join("/tmp", projectID)

	// Derive the deploy-level broadcast topic from the job ID
	broadcastID := strings.TrimSuffix(jobID, "-pull")

	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID,
		BroadcastJobID: broadcastID,
		JobType:        "git_pull",
		Command:        d.cfg.GitBinary,
		Args:           []string{"-C", targetDir, "pull", "origin", branch},
		MergeEnv:       true,
		Timeout:        5 * time.Minute,
	})
	return result, err
}

func (d *DeployService) GetLatestCommit(projectID string) (*CommitInfo, error) {
	targetDir := filepath.Join("/tmp", projectID)

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
	dir := filepath.Join("/tmp", projectID)

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

// DetectWorkingDirectory tries to find the actual working directory for the project
func (d *DeployService) DetectWorkingDirectory(projectID, userSpecified string) string {
	baseDir := filepath.Join("/tmp", projectID)

	// If user specified, use that
	if userSpecified != "" && userSpecified != "." {
		testPath := filepath.Join(baseDir, userSpecified)
		if fileExists(testPath) {
			return userSpecified
		}
	}

	// Try common patterns
	commonDirs := []string{"frontend", "client", "web", "app", "src"}
	for _, dir := range commonDirs {
		testPath := filepath.Join(baseDir, dir)
		if fileExists(filepath.Join(testPath, "package.json")) ||
		   fileExists(filepath.Join(testPath, "requirements.txt")) ||
		   fileExists(filepath.Join(testPath, "go.mod")) {
			return dir
		}
	}

	// Default to root
	return "."
}

func (d *DeployService) BuildNode(ctx context.Context, projectID, workingDir, buildCmd, outputDir string, envVars map[string]string, jobID string) (*exec.ExecResult, error) {
	baseDir := filepath.Join("/tmp", projectID)
	dir := baseDir
	if workingDir != "" && workingDir != "." {
		dir = filepath.Join(baseDir, workingDir)
	}

	// Helper to log build output to database for historical viewing
	// (real-time WS broadcasting is handled by the Runner via BroadcastJobID)
	logBuildOutput := func(result *exec.ExecResult, phase string) {
		if result != nil {
			for _, line := range result.Lines {
				log := &state.DeployLog{
					DeployID:     jobID,
					Stream:       line.Stream,
					Message:      line.Text,
					LogTimestamp: line.Timestamp,
				}
				if err := d.db.CreateDeployLog(log); err != nil {
					d.logger.Error("failed to save build log to database", zap.Error(err))
				}
			}
		}
	}

	// Step 1: npm install
	d.logger.Info("running npm install", zap.String("dir", dir))
	installResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-install",
		BroadcastJobID: jobID,
		JobType:        "npm_install",
		Command:        d.cfg.NpmBinary,
		Args:           []string{"install"},
		WorkDir:        dir,
		Env:            envVars,
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})

	// Log install output
	logBuildOutput(installResult, "install")

	if err != nil || !installResult.Success {
		return installResult, fmt.Errorf("npm install failed")
	}

	// Step 2: Check if package.json exists and has build script
	packageJsonPath := filepath.Join(dir, "package.json")
	if !fileExists(packageJsonPath) {
		return nil, fmt.Errorf("package.json not found in %s", dir)
	}

	// Read package.json to check for build script
	packageData, err := os.ReadFile(packageJsonPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read package.json: %w", err)
	}

	// Parse package.json to verify build script exists
	var pkgJSON struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(packageData, &pkgJSON); err != nil {
		d.logger.Warn("failed to parse package.json", zap.Error(err))
	}

	// Step 3: Build
	if buildCmd == "" {
		buildCmd = "build"
	}

	// Check if the build script exists in package.json
	if pkgJSON.Scripts != nil {
		if _, exists := pkgJSON.Scripts[buildCmd]; !exists {
			return nil, fmt.Errorf("build script '%s' not found in package.json. Available scripts: %v", buildCmd, pkgJSON.Scripts)
		}
	}

	d.logger.Info("running npm build", zap.String("dir", dir), zap.String("cmd", buildCmd))
	buildResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-build",
		BroadcastJobID: jobID,
		JobType:        "npm_build",
		Command:        d.cfg.NpmBinary,
		Args:           []string{"run", buildCmd},
		WorkDir:        dir,
		Env:            envVars,
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})

	// Log build output
	logBuildOutput(buildResult, "build")

	if err != nil || !buildResult.Success {
		return buildResult, fmt.Errorf("npm build failed")
	}

	// Step 4: Verify output directory
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

func (d *DeployService) BuildPython(ctx context.Context, projectID, workingDir string, envVars map[string]string, jobID string) (*exec.ExecResult, error) {
	baseDir := filepath.Join("/tmp", projectID)
	dir := baseDir
	if workingDir != "" && workingDir != "." {
		dir = filepath.Join(baseDir, workingDir)
	}

	// Step 1: Create virtualenv
	_, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-venv",
		BroadcastJobID: jobID,
		JobType:        "python_venv",
		Command:        d.cfg.PythonBinary,
		Args:           []string{"-m", "venv", ".venv"},
		WorkDir:        dir,
		MergeEnv:       true,
		Timeout:        2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("venv creation failed: %w", err)
	}

	// Step 2: Install dependencies
	pipPath := filepath.Join(dir, ".venv", "bin", "pip")
	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-pip",
		BroadcastJobID: jobID,
		JobType:        "pip_install",
		Command:        pipPath,
		Args:           []string{"install", "-r", "requirements.txt"},
		WorkDir:        dir,
		Env:            envVars,
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})
	if err != nil || !result.Success {
		return result, fmt.Errorf("pip install failed")
	}

	return result, nil
}

func (d *DeployService) BuildGo(ctx context.Context, projectID, workingDir string, envVars map[string]string, jobID string) (*exec.ExecResult, error) {
	baseDir := filepath.Join("/tmp", projectID)
	dir := baseDir
	if workingDir != "" && workingDir != "." {
		dir = filepath.Join(baseDir, workingDir)
	}

	// Step 1: Download Go modules
	d.logger.Info("running go mod download", zap.String("dir", dir))
	modResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-gomod",
		BroadcastJobID: jobID,
		JobType:        "go_mod_download",
		Command:        d.cfg.GoBinary,
		Args:           []string{"mod", "download"},
		WorkDir:        dir,
		Env:            envVars,
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})

	// Log module download output
	if modResult != nil {
		for _, line := range modResult.Lines {
			log := &state.DeployLog{
				DeployID:     jobID,
				Stream:       line.Stream,
				Message:      line.Text,
				LogTimestamp: line.Timestamp,
			}
			if err := d.db.CreateDeployLog(log); err != nil {
				d.logger.Error("failed to save build log to database", zap.Error(err))
			}
		}
	}

	if err != nil || !modResult.Success {
		return modResult, fmt.Errorf("go mod download failed")
	}

	// Step 2: Build Go binary
	d.logger.Info("running go build", zap.String("dir", dir))
	buildResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-gobuild",
		BroadcastJobID: jobID,
		JobType:        "go_build",
		Command:        d.cfg.GoBinary,
		Args:           []string{"build", "-o", "server", "."},
		WorkDir:        dir,
		Env: func() map[string]string {
			merged := make(map[string]string)
			for k, v := range envVars {
				merged[k] = v
			}
			merged["CGO_ENABLED"] = "0"
			return merged
		}(),
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})

	// Log build output
	if buildResult != nil {
		for _, line := range buildResult.Lines {
			log := &state.DeployLog{
				DeployID:     jobID,
				Stream:       line.Stream,
				Message:      line.Text,
				LogTimestamp: line.Timestamp,
			}
			if err := d.db.CreateDeployLog(log); err != nil {
				d.logger.Error("failed to save build log to database", zap.Error(err))
			}
		}
	}

	if err != nil || !buildResult.Success {
		return buildResult, fmt.Errorf("go build failed")
	}

	return buildResult, nil
}

func (d *DeployService) Deploy(ctx context.Context, project *state.Project) (string, error) {
	return d.DeployWithOptions(ctx, project, nil)
}

// DeployWithOptions deploys a project with optional runtime configuration
func (d *DeployService) DeployWithOptions(ctx context.Context, project *state.Project, opts *DeployOptions) (string, error) {
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

	// Helper to log to database
	logToDB := func(stream, message string) {
		log := &state.DeployLog{
			DeployID:     deployID,
			Stream:       stream,
			Message:      message,
			LogTimestamp: time.Now(),
		}
		if err := d.db.CreateDeployLog(log); err != nil {
			d.logger.Error("failed to save log to database", zap.Error(err))
		}

		// Also broadcast to WebSocket for real-time updates
		if d.broadcaster != nil {
			d.broadcaster.BroadcastToJob(deployID, map[string]interface{}{
				"type":      "deploy_log",
				"deployId":  deployID,
				"stream":    stream,
				"message":   message,
				"timestamp": log.LogTimestamp,
			})
		}
	}

	// Run in background goroutine
	go func() {
		var err error
		projectDir := filepath.Join("/tmp", project.ID)
		buildStart := time.Now()

		// Create a new context for the deployment (not tied to the HTTP request)
		deployCtx := context.Background()

		// AP now runs on a separate virtual interface (ap0) so it does not
		// interfere with wlan0 connectivity during builds.

		// Clone or pull
		d.broadcastPhase(deployID, "clone", "Cloning repository...")
		logToDB("stdout", "Starting deployment...")
		logToDB("stdout", fmt.Sprintf("Repository: %s", project.RepoURL))

		var gitResult *exec.ExecResult
		if fileExists(filepath.Join(projectDir, ".git")) {
			logToDB("stdout", "Pulling latest changes...")
			gitResult, err = d.Pull(deployCtx, project.ID, project.Branch, deployID+"-pull")
		} else {
			logToDB("stdout", "Cloning repository...")
			gitResult, err = d.Clone(deployCtx, project.RepoURL, project.Branch, project.ID, deployID+"-clone")
		}
		if err != nil {
			logToDB("stderr", fmt.Sprintf("Git operation failed: %s", err.Error()))
			d.failDeploy(deploy, err.Error())
			return
		}
		if gitResult != nil && !gitResult.Success {
			errMsg := fmt.Sprintf("git failed with exit code %d", gitResult.ExitCode)
			for _, line := range gitResult.Lines {
				if line.Stream == "stderr" {
					errMsg = line.Text
					logToDB("stderr", line.Text)
					break
				}
			}
			d.failDeploy(deploy, errMsg)
			return
		}
		logToDB("stdout", "Repository cloned successfully")

		// Get commit info
		commit, _ := d.GetLatestCommit(project.ID)
		if commit != nil {
			deploy.CommitHash = commit.Hash
			deploy.CommitMessage = commit.Subject
			deploy.CommitAuthor = commit.Author
			logToDB("stdout", fmt.Sprintf("Commit: %s - %s", commit.Hash[:7], commit.Subject))
		}

		// Detect working directory
		d.broadcastPhase(deployID, "detect", "Detecting project structure...")
		workingDir := d.DetectWorkingDirectory(project.ID, project.WorkingDirectory)
		logToDB("stdout", fmt.Sprintf("Working directory: %s", workingDir))

		actualProjectDir := projectDir
		if workingDir != "" && workingDir != "." {
			actualProjectDir = filepath.Join(projectDir, workingDir)
		}

		// Detect framework immediately after determining working directory
		d.broadcastPhase(deployID, "detect", "Detecting framework...")
		framework := d.docker.DetectFramework(actualProjectDir)
		d.logger.Info("detected framework",
			zap.String("projectId", project.ID),
			zap.String("framework", string(framework)),
		)
		logToDB("stdout", fmt.Sprintf("Detected framework: %s", framework))

		// Store framework info on the deploy record
		deploy.Framework = string(framework)
		deploy.IsBackend = IsBackendFramework(framework)

		// Determine project type if not set
		projectType := ProjectType(project.ProjectType)
		if projectType == "" {
			projectType = d.DetectProjectType(project.ID)
		}

		// Override projectType based on framework for pure static sites
		if framework == FrameworkStatic {
			projectType = ProjectStatic
		}

		d.broadcastPhase(deployID, "build", "Building project...")
		logToDB("stdout", "Starting build process...")

		isBackend := IsBackendFramework(framework)
		useDocker := d.cfg.DockerEnabled && isBackend // Docker only for backends when enabled

		logToDB("stdout", fmt.Sprintf("Deployment mode: %s (Docker: %v)", map[bool]string{true: "backend", false: "frontend"}[isBackend], useDocker))

		if useDocker {
			// Docker path for backend services only
			logToDB("stdout", "Using Docker for backend containerization...")
			port := GetDefaultPort(framework)
			if project.LocalPort > 0 {
				port = project.LocalPort
			}

			installCmd := ""
			if project.InstallCommand != nil && *project.InstallCommand != "" {
				installCmd = *project.InstallCommand
			} else {
				installCmd = GetDefaultInstallCommand(framework)
			}

			startCmd := ""
			if project.StartCommand != nil && *project.StartCommand != "" {
				startCmd = *project.StartCommand
			} else {
				startCmd = GetDefaultStartCommand(framework, port)
			}

			dockerResult, dockerErr := d.docker.BuildInDocker(deployCtx, project.ID, deployID, framework, installCmd, startCmd, port, envVars, workingDir, project.OutputDir)
			if dockerResult != nil {
				for _, line := range dockerResult.Lines {
					dbLog := &state.DeployLog{
						DeployID:     deployID,
						Stream:       line.Stream,
						Message:      line.Text,
						LogTimestamp: line.Timestamp,
					}
					d.db.CreateDeployLog(dbLog)
				}
			}
			if dockerErr != nil {
				logToDB("stderr", fmt.Sprintf("Docker build failed: %s", dockerErr.Error()))
				d.failDeploy(deploy, dockerErr.Error())
				return
			}
		} else {
			logToDB("stdout", "Using native build (no Docker)...")

			switch projectType {
			case ProjectNode:
				_, err = d.BuildNode(deployCtx, project.ID, workingDir, project.BuildCommand, project.OutputDir, envVars, deployID)
			case ProjectPython:
				_, err = d.BuildPython(deployCtx, project.ID, workingDir, envVars, deployID)
			case ProjectGo:
				_, err = d.BuildGo(deployCtx, project.ID, workingDir, envVars, deployID)
			case ProjectStatic:
				logToDB("stdout", "Static project — no build needed")
			}

			if err != nil {
				logToDB("stderr", fmt.Sprintf("Build failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
		}
		logToDB("stdout", "Build completed successfully")

		// ================================================================
		// POST-BUILD: Copy output or start service
		// ================================================================
		var outputPath string

		if isBackend {
			d.broadcastPhase(deployID, "service", "Starting backend service...")
			logToDB("stdout", "Starting backend service...")

			if useDocker {
				// Start Docker container for backend
				imageName := fmt.Sprintf("opendeploy/%s:latest", project.ID)
				port := GetDefaultPort(framework)
				if project.LocalPort > 0 {
					port = project.LocalPort
				}

				if d.container != nil {
					container, containerErr := d.container.StartContainer(deployCtx, project.ID, project.Name, imageName, port, envVars)
					if containerErr != nil {
						logToDB("stderr", fmt.Sprintf("Failed to start container: %s", containerErr.Error()))
						d.logger.Error("failed to start container", zap.String("projectId", project.ID), zap.Error(containerErr))
					} else {
						logToDB("stdout", fmt.Sprintf("Container started: %s (port %d)", container.Name, port))
						d.logger.Info("container started", zap.String("projectId", project.ID), zap.String("containerName", container.Name))
					}
				}
			} else {
				// Native backend: start as systemd service
				startCmd := ""
				if project.StartCommand != nil && *project.StartCommand != "" {
					startCmd = *project.StartCommand
				} else {
					startCmd = GetDefaultStartCommand(framework, GetDefaultPort(framework))
				}

				if startCmd != "" {
					serviceErr := d.CreateServiceForFramework(project.ID, project.Name, framework, envVars)
					if serviceErr != nil {
						logToDB("stderr", fmt.Sprintf("Failed to create service: %s", serviceErr.Error()))
					} else {
						logToDB("stdout", fmt.Sprintf("Backend service started (command: %s)", startCmd))
					}
				}
			}
		} else {
			// Frontend: copy build output to nginx-servable directory
			d.broadcastPhase(deployID, "service", "Copying frontend build to serve directory...")
			logToDB("stdout", "Copying build output to nginx directory...")

			outputPath, err = d.copyFrontendToNginx(project.ID, project.Name, workingDir, project.OutputDir)
			if err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to copy build output: %s", err.Error()))
				// Not a fatal error — the build still succeeded
			} else {
				logToDB("stdout", "")
				logToDB("stdout", "═══════════════════════════════════════════════════════════")
				logToDB("stdout", fmt.Sprintf("✓ Frontend files deployed to: %s", outputPath))
				logToDB("stdout", "")
				logToDB("stdout", "To serve this site with nginx, configure your server block:")
				logToDB("stdout", fmt.Sprintf("  root %s;", outputPath))
				logToDB("stdout", "  index index.html;")
				logToDB("stdout", "")
				logToDB("stdout", "Or use the nginx configuration page to set up a domain.")
				logToDB("stdout", "═══════════════════════════════════════════════════════════")
				logToDB("stdout", "")
				d.logDirectoryListing(outputPath, deployID, logToDB)
			}
		}

		// ================================================================
		// SUCCESS
		// ================================================================
		now := time.Now()
		buildDuration := now.Sub(buildStart).Seconds()
		deploy.Status = "success"
		deploy.EndedAt = &now
		deploy.ExitCode = 0
		deploy.OutputPath = outputPath
		deploy.BuildDuration = buildDuration
		d.db.UpdateDeploy(deploy)

		d.logger.Info("deploy completed",
			zap.String("deployId", deployID),
			zap.String("projectId", project.ID),
			zap.String("framework", string(framework)),
			zap.Float64("buildDuration", buildDuration),
		)

		logToDB("stdout", "")
		logToDB("stdout", fmt.Sprintf("Deployment completed successfully! (%.1fs)", buildDuration))

		// Apply nginx configuration if domain is provided
		var nginxError string
		if opts != nil && opts.EnableNginx && opts.Domain != "" {
			logToDB("stdout", "")
			logToDB("stdout", fmt.Sprintf("Configuring nginx for domain: %s", opts.Domain))

			if err := d.applyNginxForDeploy(deployCtx, project, opts.Domain, outputPath, isBackend); err != nil {
				nginxError = err.Error()
				logToDB("stderr", fmt.Sprintf("Nginx configuration failed: %s", nginxError))
				d.logger.Error("nginx apply failed",
					zap.String("deployId", deployID),
					zap.String("domain", opts.Domain),
					zap.Error(err),
				)
			} else {
				logToDB("stdout", fmt.Sprintf("Nginx configured successfully for %s", opts.Domain))
			}
		}

		// Broadcast done with output metadata
		if d.broadcaster != nil {
			result := map[string]interface{}{
				"type":          "deploy_result",
				"deployId":      deployID,
				"status":        "success",
				"framework":     string(framework),
				"outputPath":    outputPath,
				"isBackend":     isBackend,
				"buildDuration": buildDuration,
			}
			if nginxError != "" {
				result["nginxError"] = nginxError
			}
			d.broadcaster.BroadcastToJob(deployID, result)
		}
		d.broadcastPhase(deployID, "done", "Deploy complete!")
	}()

	return deployID, nil
}

// copyFrontendToNginx copies the frontend build output to the nginx sites directory
// Returns the path where files were copied to
func (d *DeployService) copyFrontendToNginx(projectID, projectName, workingDir, outputDir string) (string, error) {
	baseDir := filepath.Join("/tmp", projectID)
	srcDir := baseDir
	if workingDir != "" && workingDir != "." {
		srcDir = filepath.Join(baseDir, workingDir)
	}

	// Auto-detect output directory if not specified
	if outputDir == "" {
		for _, candidate := range []string{"dist", "build", "out", ".next/out", ".next/static"} {
			if fileExists(filepath.Join(srcDir, candidate)) {
				outputDir = candidate
				break
			}
		}
		if outputDir == "" {
			outputDir = "dist"
		}
	}

	srcPath := filepath.Join(srcDir, outputDir)
	if !fileExists(srcPath) {
		return "", fmt.Errorf("build output directory '%s' not found at %s", outputDir, srcPath)
	}

	// Create a sanitized folder name from project name
	safeName := sanitizeFolderName(projectName)
	if safeName == "" {
		safeName = projectID[:8]
	}

	// Destination: /var/www/opendeploy/sites/<project-name>/
	destDir := filepath.Join(d.cfg.OutputRoot, "sites", safeName)

	// Remove old files
	os.RemoveAll(destDir)
	os.MkdirAll(destDir, 0755)

	// Copy files using cp -a for proper permissions
	ctx := context.Background()
	result, err := d.runner.Run(ctx, exec.RunOpts{
		JobType: "copy_frontend",
		Command: "/bin/cp",
		Args:    []string{"-a", srcPath + "/.", destDir},
		Timeout: 2 * time.Minute,
	})

	if err != nil || (result != nil && !result.Success) {
		// Fallback: try with sudo
		result, err = d.runner.Run(ctx, exec.RunOpts{
			JobType: "copy_frontend_sudo",
			Command: "/usr/bin/sudo",
			Args:    []string{"/bin/cp", "-a", srcPath + "/.", destDir},
			Timeout: 2 * time.Minute,
		})
		if err != nil {
			return "", fmt.Errorf("failed to copy frontend output: %w", err)
		}
	}

	return destDir, nil
}

// sanitizeFolderName converts a project name into a safe directory name
func sanitizeFolderName(name string) string {
	name = strings.ToLower(name)
	// Replace spaces and special chars with hyphens
	var result strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result.WriteRune(c)
		} else if c == ' ' || c == '.' {
			result.WriteRune('-')
		}
	}
	// Remove consecutive hyphens
	s := result.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// Rebuild stops the container, removes old code, and triggers a fresh deployment
func (d *DeployService) Rebuild(ctx context.Context, project *state.Project) (string, error) {
	// Stop and remove existing container if it exists
	if d.container != nil {
		containers, _ := d.container.ListContainers(project.ID)
		for _, container := range containers {
			d.logger.Info("stopping container for rebuild",
				zap.String("projectId", project.ID),
				zap.String("containerId", container.ContainerID),
			)
			d.container.StopContainer(ctx, project.ID)
			d.container.RemoveContainer(ctx, project.ID)
		}
	}

	// Remove old project directory
	projectDir := filepath.Join("/tmp", project.ID)
	os.RemoveAll(projectDir)

	d.logger.Info("rebuild initiated",
		zap.String("projectId", project.ID),
		zap.String("repoUrl", project.RepoURL),
	)

	// Trigger fresh deployment
	return d.Deploy(ctx, project)
}

func (d *DeployService) failDeploy(deploy *state.Deploy, errMsg string) {
	now := time.Now()
	deploy.Status = "failed"
	deploy.EndedAt = &now
	deploy.ExitCode = 1
	d.db.UpdateDeploy(deploy)
	d.logger.Error("deploy failed", zap.String("deployId", deploy.ID), zap.String("error", errMsg))

	// Broadcast failure so SSE/WebSocket clients know the deploy ended
	if d.broadcaster != nil {
		d.broadcaster.BroadcastToJob(deploy.ID, map[string]interface{}{
			"type":     "deploy_result",
			"deployId": deploy.ID,
			"status":   "failed",
			"error":    errMsg,
		})
	}
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

// logDirectoryListing logs a tree-style listing of the output directory.
func (d *DeployService) logDirectoryListing(dir, deployID string, logToDB func(string, string)) {
	logToDB("stdout", "Output files:")
	logToDB("stdout", "─────────────────────────────────")

	var totalSize int64
	var fileCount int

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}

		// Calculate indent based on depth
		depth := strings.Count(rel, string(os.PathSeparator))
		indent := strings.Repeat("  ", depth)
		name := filepath.Base(rel)

		if info.IsDir() {
			logToDB("stdout", fmt.Sprintf("%s📁 %s/", indent, name))
		} else {
			size := info.Size()
			totalSize += size
			fileCount++
			logToDB("stdout", fmt.Sprintf("%s   %s (%s)", indent, name, formatFileSize(size)))
		}
		return nil
	})

	if err != nil {
		logToDB("stderr", fmt.Sprintf("Failed to list output directory: %s", err.Error()))
		return
	}

	logToDB("stdout", "─────────────────────────────────")
	logToDB("stdout", fmt.Sprintf("Total: %d files (%s)", fileCount, formatFileSize(totalSize)))
}

func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
	)
	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
