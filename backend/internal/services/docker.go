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
	"go.uber.org/zap"
)

// FrameworkType identifies the specific framework being used.
type FrameworkType string

const (
	FrameworkNodeExpress   FrameworkType = "node_express"
	FrameworkPythonFastAPI FrameworkType = "python_fastapi"
	FrameworkPythonDjango  FrameworkType = "python_django"
	FrameworkGo            FrameworkType = "go"
	FrameworkNodeStatic    FrameworkType = "node_static"
	FrameworkStatic        FrameworkType = "static"
)

// DockerService handles Docker-based builds.
type DockerService struct {
	runner *exec.Runner
	cfg    config.DeployConfig
	logger *zap.Logger
}

func NewDockerService(runner *exec.Runner, cfg config.DeployConfig, logger *zap.Logger) *DockerService {
	return &DockerService{runner: runner, cfg: cfg, logger: logger}
}

// DetectFramework identifies the project framework from repo contents.
func (d *DockerService) DetectFramework(projectDir string) FrameworkType {
	// Check Go first (go.mod)
	if fileExists(filepath.Join(projectDir, "go.mod")) {
		return FrameworkGo
	}

	// Check Python
	if fileExists(filepath.Join(projectDir, "requirements.txt")) || fileExists(filepath.Join(projectDir, "pyproject.toml")) {
		// Distinguish FastAPI vs Django
		if d.fileContains(filepath.Join(projectDir, "requirements.txt"), "django") ||
			fileExists(filepath.Join(projectDir, "manage.py")) {
			return FrameworkPythonDjango
		}
		return FrameworkPythonFastAPI
	}

	// Check Node.js
	if fileExists(filepath.Join(projectDir, "package.json")) {
		// Check if it has a server framework (Express)
		if d.fileContains(filepath.Join(projectDir, "package.json"), "express") ||
			d.fileContains(filepath.Join(projectDir, "package.json"), "fastify") {
			return FrameworkNodeExpress
		}
		return FrameworkNodeStatic
	}

	// Pure static site (no package.json, no build tools)
	return FrameworkStatic
}

func (d *DockerService) fileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), strings.ToLower(substr))
}

// GenerateDockerfile creates a Dockerfile for the given framework.
func (d *DockerService) GenerateDockerfile(framework FrameworkType, installCmd, startCmd string, port int) string {
	switch framework {
	case FrameworkNodeExpress:
		return d.dockerfileNodeExpress(installCmd, startCmd, port)
	case FrameworkNodeStatic:
		return d.dockerfileNodeStatic(installCmd, startCmd)
	case FrameworkPythonFastAPI:
		return d.dockerfilePythonFastAPI(installCmd, startCmd, port)
	case FrameworkPythonDjango:
		return d.dockerfilePythonDjango(installCmd, startCmd, port)
	case FrameworkGo:
		return d.dockerfileGo(port)
	default:
		return d.dockerfileNodeStatic(installCmd, startCmd)
	}
}

func (d *DockerService) dockerfileNodeExpress(installCmd, startCmd string, port int) string {
	if installCmd == "" {
		installCmd = "npm ci --production=false"
	}
	if startCmd == "" {
		startCmd = "npm start"
	}
	if port == 0 {
		port = 3000
	}
	return fmt.Sprintf(`FROM node:20-slim
WORKDIR /app
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, installCmd, port, startCmd)
}

func (d *DockerService) dockerfileNodeStatic(installCmd, buildCmd string) string {
	if installCmd == "" {
		installCmd = "npm ci"
	}
	if buildCmd == "" {
		buildCmd = "npm run build"
	}
	return fmt.Sprintf(`FROM node:20-slim AS builder
WORKDIR /app
COPY . .
RUN %s
RUN %s
`, installCmd, buildCmd)
}

func (d *DockerService) dockerfilePythonFastAPI(installCmd, startCmd string, port int) string {
	if installCmd == "" {
		installCmd = "pip install --no-cache-dir -r requirements.txt"
	}
	if startCmd == "" {
		startCmd = "uvicorn main:app --host 0.0.0.0 --port 8000"
	}
	if port == 0 {
		port = 8000
	}
	return fmt.Sprintf(`FROM python:3.11-slim
WORKDIR /app
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, installCmd, port, startCmd)
}

func (d *DockerService) dockerfilePythonDjango(installCmd, startCmd string, port int) string {
	if installCmd == "" {
		installCmd = "pip install --no-cache-dir -r requirements.txt"
	}
	if startCmd == "" {
		startCmd = "gunicorn --bind 0.0.0.0:8000 --workers 2 config.wsgi:application"
	}
	if port == 0 {
		port = 8000
	}
	return fmt.Sprintf(`FROM python:3.11-slim
WORKDIR /app
COPY . .
RUN %s
RUN python manage.py collectstatic --noinput || true
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, installCmd, port, startCmd)
}

func (d *DockerService) dockerfileGo(port int) string {
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server /app/server
RUN chmod +x /app/server
EXPOSE %d
CMD ["/app/server"]
`, port)
}

// BuildInDocker runs the project build inside a Docker container.
// The Docker build context is the locally-cloned project directory,
// so Dockerfiles use COPY instead of cloning the repo again.
func (d *DockerService) BuildInDocker(ctx context.Context, projectID, jobID string, framework FrameworkType, installCmd, startCmd string, port int, envVars map[string]string, workingDir, outputDir string) (*exec.ExecResult, error) {
	containerName := fmt.Sprintf("opendeploy-build-%s", projectID)

	// Generate Dockerfile
	dockerfile := d.GenerateDockerfile(framework, installCmd, startCmd, port)

	// The build context is the locally-cloned project directory.
	// The code was already cloned to /tmp/<projectID> by the deploy service,
	// so we just point Docker at it — no need to re-clone inside the container.
	buildCtxDir := filepath.Join("/tmp", projectID)
	if workingDir != "" && workingDir != "." {
		buildCtxDir = filepath.Join(buildCtxDir, workingDir)
	}

	// Write Dockerfile into the build context
	dockerfilePath := filepath.Join(buildCtxDir, "Dockerfile.opendeploy")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return nil, fmt.Errorf("writing Dockerfile: %w", err)
	}
	defer os.Remove(dockerfilePath) // Clean up after build

	// Create a .dockerignore if it doesn't exist to skip .git
	dockerignorePath := filepath.Join(buildCtxDir, ".dockerignore")
	needCleanupIgnore := false
	if !fileExists(dockerignorePath) {
		ignoreContent := ".git\nnode_modules\n__pycache__\n*.pyc\n.env\n"
		os.WriteFile(dockerignorePath, []byte(ignoreContent), 0644)
		needCleanupIgnore = true
	}
	if needCleanupIgnore {
		defer os.Remove(dockerignorePath)
	}

	// Build Docker image
	imageName := fmt.Sprintf("opendeploy/%s:latest", projectID)
	buildArgs := []string{
		"build",
		"--no-cache",
		"--progress=plain",
		"-f", dockerfilePath,
		"-t", imageName,
		"--memory", d.cfg.DockerMemoryLimit,
	}

	// Add env vars as build args
	for k, v := range envVars {
		buildArgs = append(buildArgs, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	buildArgs = append(buildArgs, buildCtxDir)

	d.logger.Info("building docker image",
		zap.String("projectId", projectID),
		zap.String("framework", string(framework)),
		zap.String("image", imageName),
		zap.String("buildContext", buildCtxDir),
	)

	buildResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-docker-build",
		BroadcastJobID: jobID,
		JobType:        "docker_build",
		Command:        d.cfg.DockerBinary,
		Args:           buildArgs,
		MergeEnv:       true,
		Timeout:        d.cfg.BuildTimeout,
	})
	if err != nil || !buildResult.Success {
		return buildResult, fmt.Errorf("docker build failed")
	}

	// For static frontend builds, copy output from container
	if framework == FrameworkNodeStatic {
		if outputDir == "" {
			outputDir = "dist"
		}
		return d.copyStaticOutput(ctx, imageName, containerName, projectID, outputDir, "", jobID)
	}

	// For backend apps, the image is ready to run as a container
	// The container will be started separately by the container service
	d.logger.Info("backend docker image built successfully",
		zap.String("projectId", projectID),
		zap.String("image", imageName),
	)

	return buildResult, nil
}

// copyStaticOutput extracts built static files from the Docker container.
func (d *DockerService) copyStaticOutput(ctx context.Context, imageName, containerName, projectID, outputDir, workingDir, jobID string) (*exec.ExecResult, error) {
	destDir := filepath.Join(d.cfg.OutputRoot, "frontend", projectID)
	os.MkdirAll(destDir, 0755)

	// Create temp container
	createResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-docker-create",
		BroadcastJobID: jobID,
		JobType:        "docker_create",
		Command:        d.cfg.DockerBinary,
		Args:           []string{"create", "--name", containerName, imageName},
		Timeout:        30 * time.Second,
	})
	if err != nil || !createResult.Success {
		return createResult, fmt.Errorf("failed to create container for output copy")
	}

	// Files are at /app/<outputDir> since we COPY to /app and build there
	srcPath := fmt.Sprintf("%s:/app/%s/.", containerName, outputDir)
	copyResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-docker-cp",
		BroadcastJobID: jobID,
		JobType:        "docker_cp",
		Command:        d.cfg.DockerBinary,
		Args:           []string{"cp", srcPath, destDir},
		Timeout:        2 * time.Minute,
	})

	// Cleanup container
	d.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_rm",
		Command: d.cfg.DockerBinary,
		Args:    []string{"rm", "-f", containerName},
		Timeout: 15 * time.Second,
	})

	if err != nil || !copyResult.Success {
		return copyResult, fmt.Errorf("failed to copy build output")
	}

	return copyResult, nil
}

// copyBackendOutput extracts the backend application from the Docker container.
func (d *DockerService) copyBackendOutput(ctx context.Context, imageName, containerName, projectID string, framework FrameworkType, jobID string) (*exec.ExecResult, error) {
	destDir := filepath.Join(d.cfg.OutputRoot, "backend", projectID)
	os.MkdirAll(destDir, 0755)

	// Create temp container
	createResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-docker-create",
		BroadcastJobID: jobID,
		JobType:        "docker_create",
		Command:        d.cfg.DockerBinary,
		Args:           []string{"create", "--name", containerName, imageName},
		Timeout:        30 * time.Second,
	})
	if err != nil || !createResult.Success {
		return createResult, fmt.Errorf("failed to create container for output copy")
	}

	// Copy entire /app directory (the built app is there since we COPY to /app)
	srcPath := fmt.Sprintf("%s:/app/.", containerName)
	copyResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:          jobID + "-docker-cp",
		BroadcastJobID: jobID,
		JobType:        "docker_cp",
		Command:        d.cfg.DockerBinary,
		Args:           []string{"cp", srcPath, destDir},
		Timeout:        2 * time.Minute,
	})

	// Cleanup container
	d.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_rm",
		Command: d.cfg.DockerBinary,
		Args:    []string{"rm", "-f", containerName},
		Timeout: 15 * time.Second,
	})

	if err != nil || !copyResult.Success {
		return copyResult, fmt.Errorf("failed to copy backend output")
	}

	return copyResult, nil
}

// CleanupImage removes a Docker image.
func (d *DockerService) CleanupImage(ctx context.Context, projectID string) {
	imageName := fmt.Sprintf("opendeploy/%s:latest", projectID)
	d.runner.Run(ctx, exec.RunOpts{
		JobType: "docker_rmi",
		Command: d.cfg.DockerBinary,
		Args:    []string{"rmi", "-f", imageName},
		Timeout: 30 * time.Second,
	})
}

// GetStartCommand returns the appropriate start command for a framework.
func GetStartCommand(framework FrameworkType, projectDir string) string {
	switch framework {
	case FrameworkNodeExpress:
		return fmt.Sprintf("/usr/bin/node %s/index.js", projectDir)
	case FrameworkPythonFastAPI:
		return fmt.Sprintf("%s/deps/bin/uvicorn main:app --host 0.0.0.0 --port 8000", projectDir)
	case FrameworkPythonDjango:
		return fmt.Sprintf("%s/deps/bin/gunicorn --bind 0.0.0.0:8000 --workers 2 config.wsgi:application", projectDir)
	case FrameworkGo:
		return fmt.Sprintf("%s/server", projectDir)
	default:
		return ""
	}
}

// GetDefaultPort returns the default port for a framework.
func GetDefaultPort(framework FrameworkType) int {
	switch framework {
	case FrameworkNodeExpress:
		return 3000
	case FrameworkPythonFastAPI, FrameworkPythonDjango:
		return 8000
	case FrameworkGo:
		return 8080
	default:
		return 0
	}
}

// IsBackendFramework returns true if the framework is a backend app (needs a service).
func IsBackendFramework(framework FrameworkType) bool {
	return framework != FrameworkNodeStatic && framework != FrameworkStatic
}

// GetDefaultInstallCommand returns the default install command for a framework.
func GetDefaultInstallCommand(framework FrameworkType) string {
	switch framework {
	case FrameworkNodeExpress:
		return "npm ci --production=false"
	case FrameworkNodeStatic:
		return "npm ci"
	case FrameworkPythonFastAPI, FrameworkPythonDjango:
		return "pip install --no-cache-dir -r requirements.txt"
	case FrameworkGo:
		return "" // Go uses go mod download in Dockerfile
	default:
		return ""
	}
}

// GetDefaultStartCommand returns the default start command for a framework.
func GetDefaultStartCommand(framework FrameworkType, port int) string {
	if port == 0 {
		port = GetDefaultPort(framework)
	}
	switch framework {
	case FrameworkNodeExpress:
		return "npm start"
	case FrameworkPythonFastAPI:
		return fmt.Sprintf("uvicorn main:app --host 0.0.0.0 --port %d", port)
	case FrameworkPythonDjango:
		return fmt.Sprintf("gunicorn --bind 0.0.0.0:%d --workers 2 config.wsgi:application", port)
	case FrameworkGo:
		return "/app/server"
	default:
		return ""
	}
}
