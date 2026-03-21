package services

import (
	"context"
	"encoding/json"
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

// FrameworkType represents the type of framework being deployed
type FrameworkType string

const (
	FrameworkUnknown FrameworkType = "unknown"
	FrameworkNode    FrameworkType = "node"
	FrameworkNextJS  FrameworkType = "nextjs"
	FrameworkNuxtJS  FrameworkType = "nuxtjs"
	FrameworkRemix   FrameworkType = "remix"
	FrameworkNestJS  FrameworkType = "nestjs"
	FrameworkExpress FrameworkType = "express"
	FrameworkFastify FrameworkType = "fastify"
	FrameworkReact   FrameworkType = "react"
	FrameworkVue     FrameworkType = "vue"
	FrameworkAngular FrameworkType = "angular"
	FrameworkSvelte  FrameworkType = "svelte"
	FrameworkWebpack FrameworkType = "webpack"
	FrameworkVite    FrameworkType = "vite"
	FrameworkFlask   FrameworkType = "flask"
	FrameworkDjango  FrameworkType = "django"
	FrameworkFastAPI FrameworkType = "fastapi"
	FrameworkGo      FrameworkType = "go"
	FrameworkStatic  FrameworkType = "static"
)

// IsBackendFramework returns true if the framework is a backend framework that needs to be served
func IsBackendFramework(framework FrameworkType) bool {
	switch framework {
	case FrameworkNestJS, FrameworkExpress, FrameworkFastify, FrameworkFlask, FrameworkDjango, FrameworkFastAPI, FrameworkGo:
		return true
	case FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix, FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkWebpack, FrameworkVite:
		return true // These can be backend frameworks too
	default:
		return false
	}
}

// GetDefaultPort returns the default port for the given framework
func GetDefaultPort(framework FrameworkType) int {
	switch framework {
	case FrameworkNextJS:
		return 3000
	case FrameworkNuxtJS:
		return 3000
	case FrameworkNestJS:
		return 3000
	case FrameworkExpress:
		return 3000
	case FrameworkFastify:
		return 3000
	case FrameworkFlask:
		return 5000
	case FrameworkDjango:
		return 8000
	case FrameworkFastAPI:
		return 8000
	case FrameworkGo:
		return 8080
	case FrameworkNode:
		return 3000
	default:
		return 80
	}
}

// GetDefaultInstallCommand returns the default install command for the framework
func GetDefaultInstallCommand(framework FrameworkType) string {
	switch framework {
	case FrameworkNestJS, FrameworkExpress, FrameworkFastify, FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix, FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkWebpack, FrameworkVite, FrameworkNode:
		return "npm install --prefer-offline --no-audit --progress=false"
	case FrameworkFlask:
		return "pip install -r requirements.txt"
	case FrameworkDjango:
		return "pip install -r requirements.txt"
	case FrameworkFastAPI:
		return "pip install -r requirements.txt"
	case FrameworkGo:
		return "go mod download"
	default:
		return "npm install"
	}
}

// GetDefaultStartCommand returns the default start command for the framework
func GetDefaultStartCommand(framework FrameworkType, port int) string {
	switch framework {
	case FrameworkNextJS:
		return fmt.Sprintf("npx next start -p %d", port)
	case FrameworkNuxtJS:
		return fmt.Sprintf("npx nuxt start -p %d", port)
	case FrameworkNestJS:
		return fmt.Sprintf("npm run start -- --port %d", port)
	case FrameworkExpress:
		return fmt.Sprintf("PORT=%d node server.js", port)
	case FrameworkFastify:
		return fmt.Sprintf("PORT=%d node server.js", port)
	case FrameworkFlask:
		return fmt.Sprintf("FLASK_APP=app.py flask run --host=0.0.0.0 --port %d", port)
	case FrameworkDjango:
		return fmt.Sprintf("python manage.py runserver 0.0.0.0:%d", port)
	case FrameworkFastAPI:
		return fmt.Sprintf("uvicorn main:app --host 0.0.0.0 --port %d", port)
	case FrameworkGo:
		return fmt.Sprintf("./server -port=%d", port)
	case FrameworkNode:
		return fmt.Sprintf("PORT=%d node server.js", port)
	case FrameworkRemix:
		return fmt.Sprintf("npm run start -- --port %d", port)
	default:
		return "npm start"
	}
}

// GetStartCommand returns the start command for a given framework and directory
func GetStartCommand(framework FrameworkType, dir string) string {
	switch framework {
	case FrameworkNextJS:
		return "npx next start"
	case FrameworkNuxtJS:
		return "npx nuxt start"
	case FrameworkNestJS:
		return "npm run start"
	case FrameworkExpress, FrameworkFastify:
		// Look for common entry points
		entryPoints := []string{"server.js", "app.js", "index.js", "main.js", "src/server.js", "src/app.js", "src/index.js", "dist/server.js", "dist/app.js"}
		for _, ep := range entryPoints {
			if fileExists(filepath.Join(dir, ep)) {
				return fmt.Sprintf("node %s", ep)
			}
		}
		return "node server.js"
	case FrameworkFlask:
		// Look for common Flask entry points
		entryPoints := []string{"app.py", "main.py", "server.py", "__init__.py"}
		for _, ep := range entryPoints {
			if fileExists(filepath.Join(dir, ep)) {
				return fmt.Sprintf("flask --app %s run", strings.TrimSuffix(ep, filepath.Ext(ep)))
			}
		}
		return "flask --app app.py run"
	case FrameworkDjango:
		return "python manage.py runserver 0.0.0.0:8000"
	case FrameworkFastAPI:
		// Look for common FastAPI entry points
		entryPoints := []string{"main.py", "app.py", "server.py", "api.py"}
		for _, ep := range entryPoints {
			if fileExists(filepath.Join(dir, ep)) {
				moduleName := strings.TrimSuffix(ep, filepath.Ext(ep))
				return fmt.Sprintf("uvicorn %s:app --host 0.0.0.0 --port 8000", moduleName)
			}
		}
		return "uvicorn main:app --host 0.0.0.0 --port 8000"
	case FrameworkGo:
		return "./server"
	case FrameworkNode:
		// Look for common Node entry points
		entryPoints := []string{"server.js", "app.js", "index.js", "main.js", "dist/server.js", "dist/app.js", "build/server.js", "build/app.js"}
		for _, ep := range entryPoints {
			if fileExists(filepath.Join(dir, ep)) {
				return fmt.Sprintf("node %s", ep)
			}
		}
		return "node server.js"
	case FrameworkRemix:
		return "npm run start"
	case FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkWebpack, FrameworkVite:
		// These typically shouldn't reach here since they should be static, but in case they have dev servers
		return "npm run serve"
	default:
		return ""
	}
}

// LXDService handles LXD container-based deployments
type LXDService struct {
	runner      *exec.Runner
	cfg         config.DeployConfig
	db          *state.DB
	logger      *zap.Logger
	initialized bool
}

func NewLXDService(runner *exec.Runner, cfg config.DeployConfig, db *state.DB, logger *zap.Logger) *LXDService {
	return &LXDService{
		runner:      runner,
		cfg:         cfg,
		db:          db,
		logger:      logger,
		initialized: false,
	}
}

// EnsureLXDInitialized checks if LXD is initialized and initializes it if needed
func (l *LXDService) EnsureLXDInitialized(ctx context.Context) error {
	if l.initialized {
		return nil
	}

	// Check if LXD is already initialized
	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_check",
		Command: "lxc",
		Args:    []string{"list"},
		Timeout: 10 * time.Second,
	})

	if err == nil && result.Success {
		l.initialized = true
		l.logger.Info("LXD is already initialized")
		return nil
	}

	// LXD not initialized, initialize it automatically
	l.logger.Info("LXD not initialized, initializing automatically...")

	// Initialize LXD with default settings (non-interactive)
	initCmd := `lxd init --minimal`

	initResult, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_init",
		Command: "sh",
		Args:    []string{"-c", initCmd},
		Timeout: 2 * time.Minute,
	})

	if err != nil || !initResult.Success {
		return fmt.Errorf("failed to initialize LXD: %w", err)
	}

	l.initialized = true
	l.logger.Info("LXD initialized successfully")
	return nil
}

// ContainerInfo holds information about an LXD container
type ContainerInfo struct {
	ID            string
	Name          string
	IP            string
	HostPort      int
	ContainerPort int
	Status        string
}

// CreateContainer creates an LXD container for a project
func (l *LXDService) CreateContainer(ctx context.Context, projectID, projectName, image string) (*ContainerInfo, error) {
	// Ensure LXD is initialized
	if err := l.EnsureLXDInitialized(ctx); err != nil {
		return nil, fmt.Errorf("LXD initialization failed: %w", err)
	}

	containerName := fmt.Sprintf("opendeploy-%s-%d", projectName, time.Now().Unix())

	// Check if container already exists
	existingContainer, _ := l.db.GetContainerByName(containerName)
	if existingContainer != nil {
		// Check if container still exists in LXD
		status, err := l.GetContainerStatus(ctx, existingContainer.ContainerID)
		if err == nil && status != "stopped" && status != "unknown" {
			l.logger.Info("stopping existing container before creating new one",
				zap.String("projectId", projectID),
				zap.String("containerName", existingContainer.Name),
			)
			l.StopContainer(ctx, containerName)
		}

		// Remove the old container
		l.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "-f", existingContainer.ContainerID},
			Timeout: 30 * time.Second,
		})

		// Remove from database
		l.db.DeleteContainer(existingContainer.ID)
	}

	// Launch new container
	l.logger.Info("launching lxd container",
		zap.String("projectId", projectID),
		zap.String("containerName", containerName),
		zap.String("image", image),
	)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_launch",
		Command: "lxc",
		Args:    []string{"launch", image, containerName},
		Timeout: 60 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to launch container: %w", err)
	}

	// Container name is the ID for LXD
	containerID := containerName

	// Wait for container to be ready
	time.Sleep(3 * time.Second)

	// Get container IP
	ip, err := l.GetContainerIP(ctx, containerID)
	if err != nil {
		l.logger.Warn("failed to get container IP, container may not have network yet",
			zap.String("containerId", containerID),
			zap.Error(err),
		)
		ip = "" // Continue without IP
	}

	l.logger.Info("container launched successfully",
		zap.String("containerId", containerID),
		zap.String("ip", ip),
	)

	return &ContainerInfo{
		ID:            containerID,
		Name:          containerName,
		IP:            ip,
		HostPort:      0,
		ContainerPort: 0,
		Status:        "running",
	}, nil
}

// GetContainerStatus returns the status of an LXD container
func (l *LXDService) GetContainerStatus(ctx context.Context, containerID string) (string, error) {
	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_info",
		Command: "lxc",
		Args:    []string{"info", containerID, "--format", "json"},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return "unknown", fmt.Errorf("failed to get container info: %w", err)
	}

	// Parse status from JSON output
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			output := strings.TrimSpace(line.Text)
			if strings.Contains(output, "status:") {
				// Extract status from JSON
				if strings.Contains(output, "Running") {
					return "running", nil
				} else if strings.Contains(output, "Stopped") {
					return "stopped", nil
				} else if strings.Contains(output, "Error") {
					return "error", nil
				}
			}
		}
	}

	return "unknown", nil
}

// GetContainerIP returns the IP address of an LXD container
func (l *LXDService) GetContainerIP(ctx context.Context, containerID string) (string, error) {
	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_network",
		Command: "lxc",
		Args:    []string{"list", containerID, "--format", "csv", "-c", "n"},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		return "", fmt.Errorf("failed to get container IP: %w", err)
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			ip := strings.TrimSpace(line.Text)
			if ip != "" && !strings.Contains(ip, "NAME") {
				return ip, nil
			}
		}
	}

	return "", fmt.Errorf("no IP found for container")
}

// InstallDependencies installs base dependencies in the container based on framework
func (l *LXDService) InstallDependencies(
	ctx context.Context,
	containerID string,
	framework FrameworkType,
	forFrameworkDetection bool,
) error {
	l.logger.Info("installing base dependencies in container",
		zap.String("containerId", containerID),
		zap.String("framework", string(framework)),
		zap.Bool("forFrameworkDetection", forFrameworkDetection),
	)

	// Determine required packages based on framework
	// Minimal set - only install what's actually needed
	var packages []string
	packages = append(packages, "git", "bash", "curl", "ca-certificates") // Common dependencies

	// Add framework-specific packages
	switch framework {
	case FrameworkNode, FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix, FrameworkNestJS,
		FrameworkExpress, FrameworkFastify, FrameworkReact, FrameworkVue, FrameworkAngular,
		FrameworkSvelte, FrameworkWebpack, FrameworkVite, FrameworkUnknown:
		// Don't install nodejs from Alpine packages - we'll install latest LTS manually
		// packages = append(packages, "nodejs", "npm")
	case FrameworkFlask, FrameworkDjango, FrameworkFastAPI:
		packages = append(packages, "python3", "py3-pip")
	case FrameworkGo:
		packages = append(packages, "go")
	case FrameworkStatic:
		if forFrameworkDetection {
			// For framework detection, install all so we can detect anything
			// packages = append(packages, "nodejs", "npm", "python3", "py3-pip", "go")
			packages = append(packages, "python3", "py3-pip", "go")
		}
		fallthrough // Static needs nginx, and frontend deployments also need nginx
	default:
		// For unknown frameworks or if nginx is needed
		if forFrameworkDetection {
			// packages = append(packages, "nodejs", "npm", "python3", "py3-pip", "go")
			packages = append(packages, "python3", "py3-pip", "go")
		}
	}

	// Add nginx for frontend/static deployments (not backend servers)
	if !forFrameworkDetection && !IsBackendFramework(framework) {
		packages = append(packages, "nginx")
	} else if forFrameworkDetection && framework == FrameworkUnknown {
		// For detection, add nginx to handle whichever type
		packages = append(packages, "nginx")
	}

	// Combine packages into a single apk add command
	packagesStr := strings.Join(packages, " ")
	cmd := fmt.Sprintf("apk update && apk add --no-cache %s", packagesStr)

	l.logger.Info("running dependency installation",
		zap.String("containerId", containerID),
		zap.String("command", cmd),
	)

	// Run command
	result, err := l.RunCommandInContainer(ctx, containerID, cmd)
	if err != nil {
		l.logger.Error("failed to run dependency installation command",
			zap.String("containerId", containerID),
			zap.Error(err),
		)
		return fmt.Errorf("dependency installation command failed: %w", err)
	}

	// Check if installation succeeded by exit code
	if result.ExitCode != 0 {
		l.logger.Error("dependency installation failed with non-zero exit code",
			zap.String("containerId", containerID),
			zap.Int("exitCode", result.ExitCode),
		)
		for _, line := range result.Lines {
			l.logger.Error("apk output",
				zap.String("stream", line.Stream),
				zap.String("text", line.Text),
			)
		}
		return fmt.Errorf("dependency installation failed with exit code %d", result.ExitCode)
	}

	l.logger.Info("dependencies installed successfully",
		zap.String("containerId", containerID),
		zap.Strings("packages", packages),
	)

	return nil
}

// InstallNginxInContainer installs and configures nginx inside the container for full-stack deployments
func (l *LXDService) InstallNginxInContainer(ctx context.Context, containerID string, backendPort int, frontendPath string) error {
	l.logger.Info("installing nginx in container",
		zap.String("containerId", containerID),
	)

	// Install nginx
	_, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_install_nginx",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "apk", "add", "nginx"},
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("failed to install nginx: %w", err)
	}

	// Create nginx config for full-stack routing
	nginxConfig := fmt.Sprintf(`
server {
    listen 80;
    server_name _;

    # Frontend - serve static files
    location / {
        root %s;
        index index.html;
        try_files $uri $uri/ /index.html;
    }

    # Backend API - proxy to backend service
    location /api/ {
        proxy_pass http://127.0.0.1:%d/;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket support
    location /ws {
        proxy_pass http://127.0.0.1:%d/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
`, frontendPath, backendPort, backendPort)

	// Write nginx config to container
	_, err = l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_write_nginx_config",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "sh", "-c", fmt.Sprintf("echo '%s' > /etc/nginx/http.d/default.conf", nginxConfig)},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}

	// Start nginx
	_, err = l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_start_nginx",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "rc-service", "nginx", "start"},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to start nginx: %w", err)
	}

	// Enable nginx to start on boot
	_, err = l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_enable_nginx",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "rc-update", "add", "nginx", "default"},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		l.logger.Warn("failed to enable nginx on boot", zap.Error(err))
	}

	l.logger.Info("nginx installed and configured in container",
		zap.String("containerId", containerID),
	)

	return nil
}

// InstallInContainer runs installation commands inside an LXD container
func (l *LXDService) InstallInContainer(ctx context.Context, containerID, installCmd string, envVars map[string]string) error {
	l.logger.Info("installing dependencies in container",
		zap.String("containerId", containerID),
	)

	// Set environment variables
	envArgs := []string{}
	for k, v := range envVars {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Run install command
	args := append([]string{"exec", containerID, "--"}, envArgs...)
	args = append(args, "/bin/sh", "-c", installCmd)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_install",
		Command: "lxc",
		Args:    args,
		Timeout: 10 * time.Minute,
	})

	if err != nil || !result.Success {
		return fmt.Errorf("installation failed: %w", err)
	}

	l.logger.Info("installation completed successfully",
		zap.String("containerId", containerID),
	)

	return nil
}

// StartService starts the application in an LXD container
func (l *LXDService) StartService(ctx context.Context, containerID, startCmd string) error {
	l.logger.Info("starting service in container",
		zap.String("containerId", containerID),
	)

	// Run start command in background
	args := []string{"exec", containerID, "--", "/bin/sh", "-c", fmt.Sprintf("%s &", startCmd)}

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_start",
		Command: "lxc",
		Args:    args,
		Timeout: 30 * time.Second,
	})

	if err != nil || !result.Success {
		return fmt.Errorf("failed to start service: %w", err)
	}

	l.logger.Info("service started successfully",
		zap.String("containerId", containerID),
	)

	return nil
}

// StopContainer stops an LXD container
func (l *LXDService) StopContainer(ctx context.Context, containerName string) error {
	l.logger.Info("stopping container",
		zap.String("containerName", containerName),
	)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_stop",
		Command: "lxc",
		Args:    []string{"stop", containerName},
		Timeout: 30 * time.Second,
	})

	if err != nil || !result.Success {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	return nil
}

// DeleteContainer deletes an LXD container
func (l *LXDService) DeleteContainer(ctx context.Context, containerName string) error {
	l.logger.Info("deleting container",
		zap.String("containerName", containerName),
	)

	// First, stop the container if it's running
	l.logger.Info("stopping container before deletion",
		zap.String("containerName", containerName),
	)

	stopResult, _ := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_stop",
		Command: "lxc",
		Args:    []string{"stop", containerName, "--force"},
		Timeout: 30 * time.Second,
	})

	if stopResult != nil && stopResult.Success {
		l.logger.Info("container stopped successfully",
			zap.String("containerName", containerName),
		)
	}

	// Now delete with force flag
	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_delete",
		Command: "lxc",
		Args:    []string{"delete", "--force", containerName},
		Timeout: 30 * time.Second,
	})

	if err != nil || !result.Success {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	return nil
}

// SetupPortProxy sets up a port proxy from host to container
func (l *LXDService) SetupPortProxy(ctx context.Context, containerID string, containerPort, hostPort int) error {
	l.logger.Info("setting up port proxy",
		zap.String("containerId", containerID),
		zap.Int("containerPort", containerPort),
		zap.Int("hostPort", hostPort),
	)

	// Add proxy device to container
	args := []string{
		"config", "device", "add", containerID,
		fmt.Sprintf("proxy-%d", containerPort),
		"proxy",
		fmt.Sprintf("listen=tcp:0.0.0.0:%d", hostPort),
		fmt.Sprintf("connect=tcp:127.0.0.1:%d", containerPort),
	}

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_proxy",
		Command: "lxc",
		Args:    args,
		Timeout: 10 * time.Second,
	})

	if err != nil {
		return fmt.Errorf("failed to setup port proxy: %w", err)
	}

	if !result.Success {
		// Log the error output
		var errMsg string
		for _, line := range result.Lines {
			if line.Stream == "stderr" {
				errMsg += line.Text + "\n"
			}
		}
		return fmt.Errorf("failed to setup port proxy: %s", errMsg)
	}

	return nil
}

// CopyFilesToContainer copies files to an LXD container
func (l *LXDService) CopyFilesToContainer(ctx context.Context, containerID, sourcePath, destPath string) error {
	l.logger.Info("copying files to container",
		zap.String("containerId", containerID),
		zap.String("source", sourcePath),
		zap.String("dest", destPath),
	)

	// First, create the destination directory in the container
	mkdirResult, mkdirErr := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_mkdir",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "mkdir", "-p", destPath},
		Timeout: 30 * time.Second,
	})

	if mkdirErr != nil {
		l.logger.Error("failed to create directory in container",
			zap.String("containerId", containerID),
			zap.String("destPath", destPath),
			zap.Error(mkdirErr),
		)
		if mkdirResult != nil {
			for _, line := range mkdirResult.Lines {
				l.logger.Error("mkdir output", zap.String("stream", line.Stream), zap.String("text", line.Text))
			}
		}
		return fmt.Errorf("failed to create directory %s in container: %v", destPath, mkdirErr)
	}

	// Copy files using lxc file push with recursive flag
	// Format: lxc file push -r /source/path/ containerName/dest/path/
	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_copy",
		Command: "lxc",
		Args:    []string{"file", "push", "-r", sourcePath + "/.", containerID + destPath},
		Timeout: 5 * time.Minute,
	})

	if err != nil || !result.Success {
		l.logger.Error("failed to copy files to container",
			zap.String("containerId", containerID),
			zap.String("source", sourcePath),
			zap.String("dest", destPath),
			zap.Error(err),
		)

		// Log all output lines for debugging
		if result != nil {
			l.logger.Error("lxc file push details",
				zap.Int("exitCode", result.ExitCode),
				zap.Bool("success", result.Success),
			)
			for _, line := range result.Lines {
				l.logger.Error("lxc file push output",
					zap.String("stream", line.Stream),
					zap.String("text", line.Text),
				)
			}
		}

		if err != nil {
			return fmt.Errorf("failed to copy files from %s to %s: %v", sourcePath, destPath, err)
		}
		return fmt.Errorf("failed to copy files from %s to %s: command failed with exit code %d", sourcePath, destPath, result.ExitCode)
	}

	l.logger.Info("files copied successfully",
		zap.String("containerId", containerID),
		zap.Int("lines", len(result.Lines)),
	)

	return nil
}

// RunCommandInContainer runs a command inside an LXD container
func (l *LXDService) RunCommandInContainer(ctx context.Context, containerID, command string) (*exec.ExecResult, error) {
	l.logger.Info("running command in container",
		zap.String("containerId", containerID),
		zap.String("command", command),
	)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_exec",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "/bin/sh", "-c", command},
		Timeout: 5 * time.Minute,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to run command: %w", err)
	}

	return result, nil
}

// DetectFrameworkInContainer detects the framework by examining files inside the container
func (l *LXDService) DetectFrameworkInContainer(ctx context.Context, containerID, workDir string) FrameworkType {
	// Comprehensive framework detection using a single shell command
	cmd := fmt.Sprintf(`
		cd %s && \
		if [ -f next.config.js ] || [ -f next.config.mjs ] || [ -f next.config.ts ]; then
			echo 'nextjs'
		elif [ -f nuxt.config.js ] || [ -f nuxt.config.ts ]; then
			echo 'nuxtjs'
		elif [ -f remix.config.js ]; then
			echo 'remix'
		elif [ -f package.json ]; then
			if grep -q '"nest"' package.json 2>/dev/null || grep -q '"@nestjs/core"' package.json 2>/dev/null; then
				echo 'nestjs'
			elif grep -q '"express"' package.json 2>/dev/null; then
				echo 'express'
			elif grep -q '"fastify"' package.json 2>/dev/null; then
				echo 'fastify'
			elif grep -q '"svelte"' package.json 2>/dev/null; then
				echo 'svelte'
			elif grep -q '"@angular/core"' package.json 2>/dev/null; then
				echo 'angular'
			elif grep -q '"vue"' package.json 2>/dev/null || grep -q '"vue-router"' package.json 2>/dev/null; then
				echo 'vue'
			elif grep -q '"react"' package.json 2>/dev/null || grep -q '"react-dom"' package.json 2>/dev/null; then
				echo 'react'
			elif grep -q '"vite"' package.json 2>/dev/null; then
				echo 'vite'
			elif grep -q '"webpack"' package.json 2>/dev/null; then
				echo 'webpack'
			elif grep -q '"next"' package.json 2>/dev/null; then
				echo 'nextjs'
			else
				echo 'node'
			fi
		elif [ -f requirements.txt ]; then
			if grep -qi 'flask' requirements.txt 2>/dev/null; then
				echo 'flask'
			elif grep -qi 'django' requirements.txt 2>/dev/null; then
				echo 'django'
			elif grep -qi 'fastapi' requirements.txt 2>/dev/null; then
				echo 'fastapi'
			else
				echo 'python'
			fi
		elif [ -f go.mod ]; then
			echo 'go'
		elif [ -f index.html ] || [ -f dist/index.html ] || [ -f build/index.html ]; then
			echo 'static'
		else
			echo 'static'
		fi
	`, workDir)

	result, err := l.RunCommandInContainer(ctx, containerID, cmd)
	if err != nil || result == nil || !result.Success {
		return FrameworkStatic
	}

	// Parse result
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			switch strings.TrimSpace(line.Text) {
			case "nextjs":
				return FrameworkNextJS
			case "nuxtjs":
				return FrameworkNuxtJS
			case "remix":
				return FrameworkRemix
			case "nestjs":
				return FrameworkNestJS
			case "express":
				return FrameworkExpress
			case "fastify":
				return FrameworkFastify
			case "svelte":
				return FrameworkSvelte
			case "angular":
				return FrameworkAngular
			case "vue":
				return FrameworkVue
			case "react":
				return FrameworkReact
			case "vite":
				return FrameworkVite
			case "webpack":
				return FrameworkWebpack
			case "node":
				return FrameworkNode
			case "flask":
				return FrameworkFlask
			case "django":
				return FrameworkDjango
			case "fastapi":
				return FrameworkFastAPI
			case "go":
				return FrameworkGo
			case "python":
				return FrameworkFlask
			case "static":
				return FrameworkStatic
			}
		}
	}

	return FrameworkStatic
}

// StartAppService starts the OpenRC managed application service in the container
func (l *LXDService) StartAppService(ctx context.Context, containerID string) error {
	// Start the OpenRC service
	_, err := l.RunCommandInContainer(ctx, containerID, "rc-service opendeploy-app start")
	return err
}

// StopAppService stops the OpenRC managed application service in the container
func (l *LXDService) StopAppService(ctx context.Context, containerID string) error {
	// Stop the OpenRC service
	_, err := l.RunCommandInContainer(ctx, containerID, "rc-service opendeploy-app stop")
	return err
}

// RestartAppService restarts the OpenRC managed application service in the container
func (l *LXDService) RestartAppService(ctx context.Context, containerID string) error {
	// Restart the OpenRC service
	_, err := l.RunCommandInContainer(ctx, containerID, "rc-service opendeploy-app restart")
	return err
}

// GetAppServiceStatus gets the status of the application service in the container
func (l *LXDService) GetAppServiceStatus(ctx context.Context, containerID string) (string, error) {
	result, err := l.RunCommandInContainer(ctx, containerID, "rc-service opendeploy-app status")
	if err != nil {
		return "unknown", err
	}

	// Parse status from output
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			if strings.Contains(line.Text, "started") {
				return "running", nil
			} else if strings.Contains(line.Text, "stopped") {
				return "stopped", nil
			} else if strings.Contains(line.Text, "crashed") {
				return "failed", nil
			}
		}
	}

	return "unknown", nil
}

// GetAppServiceLogs gets the application service logs from the container
func (l *LXDService) GetAppServiceLogs(ctx context.Context, containerID string, lines int) (string, error) {
	logCmd := fmt.Sprintf("tail -n %d /var/log/opendeploy-app.log", lines)
	result, err := l.RunCommandInContainer(ctx, containerID, logCmd)
	if err != nil {
		return "", err
	}

	var logs strings.Builder
	for _, line := range result.Lines {
		logs.WriteString(line.Text + "\n")
	}

	return logs.String(), nil
}

// DetectFramework detects the framework of a project in a directory
func (l *LXDService) DetectFramework(projectDir string) FrameworkType {
	// Check for Next.js
	if fileExists(filepath.Join(projectDir, "next.config.js")) ||
		fileExists(filepath.Join(projectDir, "next.config.mjs")) ||
		fileExists(filepath.Join(projectDir, "next.config.ts")) {
		return FrameworkNextJS
	}

	// Check for Nuxt.js
	if fileExists(filepath.Join(projectDir, "nuxt.config.js")) ||
		fileExists(filepath.Join(projectDir, "nuxt.config.ts")) {
		return FrameworkNuxtJS
	}

	// Check for Remix
	if fileExists(filepath.Join(projectDir, "remix.config.js")) {
		return FrameworkRemix
	}

	// Check for package.json and analyze dependencies
	if fileExists(filepath.Join(projectDir, "package.json")) {
		content, err := os.ReadFile(filepath.Join(projectDir, "package.json"))
		if err == nil {
			var pkg struct {
				Dependencies    map[string]interface{} `json:"dependencies"`
				DevDependencies map[string]interface{} `json:"devDependencies"`
			}
			if json.Unmarshal(content, &pkg) == nil {
				deps := make(map[string]interface{})
				for k, v := range pkg.Dependencies {
					deps[k] = v
				}
				for k, v := range pkg.DevDependencies {
					deps[k] = v
				}

				// Check for various frameworks
				if _, hasNext := deps["@nestjs/core"]; hasNext {
					return FrameworkNestJS
				}
				if _, hasExpress := deps["express"]; hasExpress {
					return FrameworkExpress
				}
				if _, hasFastify := deps["fastify"]; hasFastify {
					return FrameworkFastify
				}
				if _, hasNext := deps["next"]; hasNext {
					return FrameworkNextJS
				}
				if _, hasNuxt := deps["nuxt"]; hasNuxt {
					return FrameworkNuxtJS
				}
				if _, hasRemix := deps["@remix-run/node"]; hasRemix {
					return FrameworkRemix
				}
				if _, hasReact := deps["react"]; hasReact {
					return FrameworkReact
				}
				if _, hasVue := deps["vue"]; hasVue {
					return FrameworkVue
				}
				if _, hasAngular := deps["@angular/core"]; hasAngular {
					return FrameworkAngular
				}
				if _, hasSvelte := deps["svelte"]; hasSvelte {
					return FrameworkSvelte
				}
				if _, hasWebpack := deps["webpack"]; hasWebpack {
					return FrameworkWebpack
				}
				if _, hasVite := deps["vite"]; hasVite {
					return FrameworkVite
				}
			}
		}
	}

	// Check for Python frameworks
	if fileExists(filepath.Join(projectDir, "requirements.txt")) ||
		fileExists(filepath.Join(projectDir, "pyproject.toml")) {
		content, err := os.ReadFile(filepath.Join(projectDir, "requirements.txt"))
		if err != nil {
			// Try pyproject.toml if requirements.txt doesn't exist
			content, err = os.ReadFile(filepath.Join(projectDir, "pyproject.toml"))
		}

		if err == nil {
			text := string(content)
			if strings.Contains(text, "flask") {
				return FrameworkFlask
			}
			if strings.Contains(text, "django") {
				return FrameworkDjango
			}
			if strings.Contains(text, "fastapi") {
				return FrameworkFastAPI
			}
		}
	}

	// Check for Go
	if fileExists(filepath.Join(projectDir, "go.mod")) {
		return FrameworkGo
	}

	// Check for static sites
	if fileExists(filepath.Join(projectDir, "index.html")) ||
		fileExists(filepath.Join(projectDir, "public/index.html")) ||
		fileExists(filepath.Join(projectDir, "dist/index.html")) {
		return FrameworkStatic
	}

	// Default to Node if package.json exists
	if fileExists(filepath.Join(projectDir, "package.json")) {
		return FrameworkNode
	}

	return FrameworkUnknown
}

// InstallLatestNodeJS installs the latest Node.js LTS version in the container
// OR sets up a bind mount to use the host's Node.js installation
func (l *LXDService) InstallLatestNodeJS(ctx context.Context, containerID string) error {
	l.logger.Info("setting up Node.js in container",
		zap.String("containerId", containerID),
	)

	// First, try to use host's Node.js via bind mount (most efficient)
	// Check if host has Node.js installed
	hostNodePaths := []string{
		"/usr/bin/node",
		"/usr/local/bin/node",
		"/usr/local/node/bin/node",
		"/opt/node/bin/node",
	}

	// Try to find node on host
	var hostNodePath string
	for _, path := range hostNodePaths {
		checkCmd := fmt.Sprintf("test -f %s && echo 'found'", path)
		result, err := l.runner.Run(ctx, exec.RunOpts{
			JobType: "check_host_node",
			Command: "/bin/sh",
			Args:    []string{"-c", checkCmd},
			Timeout: 5 * time.Second,
		})

		if err == nil && result.Success {
			for _, line := range result.Lines {
				if strings.Contains(line.Text, "found") {
					hostNodePath = path
					l.logger.Info("found Node.js on host",
						zap.String("path", hostNodePath),
					)
					break
				}
			}
		}
		if hostNodePath != "" {
			break
		}
	}

	if hostNodePath != "" {
		// Use bind mount to share host's Node.js
		l.logger.Info("using host Node.js via bind mount",
			zap.String("hostPath", hostNodePath),
		)

		// Get the directory containing node
		nodeDir := filepath.Dir(hostNodePath)

		// Remove existing device if it exists
		removeArgs := []string{"config", "device", "remove", containerID, "host-nodejs"}
		_, _ = l.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_remove_device",
			Command: "lxc",
			Args:    removeArgs,
			Timeout: 5 * time.Second,
		})

		// Add disk device to bind mount the node directory
		args := []string{
			"config", "device", "add", containerID,
			"host-nodejs",
			"disk",
			fmt.Sprintf("source=%s", nodeDir),
			fmt.Sprintf("path=%s", nodeDir),
		}

		result, err := l.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_mount_nodejs",
			Command: "lxc",
			Args:    args,
			Timeout: 10 * time.Second,
		})

		if err != nil || !result.Success {
			l.logger.Warn("failed to bind mount host Node.js, will try install in container",
				zap.Error(err),
			)
			// Fall through to installation method
		} else {
			// Restart container to apply the mount
			l.logger.Info("restarting container to apply Node.js mount")
			restartResult, _ := l.runner.Run(ctx, exec.RunOpts{
				JobType: "lxd_restart",
				Command: "lxc",
				Args:    []string{"restart", containerID},
				Timeout: 30 * time.Second,
			})

			if restartResult != nil && restartResult.Success {
				// Wait a moment for container to be ready
				time.Sleep(3 * time.Second)

				// Verify node is accessible in container
				verifyResult, _ := l.RunCommandInContainer(ctx, containerID, "node --version")
				if verifyResult != nil && verifyResult.Success {
					l.logger.Info("host Node.js successfully mounted in container")
					return nil
				}
			}
		}
	}

	// Fallback: Install Node.js in container using curl (already installed)
	l.logger.Info("installing Node.js in container using curl")

	// Install Node.js 22.x LTS from official binary
	// This is much newer than Alpine's nodejs package (v20.15.1)
	installScript := `
set -e
cd /tmp
# Detect architecture
ARCH=$(uname -m)
if [ "$ARCH" = "aarch64" ]; then
    NODE_ARCH="arm64"
elif [ "$ARCH" = "x86_64" ]; then
    NODE_ARCH="x64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

# Download and install Node.js 22.x LTS using curl (already installed)
NODE_VERSION="v22.13.1"
curl -fsSLO https://nodejs.org/dist/${NODE_VERSION}/node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz
tar -xf node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz -C /usr/local --strip-components=1
rm node-${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz

# Verify installation
node --version
npm --version
`

	result, err := l.RunCommandInContainer(ctx, containerID, installScript)
	if err != nil {
		return fmt.Errorf("failed to install Node.js: %w", err)
	}

	if result.ExitCode != 0 {
		var errMsg string
		for _, line := range result.Lines {
			if line.Stream == "stderr" {
				errMsg += line.Text + "\n"
			}
		}
		return fmt.Errorf("Node.js installation failed: %s", errMsg)
	}

	l.logger.Info("Node.js installed successfully",
		zap.String("containerId", containerID),
	)

	return nil
}
