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
	logBuildOutput := func(result *exec.ExecResult) {
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
	logBuildOutput(installResult)

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
	logBuildOutput(buildResult)

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
		buildStart := time.Now()

		// Create a new context for the deployment (not tied to the HTTP request)
		deployCtx := context.Background()

		// Skip host-side cloning - will clone inside containers
		logToDB("stdout", "Starting deployment...")
		logToDB("stdout", fmt.Sprintf("Repository: %s", project.RepoURL))
		logToDB("stdout", fmt.Sprintf("Branch: %s", project.Branch))

		// Detect framework and working directory (needed for single-service deployments)
		workingDir := project.WorkingDirectory
		if workingDir == "" || workingDir == "." {
			workingDir = "."
		}
		logToDB("stdout", fmt.Sprintf("Working directory: %s", workingDir))

		// For LXD deployments, framework will be detected AFTER cloning inside container
		// Set default values here, will be updated after clone
		framework := FrameworkUnknown
		deploy.Framework = string(framework)
		deploy.IsBackend = false

		// Determine project type
		projectType := ProjectType(project.ProjectType)
		if projectType == "" {
			projectType = ProjectStatic
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

			// Use default framework for full-stack frontend
			backendFramework := FrameworkNode
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

			// ==================== FRONTEND CONTAINER ====================
			d.broadcastPhase(deployID, "build", "Creating frontend container...")
			logToDB("stdout", "Creating LXD container for frontend...")

			frontendContainerInfo, frontendErr := d.lxd.CreateContainer(deployCtx, project.ID+"-frontend", project.Name+"-frontend", "images:alpine/3.20")
			if frontendErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create frontend container: %s", frontendErr.Error()))
				d.failDeploy(deploy, frontendErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Frontend container created: %s (ID: %s)", frontendContainerInfo.Name, frontendContainerInfo.ID))

			// Clone repository in frontend container FIRST
			logToDB("stdout", "Cloning repository in frontend container...")
			frontendCloneCmd := fmt.Sprintf("mkdir -p /app && cd /app && git clone --branch %s --depth 1 %s repo", project.Branch, project.RepoURL)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, frontendCloneCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to clone repository in frontend container: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Repository cloned in frontend container")

			// Install base dependencies first (git, node, npm, nginx, etc.)
			d.broadcastPhase(deployID, "build", "Installing base dependencies...")
			logToDB("stdout", "Installing base dependencies in frontend container...")

			// Install minimal deps then framework-specific for frontend
			minimalDepsCmd := "apk update && apk add --no-cache git curl bash ca-certificates"
			minimalResult, _ := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, minimalDepsCmd)
			if minimalResult != nil && minimalResult.ExitCode != 0 {
				logToDB("stderr", "Failed to install minimal dependencies")
				for _, line := range minimalResult.Lines {
					logToDB(line.Stream, line.Text)
				}
				d.failDeploy(deploy, "Failed to install minimal dependencies")
				return
			}
			if minimalResult != nil {
				for _, line := range minimalResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}

			// Install Node.js, npm, and nginx for frontend
			nodeDepsCmd := "apk add --no-cache nodejs npm nginx"
			nodeResult, _ := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, nodeDepsCmd)
			if nodeResult != nil && nodeResult.ExitCode != 0 {
				logToDB("stderr", "Failed to install Node.js and nginx")
				for _, line := range nodeResult.Lines {
					logToDB(line.Stream, line.Text)
				}
				d.failDeploy(deploy, "Failed to install Node.js and nginx")
				return
			}
			if nodeResult != nil {
				for _, line := range nodeResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			logToDB("stdout", "Base dependencies installed successfully")

			// Allocate frontend port
			frontendHostPort, frontendPortErr := d.portAllocator.AllocatePort("frontend")
			if frontendPortErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to allocate frontend port: %s", frontendPortErr.Error()))
				d.failDeploy(deploy, frontendPortErr.Error())
				return
			}
			logToDB("stdout", fmt.Sprintf("Allocated frontend host port: %d", frontendHostPort))

			// Setup frontend port proxy (container port 80 -> host port)
			if err := d.lxd.SetupPortProxy(deployCtx, frontendContainerInfo.ID, 80, frontendHostPort); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup frontend port proxy: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}

			// Build frontend
			d.broadcastPhase(deployID, "build", "Building frontend...")
			logToDB("stdout", "Building frontend...")
			frontendWorkDir := fmt.Sprintf("/app/repo/%s", frontendDir)
			frontendBuildCmd := fmt.Sprintf("cd %s && npm install --prefer-offline --no-audit && npm run build", frontendWorkDir)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, frontendBuildCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Frontend build failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}

			// Configure nginx for frontend (already installed with base deps)
			logToDB("stdout", "Configuring nginx in frontend container...")
			frontendOutputPath := fmt.Sprintf("/app/repo/%s/dist", frontendDir)
			if project.OutputDir != "" {
				frontendOutputPath = fmt.Sprintf("/app/repo/%s/%s", frontendDir, project.OutputDir)
			}

			nginxSetupCmd := fmt.Sprintf(`
				mkdir -p /run/nginx && \
				echo 'server { listen 80; root %s; index index.html; location / { try_files $uri $uri/ /index.html; } }' > /etc/nginx/http.d/default.conf && \
				nginx -t && nginx -g 'daemon off;' >/dev/null 2>&1 &
			`, frontendOutputPath)

			if _, err := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, nginxSetupCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup nginx in frontend container: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Frontend container ready")

			// ==================== BACKEND CONTAINER ====================
			d.broadcastPhase(deployID, "build", "Creating backend container...")
			logToDB("stdout", "Creating LXD container for backend...")

			backendContainerInfo, backendErr := d.lxd.CreateContainer(deployCtx, project.ID+"-backend", project.Name+"-backend", "images:alpine/3.20")
			if backendErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create backend container: %s", backendErr.Error()))
				d.failDeploy(deploy, backendErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Backend container created: %s (ID: %s)", backendContainerInfo.Name, backendContainerInfo.ID))

			// Install base dependencies FIRST
			d.broadcastPhase(deployID, "build", "Installing base dependencies...")
			logToDB("stdout", "Installing base dependencies in backend container...")

			// Install minimal deps first
			backendMinDepsCmd := "apk update && apk add --no-cache git curl bash ca-certificates"
			backendMinResult, _ := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, backendMinDepsCmd)
			if backendMinResult != nil && backendMinResult.ExitCode != 0 {
				logToDB("stderr", "Failed to install minimal dependencies")
				for _, line := range backendMinResult.Lines {
					logToDB(line.Stream, line.Text)
				}
				d.failDeploy(deploy, "Failed to install minimal dependencies")
				return
			}
			if backendMinResult != nil {
				for _, line := range backendMinResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}

			// Install backend deps (Node.js, Python, Go)
			backendDepsCmd := "apk add --no-cache nodejs npm python3 py3-pip go"
			backendResult, _ := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, backendDepsCmd)
			if backendResult != nil && backendResult.ExitCode != 0 {
				logToDB("stderr", "Failed to install backend dependencies")
				for _, line := range backendResult.Lines {
					logToDB(line.Stream, line.Text)
				}
				d.failDeploy(deploy, "Failed to install backend dependencies")
				return
			}
			if backendResult != nil {
				for _, line := range backendResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			logToDB("stdout", "Base dependencies installed successfully")

			// Allocate backend port
			backendHostPort, backendPortErr := d.portAllocator.AllocatePort("backend")
			if backendPortErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to allocate backend port: %s", backendPortErr.Error()))
				d.failDeploy(deploy, backendPortErr.Error())
				return
			}
			logToDB("stdout", fmt.Sprintf("Allocated backend host port: %d", backendHostPort))

			// Setup backend port proxy (container port -> host port)
			if err := d.lxd.SetupPortProxy(deployCtx, backendContainerInfo.ID, backendContainerPort, backendHostPort); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup backend port proxy: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}

			// Clone repository in backend container
			logToDB("stdout", "Cloning repository in backend container...")
			backendCloneCmd := fmt.Sprintf("mkdir -p /app && cd /app && git clone --branch %s --depth 1 %s repo", project.Branch, project.RepoURL)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, backendCloneCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to clone repository in backend container: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}

			// Install backend dependencies
			backendWorkDir := fmt.Sprintf("/app/repo/%s", backendDir)
			installBackendCmd := fmt.Sprintf("cd %s && %s", backendWorkDir, backendInstallCmd)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, installBackendCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Backend installation failed: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}

			// Run backend build command if provided
			if project.BackendBuildCommand != "" {
				buildBackendCmd := fmt.Sprintf("cd %s && %s", backendWorkDir, project.BackendBuildCommand)
				if _, err := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, buildBackendCmd); err != nil {
					logToDB("stderr", fmt.Sprintf("Backend build failed: %s", err.Error()))
					d.failDeploy(deploy, err.Error())
					return
				}
			}

			// Write environment variables to .env file
			if len(envVars) > 0 {
				envContent := ""
				for k, v := range envVars {
					envContent += fmt.Sprintf("%s=%s\n", k, v)
				}
				writeEnvCmd := fmt.Sprintf("cd %s && cat > .env << 'EOF'\n%sEOF", backendWorkDir, envContent)
				d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, writeEnvCmd)
			}

			// Start backend service
			startBackendCmd := fmt.Sprintf("cd %s && nohup %s > /var/log/app.log 2>&1 &", backendWorkDir, startCmd)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, startBackendCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to start backend: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Backend service started")

			// Save both containers to database
			frontendContainer := &state.Container{
				ProjectID:    project.ID,
				Name:         frontendContainerInfo.Name,
				Image:        "images:alpine/3.20",
				ContainerID:  frontendContainerInfo.ID,
				Status:       "running",
				PortMappings: fmt.Sprintf(`{"host":"%d","container":"80"}`, frontendHostPort),
			}
			d.db.CreateContainer(frontendContainer)

			backendContainer := &state.Container{
				ProjectID:    project.ID,
				Name:         backendContainerInfo.Name,
				Image:        "images:alpine/3.20",
				ContainerID:  backendContainerInfo.ID,
				Status:       "running",
				PortMappings: fmt.Sprintf(`{"host":"%d","container":"%d"}`, backendHostPort, backendContainerPort),
			}
			d.db.CreateContainer(backendContainer)

			// Configure host nginx if domain provided
			if opts != nil && opts.EnableNginx && opts.Domain != "" {
				logToDB("stdout", "Configuring host nginx for domain routing...")
				domain := opts.Domain

				// Frontend config
				frontendCfg := NginxSiteConfig{
					Domain:               domain,
					FrontendProxyEnabled: true,
					FrontendProxyPort:    frontendHostPort,
				}
				frontendConfigContent := d.nginx.GenerateFrontendConfig(frontendCfg)
				if err := d.nginx.WriteConfig(fmt.Sprintf("frontend-%s", domain), frontendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Frontend nginx config failed: %s", err.Error()))
				}

				// Backend config
				backendCfg := NginxSiteConfig{
					Domain:       domain,
					ProxyEnabled: true,
					ProxyPort:    backendHostPort,
				}
				backendConfigContent := d.nginx.GenerateBackendConfig(backendCfg)
				if err := d.nginx.WriteConfig(fmt.Sprintf("backend-%s", domain), backendConfigContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Backend nginx config failed: %s", err.Error()))
				}

				// Reload nginx
				if testResult, err := d.nginx.TestConfig(deployCtx); err == nil && testResult.Success {
					d.nginx.Reload(deployCtx)
					logToDB("stdout", fmt.Sprintf("Nginx configured: frontend port %d, backend port %d", frontendHostPort, backendHostPort))
				}
			}

			logToDB("stdout", fmt.Sprintf("Full Stack deployment completed! Frontend: %d, Backend: %d", frontendHostPort, backendHostPort))

			// Success
			now := time.Now()
			buildDuration := now.Sub(buildStart)
			buildDurationSeconds := buildDuration.Seconds()
			deploy.Status = "success"
			deploy.EndedAt = &now
			deploy.ExitCode = 0
			deploy.BuildDuration = buildDurationSeconds
			d.db.UpdateDeploy(deploy)

			// Record performance statistics
			if d.perfOptimizer != nil {
				d.perfOptimizer.RecordBuildStats(buildDuration)
			}

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
			containerInfo, containerErr := d.lxd.CreateContainer(deployCtx, project.ID, project.Name, "images:alpine/3.20")
			if containerErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create LXD container: %s", containerErr.Error()))
				d.failDeploy(deploy, containerErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Container created: %s (ID: %s, IP: %s)", containerInfo.Name, containerInfo.ID, containerInfo.IP))

			// STEP 1: Clone repository directly inside the container FIRST
			d.broadcastPhase(deployID, "build", "Cloning repository in container...")
			logToDB("stdout", "Cloning repository inside container...")

			cloneCmd := fmt.Sprintf("apk update && apk add --no-cache git && mkdir -p /app && cd /app && git clone --branch %s --depth 1 %s repo", project.Branch, project.RepoURL)
			cloneResult, cloneErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, cloneCmd)
			if cloneErr != nil || !cloneResult.Success {
				logToDB("stderr", fmt.Sprintf("Failed to clone repository: %s", cloneErr))
				if cloneResult != nil {
					for _, line := range cloneResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				d.failDeploy(deploy, "Failed to clone repository")
				return
			}
			logToDB("stdout", "Repository cloned successfully in container")

			// STEP 2: Determine working directory inside container
			// Root is at /app/repo, user-specified working dir is appended
			workDir := "/app/repo"
			if workingDir != "" && workingDir != "." {
				workDir = fmt.Sprintf("/app/repo/%s", workingDir)
			}
			logToDB("stdout", fmt.Sprintf("Working directory: %s", workDir))

			// STEP 3: Install base dependencies BEFORE framework detection
			// We need git, node, npm, python, etc. available before we can check files
			d.broadcastPhase(deployID, "build", "Installing base dependencies...")
			logToDB("stdout", "Installing base dependencies in container...")

			// First install minimal deps for framework detection
			logToDB("stdout", "Installing git, curl, bash, and ca-certificates...")
			minimalDepsCmd := "apk update && apk add --no-cache git curl bash ca-certificates"
			minimalResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, minimalDepsCmd)
			if minimalResult != nil && minimalResult.ExitCode != 0 {
				logToDB("stderr", "Failed to install minimal dependencies")
				for _, line := range minimalResult.Lines {
					logToDB(line.Stream, line.Text)
				}
				d.failDeploy(deploy, "Failed to install minimal dependencies")
				return
			}
			// Log all output from minimal deps install
			if minimalResult != nil {
				for _, line := range minimalResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			logToDB("stdout", "Minimal dependencies installed successfully")

			// STEP 4: Detect framework by checking for specific files inside container
			d.broadcastPhase(deployID, "detect", "Detecting framework...")
			logToDB("stdout", "Detecting framework from cloned files...")

			// Use the LXD service's framework detection helper
			framework = d.lxd.DetectFrameworkInContainer(deployCtx, containerInfo.ID, workDir)

			d.logger.Info("detected framework",
				zap.String("projectId", project.ID),
				zap.String("framework", string(framework)),
			)
			logToDB("stdout", fmt.Sprintf("Detected framework: %s", framework))

			// STEP 5: Install framework-specific dependencies
			// Now that we know the framework, install only what's needed
			logToDB("stdout", fmt.Sprintf("Installing dependencies for %s framework...", framework))

			err = d.lxd.InstallDependencies(deployCtx, containerInfo.ID, framework, false)
			if err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to install framework dependencies: %v", err))
				d.failDeploy(deploy, fmt.Sprintf("Failed to install framework dependencies: %v", err))
				return
			}
			logToDB("stdout", "Framework dependencies installed successfully")

			d.logger.Info("detected framework",
				zap.String("projectId", project.ID),
				zap.String("framework", string(framework)),
			)
			logToDB("stdout", fmt.Sprintf("Detected framework: %s", framework))

			// Store framework info on the deploy record
			deploy.Framework = string(framework)
			deploy.IsBackend = IsBackendFramework(framework)

			// Get install and start commands based on detected framework
			installCmd = ""
			if project.InstallCommand != nil && *project.InstallCommand != "" {
				installCmd = *project.InstallCommand
			} else {
				installCmd = GetDefaultInstallCommand(framework)
			}

			startCmd = ""
			if deploy.IsBackend {
				if project.StartCommand != nil && *project.StartCommand != "" {
					startCmd = *project.StartCommand
				} else {
					startCmd = GetDefaultStartCommand(framework, containerPort)
				}
			}

			containerPort = 80 // Default for frontend
			if deploy.IsBackend {
				containerPort = GetDefaultPort(framework)
				if project.LocalPort > 0 {
					containerPort = project.LocalPort
				}
			}

			// STEP 5: Allocate host port for the container
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

			// STEP 6: Install project dependencies
			d.broadcastPhase(deployID, "build", "Installing project dependencies...")
			logToDB("stdout", "Installing project dependencies in container...")

			installProjectCmd := fmt.Sprintf("cd %s && %s", workDir, installCmd)
			installResult, installErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, installProjectCmd)
			if installErr != nil || !installResult.Success {
				logToDB("stderr", fmt.Sprintf("Failed to install project dependencies: %s", installErr))
				if installResult != nil {
					for _, line := range installResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				d.failDeploy(deploy, "Failed to install project dependencies")
				return
			}
			logToDB("stdout", "Project dependencies installed successfully")

			// STEP 7: Build project
			// Build is needed for: frontend frameworks (React, Vue, etc.), Next.js, Nuxt.js, or when explicitly specified
			needsBuild := !deploy.IsBackend // Frontend needs build
			if project.BuildCommand != "" && project.BuildCommand != "skip" {
				needsBuild = true // User wants to build
			}
			if project.BuildCommand == "skip" {
				needsBuild = false // User wants to skip
			}

			if needsBuild {
				d.broadcastPhase(deployID, "build", "Building project...")
				logToDB("stdout", "Building project in container...")

				// Get framework-specific build command
				buildCmd := ""
				switch framework {
				case FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix:
					buildCmd = "npm run build"
				case FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkVite, FrameworkWebpack:
					buildCmd = "npm run build"
				case FrameworkExpress, FrameworkFastify, FrameworkNode:
					// These are backend, may not need build unless specified
					buildCmd = ""
				case FrameworkGo:
					// Go needs build for backend
					if deploy.IsBackend {
						buildCmd = "go build -o server ."
					}
				case FrameworkFlask, FrameworkDjango, FrameworkFastAPI:
					// Python projects may not have a build step
					buildCmd = ""
				default:
					// Unknown - try build command for frontend
					if !deploy.IsBackend {
						buildCmd = "npm run build"
					}
				}

				// Override with user-provided build command if specified
				if project.BuildCommand != "" && project.BuildCommand != "skip" {
					buildCmd = project.BuildCommand
				}

				if buildCmd != "" {
					logToDB("stdout", fmt.Sprintf("Running: cd %s && %s", workDir, buildCmd))
					buildProjectCmd := fmt.Sprintf("cd %s && %s", workDir, buildCmd)
					buildResult, buildErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, buildProjectCmd)

					// Log build output
					if buildResult != nil {
						for _, line := range buildResult.Lines {
							logToDB(line.Stream, line.Text)
						}
					}

					if buildErr != nil || (buildResult != nil && buildResult.ExitCode != 0) {
						logToDB("stderr", fmt.Sprintf("Failed to build project: %s", buildErr))
						d.failDeploy(deploy, "Failed to build project")
						return
					}
					logToDB("stdout", "Project built successfully")
				} else {
					logToDB("stdout", "No build command for this framework type")
				}
			}

			// Configure container to auto-start on boot
			d.broadcastPhase(deployID, "service", "Configuring container auto-start...")
			logToDB("stdout", "Configuring container to auto-start on system boot...")
			autostartCmd := fmt.Sprintf("lxc config set %s boot.autostart true", containerInfo.ID)
			autostarResult, _ := d.runner.Run(deployCtx, exec.RunOpts{
				JobType: "lxd_autostart",
				Command: autostartCmd,
				Timeout: 10 * time.Second,
			})
			if autostarResult != nil {
				for _, line := range autostarResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			logToDB("stdout", "Container configured to auto-start on boot")

			// STEP 8: Start service or configure nginx
			if deploy.IsBackend {
				d.broadcastPhase(deployID, "service", "Starting service...")
				logToDB("stdout", "Creating OpenRC service file for auto-restart...")

				// Write environment variables to .env file
				if len(envVars) > 0 {
					envContent := ""
					for k, v := range envVars {
						envContent += fmt.Sprintf("%s=%s\n", k, v)
					}
					writeEnvCmd := fmt.Sprintf("cd %s && cat > .env << 'EOF'\n%sEOF", workDir, envContent)
					_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, writeEnvCmd)
				}

				// Create OpenRC service file for the backend
				serviceName := "opendeploy-app"
				serviceFile := fmt.Sprintf(`#!/sbin/openrc-run

command="%s"
command_args="%s"
command_background=true
pidfile="/run/${RC_SVCNAME}.pid"
directory="%s"
# Auto-restart on exit
: ${respawn_delay:=5}
`, startCmd, project.LocalPort, workDir)

				// Write the OpenRC service file
				writeServiceCmd := fmt.Sprintf("cat > /etc/init.d/%s << 'EOF'\n%s\nEOF", serviceName, serviceFile)
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, writeServiceCmd)

				// Make it executable
				chmodCmd := fmt.Sprintf("chmod +x /etc/init.d/%s", serviceName)
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, chmodCmd)

				// Enable the service
				enableCmd := fmt.Sprintf("rc-update add %s default", serviceName)
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, enableCmd)

				// Start the service
				logToDB("stdout", "Starting backend service with OpenRC...")
				startCmd := fmt.Sprintf("rc-service %s start", serviceName)
				startResult, startErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, startCmd)
				if startResult != nil {
					for _, line := range startResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				if startErr != nil || (startResult != nil && startResult.ExitCode != 0) {
					logToDB("stderr", fmt.Sprintf("Failed to start service: %s", startErr))
					d.failDeploy(deploy, "Failed to start service")
					return
				}
				logToDB("stdout", "Service started with OpenRC and configured for auto-restart")
			} else {
				// For frontend, configure nginx (already installed)
				d.broadcastPhase(deployID, "service", "Configuring nginx...")
				logToDB("stdout", "Installing nginx in container...")

				// Ensure nginx is installed
				nginxInstallCmd := "apk add --no-cache nginx"
				nginxInstResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, nginxInstallCmd)
				if nginxInstResult != nil {
					for _, line := range nginxInstResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}

				logToDB("stdout", "Configuring nginx in container...")

				// Determine output directory
				outputDir := "dist"
				if project.OutputDir != "" {
					outputDir = project.OutputDir
				}

				// Verify build output exists
				checkBuildCmd := fmt.Sprintf("ls -la %s/%s 2>&1", workDir, outputDir)
				checkResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, checkBuildCmd)
				if checkResult != nil {
					for _, line := range checkResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}

				// Setup OpenRC for nginx auto-start
				logToDB("stdout", "Configuring nginx auto-start with OpenRC...")
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "rc-update add nginx default")
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "rc-service nginx stop") // Stop first

				// Create nginx config
				nginxSetupCmd := fmt.Sprintf(`
					mkdir -p /run/nginx && \
					echo 'server { listen 80; root %s/%s; index index.html; location / { try_files $uri $uri/ /index.html; } }' > /etc/nginx/http.d/default.conf && \
					nginx -t
				`, workDir, outputDir)

				nginxResult, nginxErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, nginxSetupCmd)
				if nginxResult != nil {
					for _, line := range nginxResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				if nginxErr != nil || (nginxResult != nil && nginxResult.ExitCode != 0) {
					logToDB("stderr", fmt.Sprintf("Failed to setup nginx: %s", nginxErr))
					d.failDeploy(deploy, "Failed to setup nginx")
					return
				}

				// Start nginx with OpenRC
				logToDB("stdout", "Starting nginx with OpenRC...")
				_, _ = d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "rc-service nginx start")
				logToDB("stdout", "Nginx configured and started with auto-restart enabled")
			}

			// Save container info to database
			container := &state.Container{
				ProjectID:    project.ID,
				Name:         containerInfo.Name,
				Image:        "images:alpine/3.20",
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

	// Cleanup containers on failure to prevent dead weight accumulation
	if d.lxd != nil && deploy.ProjectID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Get containers for this project
		containers, _ := d.db.ListContainersByProject(deploy.ProjectID)
		for _, container := range containers {
			d.logger.Info("cleaning up container on deploy failure",
				zap.String("projectId", deploy.ProjectID),
				zap.String("containerName", container.Name),
			)
			// Stop and delete the container
			d.lxd.StopContainer(ctx, container.ContainerID)
			d.lxd.DeleteContainer(ctx, container.ContainerID)
			d.db.DeleteContainer(container.ID)
		}
	}

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
