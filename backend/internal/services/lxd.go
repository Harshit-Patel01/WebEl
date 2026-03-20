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
		return fmt.Sprintf("npm start")
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
	runner *exec.Runner
	cfg    config.DeployConfig
	db     *state.DB
	logger *zap.Logger
}

func NewLXDService(runner *exec.Runner, cfg config.DeployConfig, db *state.DB, logger *zap.Logger) *LXDService {
	return &LXDService{runner: runner, cfg: cfg, db: db, logger: logger}
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
	containerName := fmt.Sprintf("opendeploy-%s", projectName)

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
		Args:    []string{"launch", image, containerName, "-c", "security.nesting=true", "-c", "security.privileged=true"},
		Timeout: 60 * time.Second,
	})

	if err != nil || !result.Success {
		return nil, fmt.Errorf("failed to launch container: %w", err)
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
		return nil, fmt.Errorf("failed to get container ID")
	}

	// Wait for container to be ready
	time.Sleep(2 * time.Second)

	// Get container IP
	ip, err := l.GetContainerIP(ctx, containerID)
	if err != nil {
		l.logger.Warn("failed to get container IP, container may not have network yet",
			zap.String("containerId", containerID),
			zap.Error(err),
		)
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
		Timeout: 10 * time.Minute, // Longer timeout for npm install, etc.
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

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_delete",
		Command: "lxc",
		Args:    []string{"delete", "-f", containerName},
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

	if err != nil || !result.Success {
		return fmt.Errorf("failed to setup port proxy: %w", err)
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

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_copy",
		Command: "lxc",
		Args:    []string{"file", "push", sourcePath, fmt.Sprintf("%s%s", containerID, destPath)},
		Timeout: 60 * time.Second,
	})

	if err != nil || !result.Success {
		return fmt.Errorf("failed to copy files: %w", err)
	}

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
