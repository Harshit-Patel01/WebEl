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
	runner        *exec.Runner
	db            *state.DB
	cfg           config.DeployConfig
	logger        *zap.Logger
	lxd           *LXDService
	nginx         *NginxService
	container     *ContainerService
	broadcaster   exec.Broadcaster
	portAllocator *PortAllocator
	perfOptimizer *PerformanceOptimizer
}

// DeployOptions contains optional runtime configuration for a deployment
type DeployOptions struct {
	Domain            string
	ZoneID            string
	ManualDomain      bool
	EnableNginx       bool
	AttachToProjectID string // If attaching this backend to an existing frontend
}

func NewDeployService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *DeployService {
	container := NewContainerService(runner, db, cfg, logger)
	portAllocator := NewPortAllocator(db, cfg.PortPoolStart, cfg.PortPoolEnd)
	perfOptimizer := NewPerformanceOptimizer(runner, db, logger)
	return &DeployService{
		runner:        runner,
		db:            db,
		cfg:           cfg,
		logger:        logger,
		container:     container,
		portAllocator: portAllocator,
		perfOptimizer: perfOptimizer,
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

// SetLXDService sets the LXD service for managing LXD containers
func (d *DeployService) SetLXDService(lxd *LXDService) {
	d.lxd = lxd
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

	// Log to database immediately so user sees progress
	installLog := &state.DeployLog{
		DeployID:     jobID,
		Stream:       "stdout",
		Message:      "Installing dependencies with npm install...",
		LogTimestamp: time.Now(),
	}
	d.db.CreateDeployLog(installLog)
	if d.broadcaster != nil {
		d.broadcaster.BroadcastToJob(jobID, map[string]interface{}{
			"type":      "deploy_log",
			"deployId":  jobID,
			"stream":    "stdout",
			"message":   "Installing dependencies with npm install...",
			"timestamp": installLog.LogTimestamp,
		})
	}

	installResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-install",
		BroadcastJobID: jobID,
		JobType:        "npm_install",
		Command:        d.cfg.NpmBinary,
		Args:           []string{"install", "--prefer-offline", "--no-audit", "--progress=true"},
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

	d.logger.Info("running npm build", zap.String("dir", dir), zap.String("cmd", buildCmd))

	// Log to database immediately
	buildLog := &state.DeployLog{
		DeployID:     jobID,
		Stream:       "stdout",
		Message:      fmt.Sprintf("Running build command: npm run %s", buildCmd),
		LogTimestamp: time.Now(),
	}
	d.db.CreateDeployLog(buildLog)
	if d.broadcaster != nil {
		d.broadcaster.BroadcastToJob(jobID, map[string]interface{}{
			"type":      "deploy_log",
			"deployId":  jobID,
			"stream":    "stdout",
			"message":   buildLog.Message,
			"timestamp": buildLog.LogTimestamp,
		})
	}

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
		framework := d.lxd.DetectFramework(actualProjectDir)
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

		// Check if this is a Full Stack deployment
		isFullStack := project.ProjectType == "fullstack"

		if isFullStack {
			logToDB("stdout", "Full Stack deployment detected")
			logToDB("stdout", fmt.Sprintf("Frontend directory: %s", project.WorkingDirectory))
			logToDB("stdout", fmt.Sprintf("Backend directory: %s", project.BackendWorkingDirectory))

			frontendDir := project.WorkingDirectory
			if frontendDir == "" {
				frontendDir = "frontend"
			}

			backendDir := project.BackendWorkingDirectory
			if backendDir == "" {
				backendDir = "backend"
			}

			// Detect backend framework
			backendProjectDir := filepath.Join(projectDir, backendDir)
			backendFramework := d.lxd.DetectFramework(backendProjectDir)
			logToDB("stdout", fmt.Sprintf("Backend framework: %s", backendFramework))

			deploy.Framework = fmt.Sprintf("fullstack_%s", backendFramework)
			deploy.IsBackend = true

			backendContainerPort := GetDefaultPort(backendFramework)
			if project.LocalPort > 0 {
				backendContainerPort = project.LocalPort
			}

			backendInstallCmd := project.BackendInstallCommand
			if backendInstallCmd == "" {
				backendInstallCmd = GetDefaultInstallCommand(backendFramework)
			}

			startCmd := ""
			if project.StartCommand != nil && *project.StartCommand != "" {
				startCmd = *project.StartCommand
			} else {
				startCmd = GetDefaultStartCommand(backendFramework, backendContainerPort)
			}

			// ==================== LXD DEPLOYMENT ====================
			// Create single LXD container for both frontend and backend
			d.broadcastPhase(deployID, "build", "Creating LXD container...")
			logToDB("stdout", "Creating LXD container for full-stack deployment...")

			// Use Alpine as base image (lightweight)
			containerInfo, containerErr := d.lxd.CreateContainer(deployCtx, project.ID, project.Name, "alpine:3.19")
			if containerErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create LXD container: %s", containerErr.Error()))
				d.failDeploy(deploy, containerErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Container created: %s (ID: %s, IP: %s)", containerInfo.Name, containerInfo.ID, containerInfo.IP))

			// Find available host port for the container
			hostPort, portErr := d.portAllocator.AllocatePort("fullstack")
			if portErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to allocate host port: %s", portErr.Error()))
				d.failDeploy(deploy, portErr.Error())
				return
			}
			containerInfo.HostPort = hostPort
			containerInfo.ContainerPort = 80 // Frontend serves on port 80

			logToDB("stdout", fmt.Sprintf("Allocated host port: %d", hostPort))

			// Setup port proxy from host to container
			d.broadcastPhase(deployID, "service", "Setting up port proxy...")
			logToDB("stdout", "Setting up port proxy...")
			if err := d.lxd.SetupPortProxy(deployCtx, containerInfo.ID, containerInfo.ContainerPort, hostPort); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup port proxy: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", fmt.Sprintf("Port proxy configured: %d → %d", hostPort, containerInfo.ContainerPort))

			// Copy project files to container
			d.broadcastPhase(deployID, "build", "Copying project files...")
			logToDB("stdout", "Copying project files to container...")
			if err := d.lxd.CopyFilesToContainer(deployCtx, containerInfo.ID, projectDir, "/app"); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to copy files: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Project files copied successfully")

			// Install frontend dependencies
			d.broadcastPhase(deployID, "build", "Installing frontend dependencies...")
			logToDB("stdout", "Installing frontend dependencies...")
			frontendInstallCmd := "npm install --prefer-offline --no-audit --no-fund"
			if err := d.lxd.InstallInContainer(deployCtx, containerInfo.ID, frontendInstallCmd, envVars); err != nil {
				logToDB("stderr", fmt.Sprintf("Frontend installation failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Frontend dependencies installed")

			// Build frontend
			d.broadcastPhase(deployID, "build", "Building frontend...")
			logToDB("stdout", "Building frontend...")
			frontendBuildCmd := "npm run build"
			if project.OutputDir != "" {
				frontendBuildCmd = fmt.Sprintf("OUTPUT_DIR=%s npm run build", project.OutputDir)
			}
			_, err = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, frontendBuildCmd)
			if err != nil {
				logToDB("stderr", fmt.Sprintf("Frontend build failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Frontend built successfully")

			// Install backend dependencies
			d.broadcastPhase(deployID, "build", "Installing backend dependencies...")
			logToDB("stdout", "Installing backend dependencies...")
			if err := d.lxd.InstallInContainer(deployCtx, containerInfo.ID, backendInstallCmd, envVars); err != nil {
				logToDB("stderr", fmt.Sprintf("Backend installation failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Backend dependencies installed")

			// Run backend build command if provided
			if project.BackendBuildCommand != "" {
				d.broadcastPhase(deployID, "build", "Running backend build command...")
				logToDB("stdout", fmt.Sprintf("Running backend build command: %s", project.BackendBuildCommand))
				_, err = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, project.BackendBuildCommand)
				if err != nil {
					logToDB("stderr", fmt.Sprintf("Backend build command failed: %s", err.Error()))
					d.failDeploy(deploy, err.Error())
					return
				}
				logToDB("stdout", "Backend build command completed successfully")
			}

			// Start backend service
			d.broadcastPhase(deployID, "service", "Starting backend service...")
			logToDB("stdout", "Starting backend service...")
			if err := d.lxd.StartService(deployCtx, containerInfo.ID, startCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to start backend: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Backend service started")

			// Configure nginx for full-stack
			if opts != nil && opts.EnableNginx && opts.Domain != "" {
				d.broadcastPhase(deployID, "service", "Configuring nginx...")
				logToDB("stdout", "Configuring nginx for full-stack deployment...")

				domain := opts.Domain

				// Create frontend config (serves on /)
				frontendCfg := NginxSiteConfig{
					Domain:               domain,
					FrontendPath:         "",
					ProxyEnabled:         false,
					ProxyPort:            0,
					FrontendProxyEnabled: true,
					FrontendProxyPort:    hostPort,
				}
				frontendConfigContent := d.nginx.GenerateFrontendConfig(frontendCfg)
				frontendConfigName := fmt.Sprintf("frontend-%s", domain)

				if err := d.nginx.WriteConfig(frontendConfigName, frontendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Frontend nginx config failed: %s", err.Error()))
				} else {
					logToDB("stdout", fmt.Sprintf("Frontend nginx config written: %s", frontendConfigName))
				}

				// Create backend config (serves on /api/)
				backendCfg := NginxSiteConfig{
					Domain:               domain,
					FrontendPath:         "",
					ProxyEnabled:         true,
					ProxyPort:            hostPort,
					FrontendProxyEnabled: false,
					FrontendProxyPort:    0,
				}
				backendConfigContent := d.nginx.GenerateBackendConfig(backendCfg)
				backendConfigName := fmt.Sprintf("backend-%s", domain)

				if err := d.nginx.WriteConfig(backendConfigName, backendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Backend nginx config failed: %s", err.Error()))
				} else {
					logToDB("stdout", fmt.Sprintf("Backend nginx config written: %s", backendConfigName))
				}

				// Test and reload nginx
				testResult, err := d.nginx.TestConfig(deployCtx)
				if err != nil {
					logToDB("stderr", fmt.Sprintf("Nginx config test failed: %s", err.Error()))
				} else if !testResult.Success {
					logToDB("stderr", fmt.Sprintf("Nginx config test failed: %s", testResult.Output))
				} else {
					if err := d.nginx.Reload(deployCtx); err != nil {
						logToDB("stderr", fmt.Sprintf("Nginx reload failed: %s", err.Error()))
					} else {
						logToDB("stdout", fmt.Sprintf("Nginx configured: frontend at /, backend at /api (host port %d)", hostPort))
					}
				}
			}

			// Save container info to database
			container := &state.Container{
				ProjectID:    project.ID,
				Name:         containerInfo.Name,
				Image:        "alpine:3.19",
				ContainerID:  containerInfo.ID,
				Status:       "running",
				PortMappings: fmt.Sprintf(`{"host":"%d","container":"80"}`, hostPort),
			}
			if err := d.db.CreateContainer(container); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to save container: %s", err.Error()))
			}

			logToDB("stdout", fmt.Sprintf("Full Stack deployment completed! Container: %s, Host Port: %d", containerInfo.Name, hostPort))

			// Configure nginx for Full Stack - create separate configs for frontend and backend
			if opts != nil && opts.EnableNginx && opts.Domain != "" {
				logToDB("stdout", "Configuring nginx for Full Stack deployment...")

				domain := opts.Domain

				// Create frontend config (serves on /)
				frontendCfg := NginxSiteConfig{
					Domain:               domain,
					FrontendPath:         "",
					ProxyEnabled:         false,
					ProxyPort:            0,
					FrontendProxyEnabled: true,
					FrontendProxyPort:    hostPort,
				}
				frontendConfigContent := d.nginx.GenerateFrontendConfig(frontendCfg)
				frontendConfigName := fmt.Sprintf("frontend-%s", domain)

				if err := d.nginx.WriteConfig(frontendConfigName, frontendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Frontend nginx config failed: %s", err.Error()))
				} else {
					logToDB("stdout", fmt.Sprintf("Frontend nginx config written: %s", frontendConfigName))
				}

				// Create backend config (serves on /api/)
				backendCfg := NginxSiteConfig{
					Domain:               domain,
					FrontendPath:         "",
					ProxyEnabled:         true,
					ProxyPort:            hostPort,
					FrontendProxyEnabled: false,
					FrontendProxyPort:    0,
				}
				backendConfigContent := d.nginx.GenerateBackendConfig(backendCfg)
				backendConfigName := fmt.Sprintf("backend-%s", domain)

				if err := d.nginx.WriteConfig(backendConfigName, backendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Backend nginx config failed: %s", err.Error()))
				} else {
					logToDB("stdout", fmt.Sprintf("Backend nginx config written: %s", backendConfigName))
				}

				// Test and reload nginx
				testResult, err := d.nginx.TestConfig(deployCtx)
				if err != nil {
					logToDB("stderr", fmt.Sprintf("Nginx config test failed: %s", err.Error()))
				} else if !testResult.Success {
					logToDB("stderr", fmt.Sprintf("Nginx config test failed: %s", testResult.Output))
				} else {
					if err := d.nginx.Reload(deployCtx); err != nil {
						logToDB("stderr", fmt.Sprintf("Nginx reload failed: %s", err.Error()))
					} else {
						logToDB("stdout", fmt.Sprintf("Nginx configured: frontend at /, backend at /api (host port %d)", hostPort))
					}
				}
			}

			// Success
			now := time.Now()
			buildDuration := now.Sub(buildStart)
			buildDurationSeconds := buildDuration.Seconds()
			deploy.Status = "success"
			deploy.EndedAt = &now
			deploy.ExitCode = 0
			deploy.BuildDuration = buildDurationSeconds
			d.db.UpdateDeploy(deploy)

			// Record performance statistics for auto-optimization
			if d.perfOptimizer != nil {
				d.perfOptimizer.RecordBuildStats(buildDuration)
			}

			logToDB("stdout", "")
			logToDB("stdout", fmt.Sprintf("Full Stack deployment completed successfully! (%.1fs)", buildDurationSeconds))

			if d.broadcaster != nil {
				d.broadcaster.BroadcastToJob(deployID, map[string]interface{}{
					"type":          "deploy_result",
					"deployId":      deployID,
					"status":        "success",
					"framework":     deploy.Framework,
					"isBackend":     true,
					"buildDuration": buildDuration,
				})
			}
			d.broadcastPhase(deployID, "done", "Full Stack deploy complete!")
			return
		}

		// Override projectType based on framework for pure static sites
		if framework == FrameworkStatic {
			projectType = ProjectStatic
		}

		d.broadcastPhase(deployID, "build", "Building project...")
		logToDB("stdout", "Starting build process...")

		isBackend := IsBackendFramework(framework)
		useLXD := true // Always use LXD for all deployments

		logToDB("stdout", fmt.Sprintf("Deployment mode: %s (LXD: %v)", map[bool]string{true: "backend", false: "frontend"}[isBackend], useLXD))

		if useLXD {
			// LXD path for ALL deployments (frontend and backend)
			logToDB("stdout", "Creating LXD container...")

			// Frontend containers always expose port 80 internally.
			// Backend containers expose their framework default port.
			containerPort := 80 // Frontend serve port
			if isBackend {
				containerPort = GetDefaultPort(framework)
				if project.LocalPort > 0 {
					containerPort = project.LocalPort
				}
			}

			installCmd := ""
			if project.InstallCommand != nil && *project.InstallCommand != "" {
				installCmd = *project.InstallCommand
			} else {
				installCmd = GetDefaultInstallCommand(framework)
			}

			startCmd := ""
			if isBackend {
				if project.StartCommand != nil && *project.StartCommand != "" {
					startCmd = *project.StartCommand
				} else {
					startCmd = GetDefaultStartCommand(framework, containerPort)
				}
			}

			// Create LXD container
			d.broadcastPhase(deployID, "build", "Creating LXD container...")
			containerInfo, containerErr := d.lxd.CreateContainer(deployCtx, project.ID, project.Name, "alpine:3.19")
			if containerErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create LXD container: %s", containerErr.Error()))
				d.failDeploy(deploy, containerErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Container created: %s (ID: %s, IP: %s)", containerInfo.Name, containerInfo.ID, containerInfo.IP))

			// Find available host port for the container
			hostPort, portErr := d.portAllocator.AllocatePort(string(projectType))
			if portErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to allocate host port: %s", portErr.Error()))
				d.failDeploy(deploy, portErr.Error())
				return
			}
			containerInfo.HostPort = hostPort
			containerInfo.ContainerPort = containerPort

			logToDB("stdout", fmt.Sprintf("Allocated host port: %d", hostPort))

			// Setup port proxy from host to container
			d.broadcastPhase(deployID, "service", "Setting up port proxy...")
			logToDB("stdout", "Setting up port proxy...")
			if err := d.lxd.SetupPortProxy(deployCtx, containerInfo.ID, containerInfo.ContainerPort, hostPort); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup port proxy: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", fmt.Sprintf("Port proxy configured: %d → %d", hostPort, containerInfo.ContainerPort))

			// Copy project files to container
			projectPath := filepath.Join("/tmp", project.ID)
			if workingDir != "" && workingDir != "." {
				projectPath = filepath.Join(projectPath, workingDir)
			}

			d.broadcastPhase(deployID, "build", "Copying project files...")
			logToDB("stdout", "Copying project files to container...")
			if err := d.lxd.CopyFilesToContainer(deployCtx, containerInfo.ID, projectPath, "/app"); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to copy files: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Project files copied successfully")

			// Install dependencies in container
			d.broadcastPhase(deployID, "build", "Installing dependencies...")
			logToDB("stdout", "Installing dependencies in container...")
			if err := d.lxd.InstallInContainer(deployCtx, containerInfo.ID, installCmd, envVars); err != nil {
				logToDB("stderr", fmt.Sprintf("Installation failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Dependencies installed")

			// Start service in container if it's a backend
			if isBackend {
				d.broadcastPhase(deployID, "service", "Starting service...")
				logToDB("stdout", "Starting service in container...")
				if err := d.lxd.StartService(deployCtx, containerInfo.ID, startCmd); err != nil {
					logToDB("stderr", fmt.Sprintf("Failed to start service: %s", err.Error()))
					d.failDeploy(deploy, err.Error())
					return
				}
				logToDB("stdout", "Service started")
			}

			// Save container info to database
			container := &state.Container{
				ProjectID:    project.ID,
				Name:         containerInfo.Name,
				Image:        "alpine:3.19",
				ContainerID:  containerInfo.ID,
				Status:       "running",
				PortMappings: fmt.Sprintf(`{"host":"%d","container":"%d"}`, hostPort, containerPort),
			}
			if err := d.db.CreateContainer(container); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to save container: %s", err.Error()))
				// Continue anyway, don't fail the deploy just for DB error
			}

			logToDB("stdout", fmt.Sprintf("LXD deployment completed! Container: %s, Host Port: %d", containerInfo.Name, hostPort))

			// Set proxy ports for nginx configuration
			frontendProxyPort := 0
			backendProxyPort := hostPort // For single container deployments, backend uses the allocated port
			if !isBackend {
				frontendProxyPort = hostPort
			}

			// Configure nginx if domain or AttachToProjectID is provided
			if opts != nil && opts.EnableNginx && (opts.Domain != "" || opts.AttachToProjectID != "") {
				logToDB("stdout", "Configuring nginx...")

				domainToUse := opts.Domain
				// If attaching to an existing frontend project, we create a separate backend config that shares the domain
				if opts.AttachToProjectID != "" && isBackend {
					frontendProj, err := d.db.GetProject(opts.AttachToProjectID)
					if err == nil && frontendProj != nil && frontendProj.Domain != "" {
						domainToUse = frontendProj.Domain
						logToDB("stdout", fmt.Sprintf("Creating backend config for domain: %s", domainToUse))

						// No need to fetch frontend port - the frontend has its own config
						// We just need our backend proxy port
					} else {
						logToDB("stderr", "Warning: Could not find domain for attached frontend project")
					}
				}

				if domainToUse != "" {
					if err := d.applyNginxForDeploy(deployCtx, project, domainToUse, "", isBackend, 0, backendProxyPort); err != nil {
						logToDB("stderr", fmt.Sprintf("Nginx configuration failed: %s", err.Error()))
						d.logger.Error("nginx apply failed", zap.String("deployId", deployID), zap.String("domain", domainToUse), zap.Error(err))
					} else {
						configType := "combined"
						if isBackend {
							configType = "backend"
						} else if !isBackend && frontendProxyPort > 0 {
							configType = "frontend"
						}
						logToDB("stdout", fmt.Sprintf("Nginx %s config configured for %s", configType, domainToUse))
					}
				}
			}

			// SUCCESS
			now := time.Now()
			buildDuration := now.Sub(buildStart)
			buildDurationSeconds := buildDuration.Seconds()

			// Record performance statistics for auto-optimization
			if d.perfOptimizer != nil {
				d.perfOptimizer.RecordBuildStats(buildDuration)
			}
			deploy.Status = "success"
			deploy.EndedAt = &now
			deploy.ExitCode = 0
			deploy.BuildDuration = buildDurationSeconds
			d.db.UpdateDeploy(deploy)

			d.logger.Info("deploy completed", zap.String("deployId", deployID), zap.String("projectId", project.ID), zap.String("framework", string(framework)), zap.Float64("buildDuration", buildDurationSeconds))

			logToDB("stdout", "")
			logToDB("stdout", fmt.Sprintf("Deployment completed successfully! (%.1fs)", buildDurationSeconds))

			if d.broadcaster != nil {
				d.broadcaster.BroadcastToJob(deployID, map[string]interface{}{
					"type":          "deploy_result",
					"deployId":      deployID,
					"status":        "success",
					"framework":     string(framework),
					"isBackend":     isBackend,
					"buildDuration": buildDuration,
				})
			}
			d.broadcastPhase(deployID, "done", "Deploy complete!")

		} else {
			logToDB("stdout", "Using native build")

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

			// Start native backend service
			if isBackend {
				d.broadcastPhase(deployID, "service", "Starting backend service...")
				logToDB("stdout", "Starting backend service...")

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
			} else {
				// Frontend native: copy to nginx
				d.broadcastPhase(deployID, "service", "Deploying frontend...")
				logToDB("stdout", "Deploying frontend static files...")
				outputPath, copyErr := d.copyFrontendToNginx(project.ID, project.Name, workingDir, project.OutputDir)
				if copyErr != nil {
					logToDB("stderr", fmt.Sprintf("Failed to deploy frontend: %s", copyErr.Error()))
					d.failDeploy(deploy, copyErr.Error())
					return
				}
				logToDB("stdout", fmt.Sprintf("Frontend deployed to: %s", outputPath))
			}

			// SUCCESS
			now := time.Now()
			buildDuration := now.Sub(buildStart)
			buildDurationSeconds := buildDuration.Seconds()

			// Record performance statistics for auto-optimization
			if d.perfOptimizer != nil {
				d.perfOptimizer.RecordBuildStats(buildDuration)
			}
			deploy.Status = "success"
			deploy.EndedAt = &now
			deploy.ExitCode = 0
			deploy.BuildDuration = buildDurationSeconds
			d.db.UpdateDeploy(deploy)

			d.logger.Info("deploy completed", zap.String("deployId", deployID), zap.String("projectId", project.ID), zap.String("framework", string(framework)), zap.Float64("buildDuration", buildDurationSeconds))

			logToDB("stdout", "")
			logToDB("stdout", fmt.Sprintf("Deployment completed successfully! (%.1fs)", buildDurationSeconds))

			if d.broadcaster != nil {
				d.broadcaster.BroadcastToJob(deployID, map[string]interface{}{
					"type":          "deploy_result",
					"deployId":      deployID,
					"status":        "success",
					"framework":     string(framework),
					"isBackend":     isBackend,
					"buildDuration": buildDuration,
				})
			}
			d.broadcastPhase(deployID, "done", "Deploy complete!")
		}
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
