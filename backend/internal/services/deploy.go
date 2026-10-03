package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// networkWait waits for container network/DNS to be ready before installing packages.
const networkWait = `
# Configure DNS resolvers directly
cat > /etc/resolv.conf << 'EOF'
nameserver 8.8.8.8
nameserver 1.1.1.1
nameserver 208.67.222.222
options timeout:1 attempts:2
EOF

# Wait for network connectivity
for i in $(seq 1 15); do
  ping -c1 -W1 dl-cdn.alpinelinux.org >/dev/null 2>&1 && break
  sleep 1
done
for i in $(seq 1 15); do
  ping -c1 -W1 registry.npmjs.org >/dev/null 2>&1 && break
  sleep 1
done
`

// frontendSetupScript sets up a container for frontend projects (React, Vue, Angular, etc).
// Includes nginx and pm2 for process management.
const frontendSetupScript = `set -e` + networkWait + `
echo "SETUP: network ready"
# Install base packages including Node.js
apk update --no-cache
echo "SETUP: apk update done"
apk add --no-cache git bash ca-certificates nginx iproute2 nodejs npm

# Create nginx directories
mkdir -p /etc/nginx/http.d /run/nginx

echo "SETUP: Node.js $(node --version) and npm $(npm --version) installed"

# Configure npm
npm config set registry https://registry.npmjs.org/
npm config set fetch-retries 5
npm config set fetch-retry-mintimeout 20000
npm config set fetch-retry-maxtimeout 120000

# Install PM2 globally for process management
npm install -g pm2
pm2 startup openrc
pm2 save
echo "SETUP: PM2 installed"
node --version && npm --version && echo "SETUP: complete"
`

// nodejsSetupScript sets up a container for Node.js projects (backend and frontend).
const nodejsSetupScript = `set -e` + networkWait + `
echo "SETUP: network ready"
# Install base packages including Node.js
apk update --no-cache
echo "SETUP: apk update done"
apk add --no-cache git bash ca-certificates iproute2 nodejs npm

echo "SETUP: Node.js $(node --version) and npm $(npm --version) installed"

# Configure npm
npm config set registry https://registry.npmjs.org/
npm config set fetch-retries 5
npm config set fetch-retry-mintimeout 20000
npm config set fetch-retry-maxtimeout 120000

# Install PM2 globally for process management
npm install -g pm2
pm2 startup openrc
pm2 save
echo "SETUP: PM2 installed"
node --version && npm --version && echo "SETUP: complete"
`

// pythonSetupScript sets up a container for Python projects (Flask, Django, FastAPI).
const pythonSetupScript = `set -e` + networkWait + `
# Install Python packages
apk update --no-cache && apk add --no-cache python3 py3-pip git bash ca-certificates nodejs npm
npm install -g pm2
pm2 startup openrc
pm2 save
python3 --version
`

// goSetupScript sets up a container for Go projects.
const goSetupScript = `set -e` + networkWait + `
# Install Go packages
apk update --no-cache && apk add --no-cache go git bash ca-certificates nodejs npm
npm install -g pm2
pm2 startup openrc
pm2 save
go version
`

// staticSetupScript sets up a container for static HTML/CSS/JS sites.
const staticSetupScript = `set -e` + networkWait + `
# Install static packages
apk update --no-cache && apk add --no-cache git bash ca-certificates nginx nodejs npm
mkdir -p /etc/nginx/http.d /run/nginx
npm install -g pm2
pm2 startup openrc
pm2 save
`

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
	HostPort          int    // User-specified host port for the container proxy
}

func NewDeployService(runner *exec.Runner, db *state.DB, cfg config.DeployConfig, logger *zap.Logger) *DeployService {
	container := NewContainerService(runner, db, cfg, logger)
	portAllocator := NewPortAllocator(db, runner, cfg.PortPoolStart, cfg.PortPoolEnd)
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

// NormalizeBuildCommand accepts either a bare npm script name or a full
// command line and returns the full command.
//
//	"build"          -> "npm run build"
//	"npm run build"  -> "npm run build"
//	"yarn build"     -> "yarn build"
//
// The UI placeholder suggests the full form, so users naturally type it;
// previously that produced "npm run npm run build" and failed.
func NormalizeBuildCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "npm run build"
	}
	// Already a full command (contains a space or a known runner) — use as-is.
	if strings.ContainsAny(cmd, " \t") {
		return cmd
	}
	// Bare script name — expand via npm.
	return "npm run " + cmd
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
		buildStart := time.Now()

		// Create a new context for the deployment (not tied to the HTTP request)
		deployCtx := context.Background()

		logToDB("stdout", "Starting deployment...")
		logToDB("stdout", fmt.Sprintf("Repository: %s", project.RepoURL))
		logToDB("stdout", fmt.Sprintf("Branch: %s", project.Branch))

		// RepoURL and Branch are interpolated into `sh -c` clone commands below,
		// so reject anything that is not a plain git URL / ref before we get there.
		if !isValidRepoURL(project.RepoURL) {
			d.failDeploy(deploy, "invalid repository URL")
			return
		}
		if !isValidGitRef(project.Branch) {
			d.failDeploy(deploy, "invalid branch name")
			return
		}

		workingDir := project.WorkingDirectory
		if workingDir == "" || workingDir == "." {
			workingDir = "."
		}
		logToDB("stdout", fmt.Sprintf("Working directory: %s", workingDir))

		framework := FrameworkUnknown
		deploy.Framework = string(framework)
		deploy.IsBackend = false

		projectType := ProjectType(project.ProjectType)
		if projectType == "" {
			projectType = ProjectNode
		}

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
			logToDB("stdout", "Creating LXD container for frontend (nodejs + nginx)...")

			frontendContainerInfo, frontendErr := d.lxd.CreateContainerWithUserDataAndFramework(deployCtx, project.ID+"-frontend", project.Name+"-frontend", "images:alpine/3.23", frontendSetupScript, FrameworkReact)
			if frontendErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create frontend container: %s", frontendErr.Error()))
				d.failDeploy(deploy, frontendErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Frontend container created: %s (ID: %s)", frontendContainerInfo.Name, frontendContainerInfo.ID))

			// Clone repository in frontend container (deps already installed)
			logToDB("stdout", "Cloning repository in frontend container...")
			frontendCloneCmd := fmt.Sprintf("mkdir -p /app && cd /app && git clone --branch %s --depth 1 %s repo", project.Branch, project.RepoURL)
			if _, err := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, frontendCloneCmd); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to clone repository in frontend container: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", "Repository cloned in frontend container")

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
			// Fix npm cache before install
			fixNpmCmd := "rm -rf /root/.npm /tmp/npm-* /root/.npm-* 2>/dev/null || true && mkdir -p /root/.npm"
			d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, fixNpmCmd)
			frontendBuildCmd := "npm install --legacy-peer-deps --prefer-offline --no-audit && npm run build"
			frontendBuildResult, frontendBuildErr := d.lxd.RunCommandInContainerWithOptions(deployCtx, frontendContainerInfo.ID, frontendBuildCmd, ExecOptions{
				WorkDir: frontendWorkDir,
				Timeout: 30 * time.Minute,
			})
			if frontendBuildResult != nil {
				for _, line := range frontendBuildResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			if frontendBuildErr != nil || (frontendBuildResult != nil && frontendBuildResult.ExitCode != 0) {
				logToDB("stderr", "Frontend build failed")
				d.failDeploy(deploy, "Frontend build failed")
				return
			}

			// Configure PM2 to serve frontend static files
			logToDB("stdout", "Configuring PM2 to serve frontend static files...")
			frontendOutputPath := fmt.Sprintf("/app/repo/%s/dist", frontendDir)
			if project.OutputDir != "" {
				frontendOutputPath = fmt.Sprintf("/app/repo/%s/%s", frontendDir, project.OutputDir)
			}

			pm2ServeCmd := fmt.Sprintf(
				"pm2 serve %s 80 --name %s --spa && pm2 save",
				frontendOutputPath, project.Name+"-frontend",
			)

			pm2Result, pm2Err := d.lxd.RunCommandInContainer(deployCtx, frontendContainerInfo.ID, pm2ServeCmd)
			if pm2Result != nil {
				for _, line := range pm2Result.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			if pm2Err != nil || (pm2Result != nil && pm2Result.ExitCode != 0) {
				logToDB("stderr", "Failed to setup PM2 serve for frontend")
				d.failDeploy(deploy, "Failed to setup frontend service")
				return
			}
			logToDB("stdout", "Frontend container ready (PM2 serving build output)")

			// Configure frontend container to auto-start on boot
			d.runner.Run(deployCtx, exec.RunOpts{
				JobType: "lxd_autostart",
				Command: "lxc",
				Args:    []string{"config", "set", frontendContainerInfo.ID, "boot.autostart", "true"},
				Timeout: 10 * time.Second,
			})

			// ==================== BACKEND CONTAINER ====================
			d.broadcastPhase(deployID, "build", "Creating backend container (with Node.js)...")
			logToDB("stdout", "Creating LXD container for backend...")

			backendContainerInfo, backendErr := d.lxd.CreateContainerWithUserDataAndFramework(deployCtx, project.ID+"-backend", project.Name+"-backend", "images:alpine/3.23", nodejsSetupScript, backendFramework)
			if backendErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create backend container: %s", backendErr.Error()))
				d.failDeploy(deploy, backendErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Backend container created: %s (ID: %s)", backendContainerInfo.Name, backendContainerInfo.ID))

			// Allocate backend port (use user-specified if provided)
			var backendHostPort int
			if opts != nil && opts.HostPort > 0 {
				backendHostPort = opts.HostPort
				logToDB("stdout", fmt.Sprintf("Using user-specified backend host port: %d", backendHostPort))
			} else {
				var backendPortErr error
				backendHostPort, backendPortErr = d.portAllocator.AllocatePort("backend")
				if backendPortErr != nil {
					logToDB("stderr", fmt.Sprintf("Failed to allocate backend port: %s", backendPortErr.Error()))
					d.failDeploy(deploy, backendPortErr.Error())
					return
				}
				logToDB("stdout", fmt.Sprintf("Allocated backend host port: %d", backendHostPort))
			}

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
			// Fix npm cache before install
			fixBackendNpmCmd := "rm -rf /root/.npm /tmp/npm-* /root/.npm-* 2>/dev/null || true && mkdir -p /root/.npm"
			d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, fixBackendNpmCmd)
			if _, err := d.lxd.RunCommandInContainerWithOptions(deployCtx, backendContainerInfo.ID, backendInstallCmd, ExecOptions{
				WorkDir: backendWorkDir,
				Timeout: 30 * time.Minute,
			}); err != nil {
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

			// Start backend service via PM2 using the user-provided start script
			logToDB("stdout", "Configuring backend service with PM2...")
			pm2StartCmd := fmt.Sprintf(
				"cd %s && pm2 start npm --name %s -- %s && pm2 save",
				backendWorkDir, project.Name+"-backend", GetNPMScriptName(startCmd),
			)
			svcResult, svcErr := d.lxd.RunCommandInContainer(deployCtx, backendContainerInfo.ID, pm2StartCmd)
			if svcResult != nil {
				for _, line := range svcResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			if svcErr != nil || (svcResult != nil && svcResult.ExitCode != 0) {
				logToDB("stderr", "Failed to start backend via PM2")
				d.failDeploy(deploy, "Failed to start backend service")
				return
			}
			logToDB("stdout", "Backend service started (managed by PM2)")

			// Save both containers to database
			d.promoteContainer(frontendContainerInfo, project.ID, fmt.Sprintf(`{"host":"%d","container":"80"}`, frontendHostPort))
			d.promoteContainer(backendContainerInfo, project.ID, fmt.Sprintf(`{"host":"%d","container":"%d"}`, backendHostPort, backendContainerPort))

			if opts != nil && opts.EnableNginx && opts.Domain != "" {
				logToDB("stdout", "Configuring host nginx for domain routing (listen 80)...")
				domain := opts.Domain
				siteCfg := NginxSiteConfig{
					Domain:               domain,
					ListenPort:           80,
					FrontendProxyEnabled: true,
					FrontendProxyPort:    frontendHostPort,
					ProxyEnabled:         true,
					ProxyPort:            backendHostPort,
				}
				combinedContent := d.nginx.GenerateConfig(siteCfg)
				if err := d.nginx.WriteConfig(domain, combinedContent); err != nil {
					logToDB("stderr", fmt.Sprintf("Nginx config failed: %s", err.Error()))
				} else {
					_ = d.nginx.DeleteConfigFile(fmt.Sprintf("frontend-%s", domain))
					_ = d.nginx.DeleteConfigFile(fmt.Sprintf("backend-%s", domain))
				}
				if testResult, err := d.nginx.TestConfig(deployCtx); err == nil && testResult.Success {
					d.nginx.Reload(deployCtx)
					logToDB("stdout", fmt.Sprintf("Nginx configured: %s listen 80 -> frontend %d, backend %d", domain, frontendHostPort, backendHostPort))
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

			// installCmd and startCmd will be set after framework detection below
			var startCmd string

			// Pick the right setup script based on project type and framework
			setupScript := nodejsSetupScript
			setupLabel := "nodejs"

			// Frontend frameworks need nginx
			isFrontendFramework := framework == FrameworkReact || framework == FrameworkVue ||
				framework == FrameworkAngular || framework == FrameworkSvelte ||
				framework == FrameworkWebpack || framework == FrameworkVite ||
				framework == FrameworkStatic

			switch projectType {
			case ProjectNode:
				if isFrontendFramework {
					setupScript = frontendSetupScript
					setupLabel = "frontend"
				} else {
					setupScript = nodejsSetupScript
					setupLabel = "nodejs"
				}
			case ProjectPython:
				setupScript = pythonSetupScript
				setupLabel = "python"
			case ProjectGo:
				setupScript = goSetupScript
				setupLabel = "go"
			case ProjectStatic:
				setupScript = staticSetupScript
				setupLabel = "static"
			}

			// Create LXD container with type-specific setup
			d.broadcastPhase(deployID, "build", fmt.Sprintf("Creating LXD container (%s)...", setupLabel))
			containerInfo, containerErr := d.lxd.CreateContainerWithUserDataAndFramework(deployCtx, project.ID, project.Name, "images:alpine/3.23", setupScript, framework)
			if containerErr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to create LXD container: %s", containerErr.Error()))
				d.failDeploy(deploy, containerErr.Error())
				return
			}

			logToDB("stdout", fmt.Sprintf("Container created: %s (ID: %s, IP: %s)", containerInfo.Name, containerInfo.ID, containerInfo.IP))

			// STEP 1: Clone repository directly inside the container
			d.broadcastPhase(deployID, "build", "Cloning repository in container...")
			logToDB("stdout", "Cloning repository inside container...")

			cloneCmd := fmt.Sprintf("mkdir -p /app && cd /app && git clone --branch %s --depth 1 %s repo", project.Branch, project.RepoURL)
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

			// STEP 3: Determine working directory inside container
			// Root is at /app/repo, user-specified working dir is appended
			workDir := "/app/repo"
			if workingDir != "" && workingDir != "." {
				workDir = fmt.Sprintf("/app/repo/%s", workingDir)
			}
			logToDB("stdout", fmt.Sprintf("Working directory: %s", workDir))

			// STEP 4: Detect framework by checking for specific files inside container
			d.broadcastPhase(deployID, "detect", "Detecting framework...")
			logToDB("stdout", "Detecting framework from cloned files...")

			// Use the LXD service's framework detection helper
			framework = d.lxd.DetectFrameworkInContainer(deployCtx, containerInfo.ID, workDir)

			if !IsSupportedFrontendFramework(framework) {
				logToDB("stderr", fmt.Sprintf("Unsupported project type '%s'. This build pipeline supports Next.js and React frontends only.", framework))
				d.failDeploy(deploy, fmt.Sprintf("unsupported framework: %s", framework))
				return
			}

			d.logger.Info("detected framework",
				zap.String("projectId", project.ID),
				zap.String("framework", string(framework)),
			)
			logToDB("stdout", fmt.Sprintf("Detected framework: %s", framework))

			d.logger.Info("detected framework",
				zap.String("projectId", project.ID),
				zap.String("framework", string(framework)),
			)
			logToDB("stdout", fmt.Sprintf("Detected framework: %s", framework))

			// Store framework info on the deploy record
			deploy.Framework = string(framework)
			deploy.IsBackend = IsBackendFramework(framework)

			// Write environment variables to .env file BEFORE dependency installation
			// so they're available during npm install / pip install
			if len(envVars) > 0 {
				logToDB("stdout", "Writing environment variables to .env file...")
				// Write via stdin rather than a heredoc: a value containing the
				// delimiter (or a newline) would otherwise break out of it.
				envContent := ""
				for k, v := range envVars {
					envContent += fmt.Sprintf("%s=%s\n", k, v)
				}
				d.lxd.WriteFileInContainer(deployCtx, containerInfo.ID, "/app/repo/.env", envContent)
			}

			startCmd = ""
			if deploy.IsBackend {
				if project.StartCommand != nil && *project.StartCommand != "" {
					startCmd = *project.StartCommand
				} else {
					startCmd = GetDefaultStartCommand(framework, containerPort)
				}
			}

			containerPort = 80
			if deploy.IsBackend {
				containerPort = GetDefaultPort(framework)
				if project.LocalPort > 0 {
					containerPort = project.LocalPort
				}
			}

			// STEP 5: Allocate host port for the container (use user-specified if provided)
			var hostPort int
			if opts != nil && opts.HostPort > 0 {
				hostPort = opts.HostPort
				logToDB("stdout", fmt.Sprintf("Using user-specified host port: %d", hostPort))
			} else {
				var portErr error
				hostPort, portErr = d.portAllocator.AllocatePort(string(projectType))
				if portErr != nil {
					logToDB("stderr", fmt.Sprintf("Failed to allocate host port: %s", portErr.Error()))
					d.failDeploy(deploy, portErr.Error())
					return
				}
				logToDB("stdout", fmt.Sprintf("Allocated host port: %d", hostPort))
			}

			// Setup port proxy from host to container
			d.broadcastPhase(deployID, "service", "Setting up port proxy...")
			logToDB("stdout", "Setting up port proxy...")
			if err := d.lxd.SetupPortProxy(deployCtx, containerInfo.ID, containerPort, hostPort); err != nil {
				logToDB("stderr", fmt.Sprintf("Failed to setup port proxy: %s", err.Error()))
				d.failDeploy(deploy, err.Error())
				return
			}
			logToDB("stdout", fmt.Sprintf("Port proxy configured: %d → %d", hostPort, containerInfo.ContainerPort))

			// STEP 6: Install project dependencies
			isNodeFramework := framework == FrameworkNode || framework == FrameworkNextJS ||
				framework == FrameworkNuxtJS || framework == FrameworkRemix || framework == FrameworkNestJS ||
				framework == FrameworkExpress || framework == FrameworkFastify || framework == FrameworkReact ||
				framework == FrameworkVue || framework == FrameworkAngular || framework == FrameworkSvelte ||
				framework == FrameworkWebpack || framework == FrameworkVite || framework == FrameworkUnknown

			if isNodeFramework {
				d.broadcastPhase(deployID, "build", "Installing project dependencies...")
				logToDB("stdout", "Installing project dependencies in container...")

				// Verify working directory exists
				lsResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, fmt.Sprintf("ls -la %s", workDir))
				if lsResult != nil {
					logToDB("stdout", "Working directory contents:")
					for _, line := range lsResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}

				// Check if package.json exists
				pkgCheckResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, fmt.Sprintf("test -f %s/package.json && echo 'exists' || echo 'not_found'", workDir))
				if pkgCheckResult != nil {
					for _, line := range pkgCheckResult.Lines {
						logToDB("debug", fmt.Sprintf("package.json check: %s", line.Text))
					}
				}

				// CRITICAL: Check if npm is available, install if missing
				npmCheckResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "which npm 2>/dev/null || echo 'not_found'")
				if npmCheckResult != nil && len(npmCheckResult.Lines) > 0 {
					if strings.Contains(npmCheckResult.Lines[0].Text, "not_found") {
						logToDB("stdout", "npm not found in container, installing Node.js and npm...")
						installNodejsCmd := "apk add --no-cache nodejs npm"
						installResult, installErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, installNodejsCmd)
						if installResult != nil {
							for _, line := range installResult.Lines {
								logToDB(line.Stream, line.Text)
							}
						}
						if installErr != nil || (installResult != nil && installResult.ExitCode != 0) {
							logToDB("stderr", "Failed to install Node.js and npm in container")
							d.failDeploy(deploy, "Failed to install Node.js and npm")
							return
						}
						logToDB("stdout", "Node.js and npm installed successfully")
					}
				}

				// Fix npm cache permissions before install
				logToDB("stdout", "Fixing npm cache permissions...")
				fixNpmCmd := "rm -rf /root/.npm /tmp/npm-* /root/.npm-* 2>/dev/null || true && mkdir -p /root/.npm"
				d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, fixNpmCmd)

				// Pre-warm npm cache with package.json dependencies
				logToDB("stdout", "Pre-warming npm cache...")
				d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "npm cache verify")

				installCmd := "npm install --legacy-peer-deps --prefer-offline --no-audit --progress=false"
				if project.InstallCommand != nil && *project.InstallCommand != "" {
					installCmd = *project.InstallCommand
				}
				logToDB("stdout", fmt.Sprintf("Running: cd %s && %s", workDir, installCmd))
				installResult, installErr := d.lxd.RunCommandInContainerWithOptions(deployCtx, containerInfo.ID, installCmd, ExecOptions{
					WorkDir: workDir,
					Timeout: 30 * time.Minute,
				})

				// Log all npm output
				if installResult != nil {
					for _, line := range installResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}

				// Check for npm errors
				if installErr != nil || (installResult != nil && installResult.ExitCode != 0) {
					exitCode := -1
					if installResult != nil {
						exitCode = installResult.ExitCode
					}
					logToDB("stderr", fmt.Sprintf("npm install failed (exit code: %d)", exitCode))

					// Show npm debug log
					logToDB("stdout", "Fetching npm debug log...")
					lsLogsResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, "ls -la /root/.npm/_logs/ 2>&1 || echo 'no logs'")
					if lsLogsResult != nil {
						for _, line := range lsLogsResult.Lines {
							logToDB("debug", line.Text)
						}
					}

					// Try to get the latest npm log
					debugLogCmd := "cat /root/.npm/_logs/*.log 2>&1 | tail -50"
					debugResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, debugLogCmd)
					if debugResult != nil {
						logToDB("stderr", "--- NPM Debug Log ---")
						for _, line := range debugResult.Lines {
							logToDB("stderr", line.Text)
						}
					}
					d.failDeploy(deploy, "Failed to install project dependencies")
					return
				}
				logToDB("stdout", "Project dependencies installed successfully")
			} else if framework == FrameworkFlask || framework == FrameworkDjango || framework == FrameworkFastAPI {
				d.broadcastPhase(deployID, "build", "Installing project dependencies...")
				logToDB("stdout", "Installing Python dependencies...")
				pipInstallCmd := fmt.Sprintf("cd %s && pip install -r requirements.txt 2>/dev/null || true", workDir)
				pipResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, pipInstallCmd)
				if pipResult != nil {
					for _, line := range pipResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				logToDB("stdout", "Python dependencies installed")
			} else if framework == FrameworkGo {
				d.broadcastPhase(deployID, "build", "Installing project dependencies...")
				logToDB("stdout", "Downloading Go modules...")
				goModCmd := fmt.Sprintf("cd %s && go mod download", workDir)
				goModResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, goModCmd)
				if goModResult != nil {
					for _, line := range goModResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				logToDB("stdout", "Go modules downloaded")
			} else {
				logToDB("stdout", "No dependency installation needed for this framework")
			}

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

				// Accept both "build" and "npm run build" from the user
				if buildCmd != "" {
					buildCmd = NormalizeBuildCommand(buildCmd)
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
						exitCode := -1
						if buildResult != nil {
							exitCode = buildResult.ExitCode
						}
						logToDB("stderr", fmt.Sprintf("Failed to build project (exit code: %d)", exitCode))

						// Show what's in the directory for debugging
						lsResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, fmt.Sprintf("ls -la %s", workDir))
						if lsResult != nil {
							logToDB("debug", "[Directory listing]")
							for _, line := range lsResult.Lines {
								logToDB("debug", line.Text)
							}
						}

						d.failDeploy(deploy, "Failed to build project")
						return
					}
					logToDB("stdout", "Project built successfully")

					// Show what was built
					findResult, _ := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, fmt.Sprintf("find %s -name 'dist' -o -name 'build' -o -name 'out' 2>/dev/null | head -5", workDir))
					if findResult != nil {
						logToDB("debug", "[Build output directories]")
						for _, line := range findResult.Lines {
							logToDB("debug", line.Text)
						}
					}
				} else {
					logToDB("stdout", "No build command for this framework type")
				}
			}

			// Configure container to auto-start on boot
			d.broadcastPhase(deployID, "service", "Configuring container auto-start...")
			logToDB("stdout", "Configuring container to auto-start on system boot...")
			autostarResult, _ := d.runner.Run(deployCtx, exec.RunOpts{
				JobType: "lxd_autostart",
				Command: "lxc",
				Args:    []string{"config", "set", containerInfo.ID, "boot.autostart", "true"},
				Timeout: 10 * time.Second,
			})
			if autostarResult != nil {
				for _, line := range autostarResult.Lines {
					logToDB(line.Stream, line.Text)
				}
			}
			logToDB("stdout", "Container configured to auto-start on boot")

			// STEP 8: Start service or configure PM2
			if deploy.IsBackend {
				d.broadcastPhase(deployID, "service", "Starting service...")
				logToDB("stdout", "Configuring backend service with PM2...")

				// Ensure startCmd has a fallback - if empty, use npm start
				if startCmd == "" {
					startCmd = "start"
				}

				// Start the backend service via PM2
				// This ensures the service persists across container restarts via pm2 save/resurrect
				pm2StartCmd := fmt.Sprintf(
					"cd %s && pm2 start npm --name %s -- %s && pm2 save",
					workDir, project.Name, GetNPMScriptName(startCmd),
				)
				svcResult, svcErr := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, pm2StartCmd)
				if svcResult != nil {
					for _, line := range svcResult.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				if svcErr != nil || (svcResult != nil && svcResult.ExitCode != 0) {
					logToDB("stderr", "Failed to start backend via PM2")
					d.failDeploy(deploy, "Failed to start service")
					return
				}
				logToDB("stdout", "Backend service started (managed by PM2)")
			} else {
				// For frontend, serve static files via PM2
				d.broadcastPhase(deployID, "service", "Configuring PM2 serve...")
				logToDB("stdout", "Configuring PM2 to serve frontend static files...")

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

				// Serve the static files via PM2 on port 80
				servePath := fmt.Sprintf("%s/%s", workDir, outputDir)
				pm2ServeCmd := fmt.Sprintf(

					"pm2 serve %s 80 --name %s --spa && pm2 save",
					servePath, project.Name,
				)

				pm2Result, pm2Err := d.lxd.RunCommandInContainer(deployCtx, containerInfo.ID, pm2ServeCmd)
				if pm2Result != nil {
					for _, line := range pm2Result.Lines {
						logToDB(line.Stream, line.Text)
					}
				}
				if pm2Err != nil || (pm2Result != nil && pm2Result.ExitCode != 0) {
					logToDB("stderr", "Failed to setup PM2 serve for frontend")
					d.failDeploy(deploy, "Failed to setup frontend service")
					return
				}
				logToDB("stdout", "Frontend static files being served by PM2")
			}

			// Save container info to database
			if perr := d.promoteContainer(containerInfo, project.ID, fmt.Sprintf(`{"host":"%d","container":"%d"}`, hostPort, containerPort)); perr != nil {
				logToDB("stderr", fmt.Sprintf("Failed to save container: %s", perr.Error()))
			}

			logToDB("stdout", fmt.Sprintf("LXD deployment completed! Container: %s, Host Port: %d", containerInfo.Name, hostPort))

			frontendProxyPort := 0
			backendProxyPort := hostPort
			if !isBackend {
				frontendProxyPort = hostPort
			}
			if opts != nil && opts.EnableNginx && (opts.Domain != "" || opts.AttachToProjectID != "") {
				logToDB("stdout", "Configuring nginx (listen 80)...")
				domainToUse := opts.Domain
				attachedFrontendPort := 0
				if opts.AttachToProjectID != "" && isBackend {
					frontendProj, err := d.db.GetProject(opts.AttachToProjectID)
					if err == nil && frontendProj != nil && frontendProj.Domain != "" {
						domainToUse = frontendProj.Domain
						logToDB("stdout", fmt.Sprintf("Creating backend config for domain: %s", domainToUse))
						if frontendContainers, err := d.db.ListContainersByProject(opts.AttachToProjectID); err == nil {
							for _, fc := range frontendContainers {
								if hp, _, perr := parsePortMapping(fc.PortMappings); perr == nil && hp > 0 {
									attachedFrontendPort = hp
									break
								}
							}
						}
						if attachedFrontendPort > 0 {
							frontendProxyPort = attachedFrontendPort
							logToDB("stdout", fmt.Sprintf("Found frontend host port: %d", attachedFrontendPort))
						} else {
							logToDB("stderr", "Warning: Could not find frontend container port; backend routing only")
						}
					} else {
						logToDB("stderr", "Warning: Could not find domain for attached frontend project")
					}
				}
				if domainToUse != "" {
					nginxPort, err := d.applyNginxForDeploy(deployCtx, project, domainToUse, "", isBackend, frontendProxyPort, backendProxyPort)
					if err != nil {
						logToDB("stderr", fmt.Sprintf("Nginx configuration failed: %s", err.Error()))
						d.logger.Error("nginx apply failed", zap.String("deployId", deployID), zap.String("domain", domainToUse), zap.Error(err))
					} else {
						configType := "combined"
						if isBackend {
							configType = "backend"
						} else if !isBackend && frontendProxyPort > 0 {
							configType = "frontend"
						}
						logToDB("stdout", fmt.Sprintf("Nginx %s config configured for %s (listen %d -> frontend %d, backend %d)", configType, domainToUse, nginxPort, frontendProxyPort, backendProxyPort))
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
		}
	}()

	return deployID, nil
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

	// Old source is removed with the container above; nothing is kept on the host.

	d.logger.Info("rebuild initiated",
		zap.String("projectId", project.ID),
		zap.String("repoUrl", project.RepoURL),
	)

	// Trigger fresh deployment
	return d.Deploy(ctx, project)
}

func (d *DeployService) promoteContainer(info *ContainerInfo, projectID, portMappings string) error {
	if info == nil {
		return fmt.Errorf("nil container info")
	}
	if info.DBID != "" {
		if rec, _ := d.db.GetContainer(info.DBID); rec != nil {
			rec.ProjectID = projectID
			rec.Name = info.Name
			rec.ContainerID = info.ID
			rec.Status = "running"
			rec.PortMappings = portMappings
			if err := d.db.UpdateContainer(rec); err != nil {
				return err
			}
			d.removeStaleCreatingContainers(projectID, rec.ID, info.ID)
			return nil
		}
	}
	if rec, _ := d.db.GetContainerByName(info.Name); rec != nil {
		rec.ProjectID = projectID
		rec.ContainerID = info.ID
		rec.Status = "running"
		rec.PortMappings = portMappings
		if err := d.db.UpdateContainer(rec); err != nil {
			return err
		}
		d.removeStaleCreatingContainers(projectID, rec.ID, info.ID)
		return nil
	}
	for _, pid := range []string{projectID, projectID + "-frontend", projectID + "-backend"} {
		if containers, _ := d.db.ListContainersByProject(pid); len(containers) > 0 {
			for _, c := range containers {
				if c.ContainerID == info.ID {
					c.ProjectID = projectID
					c.Name = info.Name
					c.Status = "running"
					c.PortMappings = portMappings
					if err := d.db.UpdateContainer(&c); err != nil {
						return err
					}
					d.removeStaleCreatingContainers(projectID, c.ID, info.ID)
					return nil
				}
			}
		}
	}
	rec := &state.Container{
		ProjectID:    projectID,
		Name:         info.Name,
		Image:        "images:alpine/3.23",
		ContainerID:  info.ID,
		Status:       "running",
		PortMappings: portMappings,
	}
	if err := d.db.CreateContainer(rec); err != nil {
		return err
	}
	d.removeStaleCreatingContainers(projectID, rec.ID, info.ID)
	return nil
}

func (d *DeployService) removeStaleCreatingContainers(projectID, keepID, lxdName string) {
	for _, pid := range []string{projectID, projectID + "-frontend", projectID + "-backend"} {
		containers, _ := d.db.ListContainersByProject(pid)
		for _, c := range containers {
			if c.ID == keepID {
				continue
			}
			if c.Status == "creating" && (c.ContainerID == lxdName || c.ProjectID == pid) {
				if c.ContainerID == lxdName || c.Name == "" || len(containers) > 1 {
					d.db.DeleteContainer(c.ID)
				}
			}
		}
	}
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

		// Get containers for this project, including full-stack variants
		allProjectIDs := []string{deploy.ProjectID, deploy.ProjectID + "-frontend", deploy.ProjectID + "-backend"}
		for _, pid := range allProjectIDs {
			containers, _ := d.db.ListContainersByProject(pid)
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

// repoURLRegex allows only https://, git@ SSH remotes, and git:// URLs built
// from a restricted character set. Shell metacharacters (`;`, `|`, `&`, `$`,
// backticks, whitespace) are excluded because the URL is interpolated into an
// `sh -c` git clone command inside the container.
var repoURLRegex = regexp.MustCompile(`^(https://[a-zA-Z0-9._\-]+(:[0-9]+)?/[a-zA-Z0-9._\-/]+(\.git)?|git@[a-zA-Z0-9._\-]+:[a-zA-Z0-9._\-/]+(\.git)?|git://[a-zA-Z0-9._\-]+/[a-zA-Z0-9._\-/]+(\.git)?)$`)

func isValidRepoURL(url string) bool {
	return repoURLRegex.MatchString(url)
}

// gitRefRegex matches a branch/tag name without shell metacharacters. Refs are
// passed to `git clone --branch <ref>`, so they must be inert.
var gitRefRegex = regexp.MustCompile(`^[a-zA-Z0-9._\-/]{1,255}$`)

func isValidGitRef(ref string) bool {
	if ref == "" {
		return false
	}
	// Reject refs that git itself forbids or that would change option parsing.
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") {
		return false
	}
	return gitRefRegex.MatchString(ref)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
