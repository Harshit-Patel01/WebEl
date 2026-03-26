package services

import (
	"context"
	"fmt"
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
	case FrameworkNode, FrameworkNestJS, FrameworkExpress, FrameworkFastify, FrameworkFlask, FrameworkDjango, FrameworkFastAPI, FrameworkGo:
		return true
	case FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix:
		return true // SSR frameworks that run a Node.js server
	default:
		return false // React, Vue, Angular, Svelte, Webpack, Vite, Static — all need nginx
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
	case FrameworkNode, FrameworkNestJS, FrameworkExpress, FrameworkFastify, FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix, FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkWebpack, FrameworkVite:
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
		return fmt.Sprintf("node server.js")
	case FrameworkFastify:
		return fmt.Sprintf("node server.js")
	case FrameworkFlask:
		return fmt.Sprintf("FLASK_APP=app.py flask run --host=0.0.0.0 --port %d", port)
	case FrameworkDjango:
		return fmt.Sprintf("python manage.py runserver 0.0.0.0:%d", port)
	case FrameworkFastAPI:
		return fmt.Sprintf("uvicorn main:app --host 0.0.0.0 --port %d", port)
	case FrameworkGo:
		return fmt.Sprintf("./server")
	case FrameworkNode:
		return fmt.Sprintf("node server.js")
	case FrameworkRemix:
		return fmt.Sprintf("npm run start -- --port %d", port)
	default:
		return "npm start"
	}
}

// GetNPMScriptName extracts the npm script name from a start command for use with pm2 start npm.
// e.g. "npm start" → "start", "npm run dev" → "dev", "node server.js" → "start" (fallback)
func GetNPMScriptName(startCmd string) string {
	s := strings.TrimSpace(startCmd)
	if strings.HasPrefix(s, "npm run ") {
		return strings.TrimPrefix(s, "npm run ")
	}
	if strings.HasPrefix(s, "npm ") {
		return strings.TrimPrefix(s, "npm ")
	}
	if s == "npm" {
		return "start"
	}
	return "start"
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

// GetImageTypeForFramework maps framework types to pre-built image types
func GetImageTypeForFramework(framework FrameworkType) ImageType {
	switch framework {
	case FrameworkNextJS, FrameworkNuxtJS, FrameworkRemix, FrameworkReact, FrameworkVue, FrameworkAngular, FrameworkSvelte, FrameworkWebpack, FrameworkVite:
		return ImageFrontend
	case FrameworkNode, FrameworkNestJS, FrameworkExpress, FrameworkFastify:
		return ImageNodeJS
	case FrameworkFlask, FrameworkDjango, FrameworkFastAPI:
		return ImagePython
	case FrameworkGo:
		return ImageGo
	case FrameworkStatic:
		return ImageStatic
	default:
		return ImageNodeJS // Default to NodeJS
	}
}

// LXDService handles LXD container-based deployments
type LXDService struct {
	runner       *exec.Runner
	cfg          config.DeployConfig
	db           *state.DB
	logger       *zap.Logger
	initialized  bool
	ImageBuilder *ImageBuilder
}

func NewLXDService(runner *exec.Runner, cfg config.DeployConfig, db *state.DB, logger *zap.Logger) *LXDService {
	return &LXDService{
		runner:       runner,
		cfg:          cfg,
		db:           db,
		logger:       logger,
		initialized:  false,
		ImageBuilder: NewImageBuilder(runner, logger),
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

// CreateContainerWithUserDataAndFramework creates an LXD container using pre-built images when available
func (l *LXDService) CreateContainerWithUserDataAndFramework(ctx context.Context, projectID, projectName, image, userData string, framework FrameworkType) (*ContainerInfo, error) {
	// Ensure LXD is initialized
	if err := l.EnsureLXDInitialized(ctx); err != nil {
		return nil, fmt.Errorf("LXD initialization failed: %w", err)
	}

	containerName := fmt.Sprintf("opendeploy-%s-%d", projectName, time.Now().Unix())

	// Use pre-built image if available for the framework
	if framework != FrameworkUnknown {
		imageType := GetImageTypeForFramework(framework)
		preBuiltImage := l.ImageBuilder.GetImageName(imageType)

		// Check if pre-built image exists
		if err := l.ImageBuilder.EnsureImage(ctx, imageType); err == nil {
			l.logger.Info("using pre-built image",
				zap.String("image", preBuiltImage),
				zap.String("framework", string(framework)))
			image = preBuiltImage
		} else {
			l.logger.Warn("pre-built image not available, using base image",
				zap.String("framework", string(framework)),
				zap.Error(err))
		}
	}

	// Check if container already exists
	existingContainer, _ := l.db.GetContainerByName(containerName)
	if existingContainer != nil {
		status, err := l.GetContainerStatus(ctx, existingContainer.ContainerID)
		if err == nil && status != "stopped" && status != "unknown" {
			l.logger.Info("stopping existing container before creating new one",
				zap.String("projectId", projectID),
				zap.String("containerName", existingContainer.Name),
			)
			l.StopContainer(ctx, existingContainer.ContainerID)
		}

		l.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_delete",
			Command: "lxc",
			Args:    []string{"delete", "-f", existingContainer.ContainerID},
			Timeout: 30 * time.Second,
		})

		l.db.DeleteContainer(existingContainer.ID)
	}

	// Use lxc init to create container without starting (so we can configure before boot)
	l.logger.Info("initializing lxd container",
		zap.String("projectId", projectID),
		zap.String("containerName", containerName),
		zap.String("image", image),
	)

	initResult, initErr := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_init",
		Command: "lxc",
		Args:    []string{"init", image, containerName},
		Timeout: 60 * time.Second,
	})

	if initErr != nil || (initResult != nil && !initResult.Success) {
		return nil, fmt.Errorf("failed to init container: %w", initErr)
	}

	containerID := containerName

	// Configure container network optimizations
	l.logger.Info("configuring container network", zap.String("containerId", containerID))
	l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_config",
		Command: "lxc",
		Args:    []string{"config", "set", containerID, "limits.network.priority", "10"},
		Timeout: 10 * time.Second,
	})

	// Start the container
	l.logger.Info("starting container", zap.String("containerId", containerID))
	startResult, startErr := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_start",
		Command: "lxc",
		Args:    []string{"start", containerID},
		Timeout: 30 * time.Second,
	})

	if startErr != nil || (startResult != nil && !startResult.Success) {
		return nil, fmt.Errorf("failed to start container: %w", startErr)
	}

	// Wait for container to be ready and get network
	time.Sleep(5 * time.Second)

	// Execute user-data setup script if provided
	// (Alpine doesn't have cloud-init, so we run it directly)
	if userData != "" {
		l.logger.Info("running setup script in container",
			zap.String("containerId", containerID),
		)

		setupResult, setupErr := l.runner.Run(ctx, exec.RunOpts{
			JobType: "lxd_setup",
			Command: "lxc",
			Args:    []string{"exec", containerID, "--", "/bin/sh", "-c", userData},
			Timeout: 10 * time.Minute,
		})

		if setupErr != nil || (setupResult != nil && !setupResult.Success) {
			var allOutput string
			if setupResult != nil {
				for _, line := range setupResult.Lines {
					allOutput += fmt.Sprintf("[%s] %s\n", line.Stream, line.Text)
				}
			}
			return nil, fmt.Errorf("container setup failed (exit code: %d):\n%s(error: %v)", func() int {
				if setupResult != nil {
					return setupResult.ExitCode
				}
				return -1
			}(), allOutput, setupErr)
		}

		l.logger.Info("container setup completed", zap.String("containerId", containerID))
	}

	// Get container IP
	ip, err := l.GetContainerIP(ctx, containerID)
	if err != nil {
		l.logger.Warn("failed to get container IP, container may not have network yet",
			zap.String("containerId", containerID),
			zap.Error(err),
		)
		ip = ""
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

// UpdatePortMapping changes the host and/or container port mapping for a running container.
// It removes the old proxy device and adds a new one with the updated ports.
func (l *LXDService) UpdatePortMapping(ctx context.Context, containerID string, oldContainerPort, newContainerPort, newHostPort int) error {
	l.logger.Info("updating port mapping",
		zap.String("containerId", containerID),
		zap.Int("oldContainerPort", oldContainerPort),
		zap.Int("newContainerPort", newContainerPort),
		zap.Int("newHostPort", newHostPort),
	)

	oldDeviceName := fmt.Sprintf("proxy-%d", oldContainerPort)
	newDeviceName := fmt.Sprintf("proxy-%d", newContainerPort)

	// Remove the existing proxy device
	removeResult, removeErr := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_proxy_remove",
		Command: "lxc",
		Args:    []string{"config", "device", "remove", containerID, oldDeviceName},
		Timeout: 10 * time.Second,
	})

	if removeErr != nil || (removeResult != nil && !removeResult.Success) {
		var errMsg string
		if removeResult != nil {
			for _, line := range removeResult.Lines {
				if line.Stream == "stderr" {
					errMsg += line.Text + "\n"
				}
			}
		}
		return fmt.Errorf("failed to remove old port proxy: %s (error: %v)", errMsg, removeErr)
	}

	// Add the new proxy device with updated ports
	addResult, addErr := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_proxy_add",
		Command: "lxc",
		Args: []string{
			"config", "device", "add", containerID,
			newDeviceName,
			"proxy",
			fmt.Sprintf("listen=tcp:0.0.0.0:%d", newHostPort),
			fmt.Sprintf("connect=tcp:127.0.0.1:%d", newContainerPort),
		},
		Timeout: 10 * time.Second,
	})

	if addErr != nil || (addResult != nil && !addResult.Success) {
		var errMsg string
		if addResult != nil {
			for _, line := range addResult.Lines {
				if line.Stream == "stderr" {
					errMsg += line.Text + "\n"
				}
			}
		}
		return fmt.Errorf("failed to add new port proxy: %s (error: %v)", errMsg, addErr)
	}

	l.logger.Info("port mapping updated successfully",
		zap.String("containerId", containerID),
		zap.Int("oldContainerPort", oldContainerPort),
		zap.Int("newContainerPort", newContainerPort),
		zap.Int("newHostPort", newHostPort),
	)

	return nil
}

// RunCommandInContainer runs a command inside an LXD container
func (l *LXDService) RunCommandInContainer(ctx context.Context, containerID, command string) (*exec.ExecResult, error) {
	l.logger.Info("running command in container",
		zap.String("containerId", containerID),
		zap.String("command", command),
	)

	command = wrapWithNodePath(command)

	args := []string{"exec", containerID}
	args = append(args, getNodeEnvFlags()...)
	args = append(args, "--", "/bin/sh", "-c", command)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_exec",
		Command: "lxc",
		Args:    args,
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
	// Start all PM2 managed processes
	_, err := l.RunCommandInContainer(ctx, containerID, "pm2 start all")
	return err
}

// StopAppService stops all PM2 managed application processes in the container
func (l *LXDService) StopAppService(ctx context.Context, containerID string) error {
	// Stop all PM2 managed processes
	_, err := l.RunCommandInContainer(ctx, containerID, "pm2 stop all")
	return err
}

// RestartAppService restarts all PM2 managed application processes in the container
func (l *LXDService) RestartAppService(ctx context.Context, containerID string) error {
	// Restart all PM2 managed processes
	_, err := l.RunCommandInContainer(ctx, containerID, "pm2 restart all")
	return err
}

// GetAppServiceStatus gets the status of PM2 managed application processes in the container
func (l *LXDService) GetAppServiceStatus(ctx context.Context, containerID string) (string, error) {
	result, err := l.RunCommandInContainer(ctx, containerID, "pm2 jlist")
	if err != nil {
		return "unknown", err
	}

	// Parse status from pm2 jlist JSON output
	var output strings.Builder
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			output.WriteString(line.Text)
		}
	}

	jsonStr := strings.TrimSpace(output.String())
	if jsonStr == "" || jsonStr == "[]" {
		return "stopped", nil
	}

	// Check for any running processes in the JSON
	if strings.Contains(jsonStr, `"status":"online"`) {
		return "running", nil
	}
	if strings.Contains(jsonStr, `"status":"stopped"`) {
		return "stopped", nil
	}
	if strings.Contains(jsonStr, `"status":"errored"`) {
		return "failed", nil
	}

	return "unknown", nil
}

// GetAppServiceLogs gets the application service logs from the container
func (l *LXDService) GetAppServiceLogs(ctx context.Context, containerID string, lines int) (string, error) {
	logCmd := fmt.Sprintf("pm2 logs --nostream --lines %d 2>/dev/null || echo no_logs_found", lines)
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
