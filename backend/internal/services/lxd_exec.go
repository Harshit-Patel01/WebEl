package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

// ExecOptions holds options for executing commands in containers
type ExecOptions struct {
	WorkDir     string
	Environment map[string]string
	Timeout     time.Duration
}

// RunCommandInContainerWithOptions runs a command inside an LXD container with working directory and environment
func (l *LXDService) RunCommandInContainerWithOptions(ctx context.Context, containerID, command string, opts ExecOptions) (*exec.ExecResult, error) {
	l.logger.Info("running command in container with options",
		zap.String("containerId", containerID),
		zap.String("command", command),
		zap.String("workDir", opts.WorkDir),
	)

	// Build the full command with cd to workdir if specified
	fullCmd := command
	if opts.WorkDir != "" {
		fullCmd = fmt.Sprintf("cd %s && %s", opts.WorkDir, command)
	}

	// Add environment variables if specified
	if len(opts.Environment) > 0 {
		var envPrefix strings.Builder
		for k, v := range opts.Environment {
			envPrefix.WriteString(fmt.Sprintf("export %s='%s' && ", k, v))
		}
		fullCmd = envPrefix.String() + fullCmd
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_exec",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "/bin/sh", "-c", fullCmd},
		Timeout: timeout,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to run command: %w", err)
	}

	return result, nil
}

// ResolveWorkDir resolves the working directory inside the container
func ResolveWorkDir(repoRoot, userSubdir string) string {
	if userSubdir == "" || userSubdir == "." {
		return repoRoot
	}
	// Clean path to prevent traversal
	cleaned := strings.TrimPrefix(userSubdir, "/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	return fmt.Sprintf("%s/%s", repoRoot, cleaned)
}
