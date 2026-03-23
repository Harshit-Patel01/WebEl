package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

// ImageBuilder manages pre-built LXD images for faster deployments
type ImageBuilder struct {
	runner *exec.Runner
	logger *zap.Logger
}

// ImageType represents different pre-built image types
type ImageType string

const (
	ImageFrontend ImageType = "frontend"
	ImageNodeJS   ImageType = "nodejs"
	ImagePython   ImageType = "python"
	ImageGo       ImageType = "go"
	ImageStatic   ImageType = "static"
)

func NewImageBuilder(runner *exec.Runner, logger *zap.Logger) *ImageBuilder {
	return &ImageBuilder{
		runner: runner,
		logger: logger,
	}
}

// EnsureImage ensures a pre-built image exists for the given type
func (ib *ImageBuilder) EnsureImage(ctx context.Context, imageType ImageType) error {
	imageName := fmt.Sprintf("opendeploy-%s", imageType)

	// Check if image already exists
	checkResult, _ := ib.runner.Run(ctx, exec.RunOpts{
		JobType: "check_image",
		Command: "lxc",
		Args:    []string{"image", "list", "--format", "csv", "--columns", "f"},
		Timeout: 30 * time.Second,
	})

	if checkResult != nil {
		for _, line := range checkResult.Lines {
			if strings.Contains(line.Text, imageName) {
				ib.logger.Info("pre-built image exists", zap.String("image", imageName))
				return nil
			}
		}
	}

	ib.logger.Info("building pre-configured image", zap.String("image", imageName))

	// Create temporary container for building image
	tempContainer := fmt.Sprintf("temp-build-%s-%d", imageType, time.Now().Unix())

	// Initialize container
	_, err := ib.runner.Run(ctx, exec.RunOpts{
		JobType: "init_temp_container",
		Command: "lxc",
		Args:    []string{"init", "images:alpine/3.23", tempContainer},
		Timeout: 60 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to init temp container: %w", err)
	}

	// Start container
	_, err = ib.runner.Run(ctx, exec.RunOpts{
		JobType: "start_temp_container",
		Command: "lxc",
		Args:    []string{"start", tempContainer},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to start temp container: %w", err)
	}

	// Wait for network
	time.Sleep(5 * time.Second)

	// Run setup script based on image type
	setupScript := ib.getSetupScript(imageType)
	_, err = ib.runner.Run(ctx, exec.RunOpts{
		JobType: "run_setup_script",
		Command: "lxc",
		Args:    []string{"exec", tempContainer, "--", "/bin/sh", "-c", setupScript},
		Timeout: 15 * time.Minute,
	})
	if err != nil {
		ib.cleanupTempContainer(ctx, tempContainer)
		return fmt.Errorf("failed to run setup script: %w", err)
	}

	// Stop container
	_, err = ib.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_temp_container",
		Command: "lxc",
		Args:    []string{"stop", tempContainer},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		ib.cleanupTempContainer(ctx, tempContainer)
		return fmt.Errorf("failed to stop temp container: %w", err)
	}

	// Publish as image
	_, err = ib.runner.Run(ctx, exec.RunOpts{
		JobType: "publish_image",
		Command: "lxc",
		Args:    []string{"publish", tempContainer, imageName, "--alias", imageName},
		Timeout: 2 * time.Minute,
	})
	if err != nil {
		ib.cleanupTempContainer(ctx, tempContainer)
		return fmt.Errorf("failed to publish image: %w", err)
	}

	// Cleanup temp container
	ib.cleanupTempContainer(ctx, tempContainer)

	ib.logger.Info("successfully built pre-configured image", zap.String("image", imageName))
	return nil
}

// getSetupScript returns the setup script for each image type
func (ib *ImageBuilder) getSetupScript(imageType ImageType) string {
	baseScript := `
# Configure DNS resolvers directly
cat > /etc/resolv.conf << 'EOF'
nameserver 8.8.8.8
nameserver 1.1.1.1
nameserver 208.67.222.222
options timeout:1 attempts:2
EOF
`

	switch imageType {
	case ImageFrontend:
		return baseScript + `
apk update --no-cache
apk add --no-cache git bash ca-certificates supervisor nginx iproute2 nodejs npm
mkdir -p /etc/nginx/http.d /run/nginx
npm config set registry https://registry.npmjs.org/
npm config set fetch-retries 5
npm config set fetch-retry-mintimeout 20000
npm config set fetch-retry-maxtimeout 120000
mkdir -p /var/log/supervisor /etc/supervisor.d
node --version && npm --version && echo "Frontend image ready"
`
	case ImageNodeJS:
		return baseScript + `
apk update --no-cache
apk add --no-cache git bash ca-certificates supervisor iproute2 nodejs npm
npm config set registry https://registry.npmjs.org/
npm config set fetch-retries 5
npm config set fetch-retry-mintimeout 20000
npm config set fetch-retry-maxtimeout 120000
mkdir -p /var/log/supervisor /etc/supervisor.d
node --version && npm --version && echo "Node.js image ready"
`
	case ImagePython:
		return baseScript + `
apk update --no-cache
apk add --no-cache python3 py3-pip git bash ca-certificates supervisor
mkdir -p /var/log/supervisor /etc/supervisor.d
python3 --version && echo "Python image ready"
`
	case ImageGo:
		return baseScript + `
apk update --no-cache
apk add --no-cache go git bash ca-certificates supervisor
mkdir -p /var/log/supervisor /etc/supervisor.d
go version && echo "Go image ready"
`
	case ImageStatic:
		return baseScript + `
apk update --no-cache
apk add --no-cache git bash ca-certificates nginx supervisor
mkdir -p /var/log/supervisor /etc/supervisor.d
echo "Static image ready"
`
	default:
		return baseScript + `
apk update --no-cache
apk add --no-cache git bash ca-certificates supervisor
mkdir -p /var/log/supervisor /etc/supervisor.d
echo "Generic image ready"
`
	}
}

// cleanupTempContainer cleans up temporary containers
func (ib *ImageBuilder) cleanupTempContainer(ctx context.Context, containerName string) {
	ib.runner.Run(ctx, exec.RunOpts{
		JobType: "cleanup_temp",
		Command: "lxc",
		Args:    []string{"delete", "-f", containerName},
		Timeout: 30 * time.Second,
	})
}

// GetImageName returns the image name for a given image type
func (ib *ImageBuilder) GetImageName(imageType ImageType) string {
	return fmt.Sprintf("opendeploy-%s", imageType)
}

// ListImages lists all available pre-built images
func (ib *ImageBuilder) ListImages(ctx context.Context) ([]string, error) {
	result, err := ib.runner.Run(ctx, exec.RunOpts{
		JobType: "list_images",
		Command: "lxc",
		Args:    []string{"image", "list", "--format", "csv", "--columns", "f"},
		Timeout: 30 * time.Second,
	})

	if err != nil {
		return nil, err
	}

	var images []string
	for _, line := range result.Lines {
		if strings.Contains(line.Text, "opendeploy-") {
			images = append(images, line.Text)
		}
	}

	return images, nil
}

// DeleteImage deletes a pre-built image
func (ib *ImageBuilder) DeleteImage(ctx context.Context, imageName string) error {
	_, err := ib.runner.Run(ctx, exec.RunOpts{
		JobType: "delete_image",
		Command: "lxc",
		Args:    []string{"image", "delete", imageName},
		Timeout: 30 * time.Second,
	})
	return err
}
