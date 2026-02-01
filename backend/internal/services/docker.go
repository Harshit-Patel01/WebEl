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
	FrameworkNodeExpress  FrameworkType = "node_express"
	FrameworkPythonFastAPI FrameworkType = "python_fastapi"
	FrameworkPythonDjango FrameworkType = "python_django"
	FrameworkGo           FrameworkType = "go"
	FrameworkNodeStatic   FrameworkType = "node_static"
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

	return FrameworkNodeStatic
}

func (d *DockerService) fileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), strings.ToLower(substr))
}

// GenerateDockerfile creates a Dockerfile for the given framework.
func (d *DockerService) GenerateDockerfile(framework FrameworkType, buildCmd, outputDir string) string {
	switch framework {
	case FrameworkNodeExpress:
		return d.dockerfileNodeExpress(buildCmd)
	case FrameworkNodeStatic:
		return d.dockerfileNodeStatic(buildCmd, outputDir)
	case FrameworkPythonFastAPI:
		return d.dockerfilePythonFastAPI()
	case FrameworkPythonDjango:
		return d.dockerfilePythonDjango()
	case FrameworkGo:
		return d.dockerfileGo()
	default:
		return d.dockerfileNodeStatic(buildCmd, outputDir)
	}
}

func (d *DockerService) dockerfileNodeExpress(buildCmd string) string {
	if buildCmd == "" {
		buildCmd = "npm run build"
	}
	return fmt.Sprintf(`FROM node:20-slim AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci --production=false
COPY . .
RUN %s || true

FROM node:20-slim
WORKDIR /app
COPY --from=builder /app /app
RUN npm prune --production
EXPOSE 3000
CMD ["node", "index.js"]
`, buildCmd)
}

func (d *DockerService) dockerfileNodeStatic(buildCmd, outputDir string) string {
	if buildCmd == "" {
		buildCmd = "npm run build"
	}
	if outputDir == "" {
		outputDir = "dist"
	}
	return fmt.Sprintf(`FROM node:20-slim AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN %s
# Build output will be in /app/%s
`, buildCmd, outputDir)
}

func (d *DockerService) dockerfilePythonFastAPI() string {
	return `FROM python:3.11-slim AS builder
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --target=/app/deps -r requirements.txt
COPY . .

FROM python:3.11-slim
WORKDIR /app
COPY --from=builder /app /app
ENV PYTHONPATH=/app/deps
EXPOSE 8000
CMD ["python", "-m", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
`
}

func (d *DockerService) dockerfilePythonDjango() string {
	return `FROM python:3.11-slim AS builder
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --target=/app/deps -r requirements.txt
COPY . .
RUN PYTHONPATH=/app/deps python manage.py collectstatic --noinput || true

FROM python:3.11-slim
WORKDIR /app
COPY --from=builder /app /app
ENV PYTHONPATH=/app/deps
EXPOSE 8000
CMD ["python", "-m", "gunicorn", "--bind", "0.0.0.0:8000", "--workers", "2", "config.wsgi:application"]
`
}

func (d *DockerService) dockerfileGo() string {
	return `FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /app/server .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server /app/server
RUN chmod +x /app/server
EXPOSE 8080
CMD ["/app/server"]
`
}

// BuildInDocker runs the project build inside a Docker container.
func (d *DockerService) BuildInDocker(ctx context.Context, projectID, jobID string, framework FrameworkType, buildCmd, outputDir string, envVars map[string]string) (*exec.ExecResult, error) {
	projectDir := filepath.Join(d.cfg.WorkspaceRoot, projectID)
	containerName := fmt.Sprintf("opendeploy-build-%s", projectID)

	// Generate Dockerfile
	dockerfile := d.GenerateDockerfile(framework, buildCmd, outputDir)
	dockerfilePath := filepath.Join(projectDir, "Dockerfile.opendeploy")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return nil, fmt.Errorf("writing Dockerfile: %w", err)
	}
	defer os.Remove(dockerfilePath)

	// Build Docker image
	imageName := fmt.Sprintf("opendeploy/%s:latest", projectID)
	buildArgs := []string{
		"build",
		"-f", dockerfilePath,
		"-t", imageName,
		"--memory", d.cfg.DockerMemoryLimit,
	}

	// Add env vars as build args
	for k, v := range envVars {
		buildArgs = append(buildArgs, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	buildArgs = append(buildArgs, projectDir)

	d.logger.Info("building docker image",
		zap.String("projectId", projectID),
		zap.String("framework", string(framework)),
		zap.String("image", imageName),
	)

	buildResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:    jobID + "-docker-build",
		JobType:  "docker_build",
		Command:  d.cfg.DockerBinary,
		Args:     buildArgs,
		MergeEnv: true,
		Timeout:  d.cfg.BuildTimeout,
	})
	if err != nil || !buildResult.Success {
		return buildResult, fmt.Errorf("docker build failed")
	}

	// For static frontend builds, copy output from container
	if framework == FrameworkNodeStatic {
		if outputDir == "" {
			outputDir = "dist"
		}
		return d.copyStaticOutput(ctx, imageName, containerName, projectID, outputDir, jobID)
	}

	// For backend apps, copy the full app from the container
	return d.copyBackendOutput(ctx, imageName, containerName, projectID, framework, jobID)
}

// copyStaticOutput extracts built static files from the Docker container.
func (d *DockerService) copyStaticOutput(ctx context.Context, imageName, containerName, projectID, outputDir, jobID string) (*exec.ExecResult, error) {
	destDir := filepath.Join(d.cfg.OutputRoot, "frontend", projectID)
	os.MkdirAll(destDir, 0755)

	// Create temp container
	createResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID + "-docker-create",
		JobType: "docker_create",
		Command: d.cfg.DockerBinary,
		Args:    []string{"create", "--name", containerName, imageName},
		Timeout: 30 * time.Second,
	})
	if err != nil || !createResult.Success {
		return createResult, fmt.Errorf("failed to create container for output copy")
	}

	// Copy build output
	srcPath := fmt.Sprintf("%s:/app/%s/.", containerName, outputDir)
	copyResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID + "-docker-cp",
		JobType: "docker_cp",
		Command: d.cfg.DockerBinary,
		Args:    []string{"cp", srcPath, destDir},
		Timeout: 2 * time.Minute,
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
		JobID:   jobID + "-docker-create",
		JobType: "docker_create",
		Command: d.cfg.DockerBinary,
		Args:    []string{"create", "--name", containerName, imageName},
		Timeout: 30 * time.Second,
	})
	if err != nil || !createResult.Success {
		return createResult, fmt.Errorf("failed to create container for output copy")
	}

	// Copy entire /app directory
	srcPath := fmt.Sprintf("%s:/app/.", containerName)
	copyResult, err := d.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID + "-docker-cp",
		JobType: "docker_cp",
		Command: d.cfg.DockerBinary,
		Args:    []string{"cp", srcPath, destDir},
		Timeout: 2 * time.Minute,
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
	return framework != FrameworkNodeStatic
}
