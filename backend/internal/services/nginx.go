package services

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/templates"
	"go.uber.org/zap"
)

type NginxSiteConfig struct {
	Domain       string `json:"domain"`
	FrontendPath string `json:"frontend_path"`
	ProxyEnabled bool   `json:"proxy_enabled"`
	ProxyPort    int    `json:"proxy_port"`
}

type NginxTestResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output"`
	Reloaded bool   `json:"reloaded"`
}

type AccessLogEntry struct {
	IP           string `json:"ip"`
	Timestamp    string `json:"timestamp"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Status       int    `json:"status"`
	ResponseSize int    `json:"response_size"`
	Duration     string `json:"duration"`
}

type NginxService struct {
	runner *exec.Runner
	cfg    config.NginxConfig
	logger *zap.Logger
}

func NewNginxService(runner *exec.Runner, cfg config.NginxConfig, logger *zap.Logger) *NginxService {
	return &NginxService{runner: runner, cfg: cfg, logger: logger}
}

func (n *NginxService) GenerateConfig(siteCfg NginxSiteConfig) string {
	return templates.RenderNginxConfig(templates.NginxTemplateData{
		Domain:       siteCfg.Domain,
		FrontendPath: siteCfg.FrontendPath,
		ProxyEnabled: siteCfg.ProxyEnabled,
		ProxyPort:    siteCfg.ProxyPort,
	})
}

func (n *NginxService) WriteConfig(siteName, configContent string) error {
	availablePath := filepath.Join(n.cfg.SitesAvailable, siteName)
	enabledPath := filepath.Join(n.cfg.SitesEnabled, siteName)

	// Write to temp file first, then rename (atomic)
	tmpPath := availablePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(configContent), 0644); err != nil {
		// Try with sudo
		result, sudoErr := n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_write_config",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/tee", availablePath},
			Timeout: 10 * time.Second,
		})
		if sudoErr != nil || !result.Success {
			return fmt.Errorf("writing nginx config: %w (sudo also failed)", err)
		}
		// Create symlink
		n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_symlink",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/ln", "-sf", availablePath, enabledPath},
			Timeout: 5 * time.Second,
		})
		return nil
	}

	// Atomic rename
	os.Rename(tmpPath, availablePath)

	// Create symlink to sites-enabled
	os.Remove(enabledPath) // remove old symlink if exists
	os.Symlink(availablePath, enabledPath)

	return nil
}

func (n *NginxService) TestConfig(ctx context.Context) (*NginxTestResult, error) {
	// Nginx is typically installed as a service. Let's find the correct binary path.
	nginxPaths := []string{"/usr/sbin/nginx", "/usr/bin/nginx", "/usr/local/bin/nginx", "/usr/local/sbin/nginx", "C:\\nginx\\nginx.exe"}
	var nginxCmd string
	for _, path := range nginxPaths {
		if fileExists(path) {
			nginxCmd = path
			break
		}
	}

	if nginxCmd == "" {
		nginxCmd = "nginx" // fallback to trusting PATH
	}

	result, err := n.runner.Run(ctx, exec.RunOpts{
		JobType: "nginx_test",
		Command: nginxCmd,
		Args:    []string{"-t"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	output := ""
	for _, line := range result.Lines {
		output += line.Text + "\n"
	}

	return &NginxTestResult{
		Success: result.Success,
		Output:  strings.TrimSpace(output),
	}, nil
}

func (n *NginxService) Reload(ctx context.Context) error {
	result, err := n.runner.Run(ctx, exec.RunOpts{
		JobType: "nginx_reload",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "reload", "nginx"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		// Fallback for non-systemd environments (like testing on Windows)
		if runtime.GOOS == "windows" {
			n.logger.Warn("simulating nginx reload on windows")
			return nil
		}
		return err
	}
	if !result.Success {
		// Get status for error details
		statusResult, _ := n.runner.Run(ctx, exec.RunOpts{
			JobType: "nginx_status",
			Command: "/usr/bin/systemctl",
			Args:    []string{"status", "nginx"},
			Timeout: 5 * time.Second,
		})
		errMsg := "nginx reload failed"
		if statusResult != nil {
			for _, line := range statusResult.Lines {
				errMsg += "\n" + line.Text
			}
		}
		return fmt.Errorf(errMsg)
	}
	return nil
}

func (n *NginxService) ValidateUpstream(ctx context.Context, port int) (bool, error) {
	result, err := n.runner.Run(ctx, exec.RunOpts{
		JobType: "port_check",
		Command: "/usr/bin/ss",
		Args:    []string{"-tlnp"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return false, err
	}

	portStr := fmt.Sprintf(":%d", port)
	for _, line := range result.Lines {
		if strings.Contains(line.Text, portStr) {
			return true, nil
		}
	}
	return false, nil
}

var accessLogRegex = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "(\S+) (\S+) [^"]*" (\d+) (\d+)`)

func (n *NginxService) GetAccessLog(lines int) ([]AccessLogEntry, error) {
	logPath := filepath.Join(n.cfg.LogPath, "access.log")
	file, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("opening access log: %w", err)
	}
	defer file.Close()

	// Read all lines, then take the last N
	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	start := 0
	if len(allLines) > lines {
		start = len(allLines) - lines
	}

	var entries []AccessLogEntry
	for _, line := range allLines[start:] {
		matches := accessLogRegex.FindStringSubmatch(line)
		if len(matches) < 7 {
			continue
		}
		status, _ := strconv.Atoi(matches[5])
		size, _ := strconv.Atoi(matches[6])
		entries = append(entries, AccessLogEntry{
			IP:           matches[1],
			Timestamp:    matches[2],
			Method:       matches[3],
			Path:         matches[4],
			Status:       status,
			ResponseSize: size,
		})
	}
	return entries, nil
}

// NginxFileInfo represents a config file in sites-available.
type NginxFileInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Size    int64  `json:"size"`
}

// isValidConfigName checks that a config filename is safe (no path traversal).
func isValidConfigName(name string) bool {
	if name == "" {
		return false
	}
	// Must not contain path separators or directory traversal
	if strings.Contains(name, "/") || strings.Contains(name, "\\") ||
		strings.Contains(name, "..") || name == "." {
		return false
	}
	// Only allow alphanumeric, hyphens, underscores, dots
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// isValidDomain validates that a string looks like a valid hostname.
var nginxDomainRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)

func IsValidDomain(domain string) bool {
	if len(domain) > 253 {
		return false
	}
	return nginxDomainRegex.MatchString(domain)
}

// ListConfigFiles returns all config files in sites-available and whether they're enabled.
func (n *NginxService) ListConfigFiles() ([]NginxFileInfo, error) {
	entries, err := os.ReadDir(n.cfg.SitesAvailable)
	if err != nil {
		return nil, fmt.Errorf("reading sites-available: %w", err)
	}

	var files []NginxFileInfo
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Check if enabled (symlink exists in sites-enabled)
		enabledPath := filepath.Join(n.cfg.SitesEnabled, entry.Name())
		_, enabledErr := os.Lstat(enabledPath)
		enabled := enabledErr == nil

		files = append(files, NginxFileInfo{
			Name:    entry.Name(),
			Enabled: enabled,
			Size:    info.Size(),
		})
	}
	return files, nil
}

// ReadConfigFile reads the contents of a config file in sites-available.
func (n *NginxService) ReadConfigFile(name string) (string, error) {
	if !isValidConfigName(name) {
		return "", fmt.Errorf("invalid config file name")
	}
	path := filepath.Join(n.cfg.SitesAvailable, name)
	// Verify the resolved path is still within sites-available
	absPath, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(absPath, n.cfg.SitesAvailable) {
		return "", fmt.Errorf("invalid config file path")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading config file: %w", err)
	}
	return string(data), nil
}

// WriteConfigFile writes content to a config file atomically.
func (n *NginxService) WriteConfigFile(name, content string) error {
	if !isValidConfigName(name) {
		return fmt.Errorf("invalid config file name")
	}
	availablePath := filepath.Join(n.cfg.SitesAvailable, name)

	// Write to temp file first, then rename (atomic)
	tmpPath := availablePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		// Try with sudo
		_, sudoErr := n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_write_config",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/tee", tmpPath},
			Timeout: 10 * time.Second,
		})
		if sudoErr != nil {
			return fmt.Errorf("writing config file: %w", err)
		}
	}

	// Atomic rename
	if err := os.Rename(tmpPath, availablePath); err != nil {
		n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_rename",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/mv", tmpPath, availablePath},
			Timeout: 5 * time.Second,
		})
	}

	return nil
}

// EnableSite creates a symlink in sites-enabled pointing to sites-available.
func (n *NginxService) EnableSite(name string) error {
	if !isValidConfigName(name) {
		return fmt.Errorf("invalid config file name")
	}
	availablePath := filepath.Join(n.cfg.SitesAvailable, name)
	enabledPath := filepath.Join(n.cfg.SitesEnabled, name)

	// Verify the source file exists
	if _, err := os.Stat(availablePath); err != nil {
		return fmt.Errorf("config file does not exist: %s", name)
	}

	// Remove old symlink if exists
	os.Remove(enabledPath)

	if err := os.Symlink(availablePath, enabledPath); err != nil {
		// Try with sudo
		_, sudoErr := n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_enable",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/ln", "-sf", availablePath, enabledPath},
			Timeout: 5 * time.Second,
		})
		if sudoErr != nil {
			return fmt.Errorf("enabling site: %w", err)
		}
	}
	return nil
}

// DisableSite removes the symlink from sites-enabled.
func (n *NginxService) DisableSite(name string) error {
	if !isValidConfigName(name) {
		return fmt.Errorf("invalid config file name")
	}
	enabledPath := filepath.Join(n.cfg.SitesEnabled, name)

	if err := os.Remove(enabledPath); err != nil {
		// Try with sudo
		_, sudoErr := n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_disable",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/rm", "-f", enabledPath},
			Timeout: 5 * time.Second,
		})
		if sudoErr != nil {
			return fmt.Errorf("disabling site: %w", err)
		}
	}
	return nil
}

// DeleteConfigFile removes a config from both sites-available and sites-enabled.
func (n *NginxService) DeleteConfigFile(name string) error {
	if !isValidConfigName(name) {
		return fmt.Errorf("invalid config file name")
	}
	// Disable first
	n.DisableSite(name)

	availablePath := filepath.Join(n.cfg.SitesAvailable, name)
	if err := os.Remove(availablePath); err != nil {
		n.runner.Run(context.Background(), exec.RunOpts{
			JobType: "nginx_delete",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/rm", "-f", availablePath},
			Timeout: 5 * time.Second,
		})
	}
	return nil
}
