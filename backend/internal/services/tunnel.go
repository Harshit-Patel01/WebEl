package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/config"
	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type TunnelService struct {
	runner *exec.Runner
	cfg    config.CloudflaredConfig
	db     *state.DB
	logger *zap.Logger
}

type TunnelSetupRequest struct {
	APIToken   string `json:"api_token"`
	AccountID  string `json:"account_id"`
	ZoneID     string `json:"zone_id"`
	Subdomain  string `json:"subdomain"`
	Domain     string `json:"domain"` // Full domain from zone
	TunnelName string `json:"tunnel_name"`
}

type TunnelInfo struct {
	TunnelID   string `json:"tunnel_id"`
	TunnelName string `json:"tunnel_name"`
	Domain     string `json:"domain"`
	Status     string `json:"status"`
	AccountID  string `json:"account_id"`
	ZoneID     string `json:"zone_id"`
}

func NewTunnelService(runner *exec.Runner, cfg config.CloudflaredConfig, db *state.DB, logger *zap.Logger) *TunnelService {
	return &TunnelService{
		runner: runner,
		cfg:    cfg,
		db:     db,
		logger: logger,
	}
}

// ensureCloudflaredInstalled checks if cloudflared is installed and installs it if missing
func (t *TunnelService) ensureCloudflaredInstalled(ctx context.Context) error {
	// Check if cloudflared is already installed
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "check_cloudflared",
		Command: "/usr/local/bin/cloudflared",
		Args:    []string{"--version"},
		Timeout: 5 * time.Second,
	})

	if err == nil && result.Success {
		t.logger.Info("cloudflared is already installed")
		return nil
	}

	// cloudflared not found, install it
	t.logger.Info("cloudflared not found, installing...")

	// Download and install cloudflared
	// For Linux AMD64
	installScript := `
		cd /tmp && \
		wget -q https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 && \
		sudo mv cloudflared-linux-amd64 /usr/local/bin/cloudflared && \
		sudo chmod +x /usr/local/bin/cloudflared
	`

	result, err = t.runner.Run(ctx, exec.RunOpts{
		JobType: "install_cloudflared",
		Command: "/bin/bash",
		Args:    []string{"-c", installScript},
		Timeout: 60 * time.Second,
	})

	if err != nil {
		return fmt.Errorf("failed to install cloudflared: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("cloudflared installation failed: %s", result.Error)
	}

	t.logger.Info("cloudflared installed successfully")
	return nil
}

// SetupTunnel implements the complete API-based tunnel setup flow
func (t *TunnelService) SetupTunnel(ctx context.Context, req TunnelSetupRequest) (*TunnelInfo, error) {
	cfAPI := NewCloudflareAPI(req.APIToken)

	// Step 0: Ensure cloudflared is installed
	if err := t.ensureCloudflaredInstalled(ctx); err != nil {
		return nil, fmt.Errorf("cloudflared installation check failed: %w", err)
	}

	// Step 1: Verify token
	t.logger.Info("verifying Cloudflare API token")
	tokenResult, err := cfAPI.VerifyToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}
	if tokenResult.Status != "active" {
		return nil, fmt.Errorf("token is not active: %s", tokenResult.Status)
	}

	// Step 2: Create tunnel via API
	t.logger.Info("creating tunnel via Cloudflare API", zap.String("name", req.TunnelName))
	tunnelResp, err := cfAPI.CreateTunnel(ctx, req.AccountID, req.TunnelName)
	if err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %w", err)
	}

	t.logger.Info("tunnel created", zap.String("tunnelID", tunnelResp.ID), zap.String("name", tunnelResp.Name))

	// Step 3: Create DNS record
	fullDomain := fmt.Sprintf("%s.%s", req.Subdomain, req.Domain)
	t.logger.Info("creating DNS record", zap.String("domain", fullDomain))

	dnsRecord := DNSRecordCreateRequest{
		Type:    "CNAME",
		Name:    req.Subdomain,
		Content: fmt.Sprintf("%s.cfargotunnel.com", tunnelResp.ID),
		Proxied: true,
		TTL:     1,
	}

	dnsResp, err := cfAPI.CreateDNSRecord(ctx, req.ZoneID, dnsRecord)
	if err != nil {
		// If DNS creation fails, try to clean up the tunnel
		_ = cfAPI.DeleteTunnel(ctx, req.AccountID, tunnelResp.ID)
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}

	// Step 4: Save to database (before writing config)
	tunnelConfig := &state.TunnelConfig{
		TunnelID:    tunnelResp.ID,
		TunnelName:  tunnelResp.Name,
		TunnelToken: tunnelResp.Token, // TODO: Encrypt before storing
		AccountID:   req.AccountID,
		ZoneID:      req.ZoneID,
		Domain:      fullDomain,
		Status:      "active",
	}

	if err := t.db.SaveTunnelConfig(tunnelConfig); err != nil {
		t.logger.Error("failed to save tunnel config to database", zap.Error(err))
		return nil, fmt.Errorf("failed to save tunnel config: %w", err)
	}

	// Step 5: Create initial route in database
	initialRoute := &state.TunnelRoute{
		TunnelID:    tunnelResp.ID,
		Hostname:    fullDomain,
		ZoneID:      req.ZoneID,
		DNSRecordID: dnsResp.ID,
		LocalScheme: "http",
		LocalPort:   80,
		SortOrder:   0,
	}

	if err := t.db.CreateTunnelRoute(initialRoute); err != nil {
		t.logger.Error("failed to save initial route", zap.Error(err))
		return nil, fmt.Errorf("failed to save initial route: %w", err)
	}

	// Step 6: Write cloudflared config from database
	t.logger.Info("writing cloudflared config")
	if err := t.writeConfig(tunnelResp.ID, fullDomain); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Step 7: Create systemd service
	t.logger.Info("creating systemd service")
	if err := t.createSystemdService(tunnelResp.Token); err != nil {
		return nil, fmt.Errorf("failed to create systemd service: %w", err)
	}

	// Step 8: Start the service
	t.logger.Info("starting cloudflared service")
	if err := t.startService(ctx); err != nil {
		return nil, fmt.Errorf("failed to start service: %w", err)
	}

	// Step 9: Verify tunnel is active
	t.logger.Info("verifying tunnel status")
	time.Sleep(3 * time.Second)

	if err := t.verifyService(ctx); err != nil {
		t.logger.Warn("service verification failed", zap.Error(err))
		// Don't fail the setup, just log the warning
	}

	t.logger.Info("tunnel setup complete", zap.String("domain", fullDomain))

	return &TunnelInfo{
		TunnelID:   tunnelResp.ID,
		TunnelName: tunnelResp.Name,
		Domain:     fullDomain,
		Status:     "active",
		AccountID:  req.AccountID,
		ZoneID:     req.ZoneID,
	}, nil
}

// GetStatus returns the current tunnel status
func (t *TunnelService) GetStatus(ctx context.Context) (*TunnelInfo, error) {
	// Get from database
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return &TunnelInfo{Status: "not_configured"}, nil
	}

	// Check systemd service status
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_status",
		Command: "/usr/bin/systemctl",
		Args:    []string{"is-active", "opendeploy-cloudflared"},
		Timeout: 5 * time.Second,
	})

	status := "inactive"
	if err == nil && result.Success {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				status = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	// Get active routes
	routes, err := t.db.ListTunnelRoutes(config.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get routes: %w", err)
	}

	// Use the first route's domain if available, otherwise use stored domain
	domain := config.Domain
	if len(routes) > 0 {
		domain = routes[0].Hostname
	}

	return &TunnelInfo{
		TunnelID:   config.TunnelID,
		TunnelName: config.TunnelName,
		Domain:     domain,
		Status:     status,
		AccountID:  config.AccountID,
		ZoneID:     config.ZoneID,
	}, nil
}

// VerifyAndCleanupTunnel verifies if the tunnel still exists in Cloudflare and cleans up if not
func (t *TunnelService) VerifyAndCleanupTunnel(ctx context.Context, apiKey string) error {
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return nil // No tunnel configured, nothing to clean up
	}

	// If we have an API key, verify the tunnel still exists in Cloudflare
	if apiKey != "" {
		cfAPI := NewCloudflareAPI(apiKey)
		_, err := cfAPI.GetTunnel(ctx, config.AccountID, config.TunnelID)
		if err != nil {
			// Tunnel doesn't exist in Cloudflare anymore, clean up local config
			t.logger.Warn("tunnel no longer exists in Cloudflare, cleaning up local config",
				zap.String("tunnelID", config.TunnelID),
				zap.Error(err))

			// Stop local service if running
			_ = t.stopService(ctx)
			_ = t.disableService(ctx)

			// Delete local config
			_ = t.db.DeleteTunnelConfig(config.ID)

			// Delete routes
			routes, _ := t.db.ListTunnelRoutes(config.TunnelID)
			for _, route := range routes {
				_ = t.db.DeleteTunnelRoute(route.ID)
			}

			return fmt.Errorf("tunnel was deleted from Cloudflare, local config has been cleaned up")
		}
	}

	return nil
}

// RestartTunnel restarts the cloudflared service
func (t *TunnelService) RestartTunnel(ctx context.Context) error {
	// Get tunnel config from database
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Check if service file exists
	_, err = os.Stat("/etc/systemd/system/opendeploy-cloudflared.service")
	if os.IsNotExist(err) {
		// Service doesn't exist, recreate it
		t.logger.Info("systemd service not found, recreating it")

		// Recreate the systemd service
		if err := t.createSystemdService(config.TunnelToken); err != nil {
			return fmt.Errorf("failed to recreate systemd service: %w", err)
		}

		// Recreate the config file
		if err := t.writeConfig(config.TunnelID, config.Domain); err != nil {
			return fmt.Errorf("failed to recreate config: %w", err)
		}

		// Start the service
		if err := t.startService(ctx); err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}

		return nil
	}

	// Service exists, just restart it
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_restart",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "restart", "opendeploy-cloudflared"},
		Timeout: 15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("restarting cloudflared service: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("cloudflared service failed to restart: %s", result.Error)
	}

	return nil
}

// StopLocalTunnel stops the local cloudflared service but keeps the tunnel in Cloudflare
func (t *TunnelService) StopLocalTunnel(ctx context.Context) error {
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Stop and disable service
	t.logger.Info("stopping cloudflared service")
	_ = t.stopService(ctx)
	_ = t.disableService(ctx)

	// Delete systemd unit file
	t.logger.Info("removing systemd unit file")
	_ = os.Remove("/etc/systemd/system/opendeploy-cloudflared.service")

	// Reload systemd
	_, _ = t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_daemon_reload",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "daemon-reload"},
		Timeout: 10 * time.Second,
	})

	// Delete config file
	t.logger.Info("removing config file")
	_ = os.Remove(t.cfg.ConfigPath)

	// Update tunnel status in database to inactive
	config.Status = "inactive"
	if err := t.db.UpdateTunnelConfig(config); err != nil {
		t.logger.Error("failed to update tunnel config status", zap.Error(err))
	}

	t.logger.Info("local tunnel stopped", zap.String("tunnelID", config.TunnelID))
	return nil
}

// DeleteTunnel removes the tunnel completely
func (t *TunnelService) DeleteTunnel(ctx context.Context, apiKey string) error {
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Only proceed with Cloudflare API calls if we have an API key
	if apiKey != "" {
		cfAPI := NewCloudflareAPI(apiKey)

		// Stop and disable service
		t.logger.Info("stopping cloudflared service")
		_ = t.stopService(ctx)
		_ = t.disableService(ctx)

		// Delete systemd unit file
		t.logger.Info("removing systemd unit file")
		_ = os.Remove("/etc/systemd/system/opendeploy-cloudflared.service")

		// Reload systemd
		_, _ = t.runner.Run(ctx, exec.RunOpts{
			JobType: "tunnel_daemon_reload",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/systemctl", "daemon-reload"},
			Timeout: 10 * time.Second,
		})

		// Delete DNS record
		t.logger.Info("deleting DNS record")
		records, err := cfAPI.ListDNSRecords(ctx, config.ZoneID, config.Domain)
		if err == nil {
			for _, record := range records {
				if record.Name == config.Domain && record.Type == "CNAME" {
					_ = cfAPI.DeleteDNSRecord(ctx, config.ZoneID, record.ID)
				}
			}
		}

		// Delete tunnel from Cloudflare
		t.logger.Info("deleting tunnel from Cloudflare")
		if err := cfAPI.DeleteTunnel(ctx, config.AccountID, config.TunnelID); err != nil {
			t.logger.Warn("failed to delete tunnel from Cloudflare", zap.Error(err))
			// Continue with local cleanup
		}
	} else {
		// No API token, just clean up locally
		t.logger.Warn("no API token available, performing local cleanup only")
		_ = t.stopService(ctx)
		_ = t.disableService(ctx)
		_ = os.Remove("/etc/systemd/system/opendeploy-cloudflared.service")
	}

	// Delete config file
	t.logger.Info("removing config file")
	_ = os.Remove(t.cfg.ConfigPath)

	// Delete from database
	if err := t.db.DeleteTunnelConfig(config.ID); err != nil {
		t.logger.Error("failed to delete tunnel config from database", zap.Error(err))
	}

	t.logger.Info("tunnel deleted successfully")
	return nil
}

// writeConfig writes the cloudflared config.yml from database routes
func (t *TunnelService) writeConfig(tunnelID, domain string) error {
	// Get routes from database
	routes, err := t.db.ListTunnelRoutes(tunnelID)
	if err != nil {
		return fmt.Errorf("failed to get tunnel routes: %w", err)
	}

	// Build config content
	var configBuilder strings.Builder
	configBuilder.WriteString(fmt.Sprintf("tunnel: %s\n\ningress:\n", tunnelID))

	// Add routes ordered by sort_order
	for _, route := range routes {
		configBuilder.WriteString(fmt.Sprintf("  - hostname: %s\n", route.Hostname))
		if route.PathPrefix != "" {
			configBuilder.WriteString(fmt.Sprintf("    path: %s\n", route.PathPrefix))
		}
		configBuilder.WriteString(fmt.Sprintf("    service: %s://localhost:%d\n", route.LocalScheme, route.LocalPort))
	}

	// Always add catch-all rule at the end
	configBuilder.WriteString("  - service: http_status:404\n")

	configContent := configBuilder.String()

	// Ensure directory exists
	configDir := "/etc/cloudflared"
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Try writing directly first
	err = os.WriteFile(t.cfg.ConfigPath, []byte(configContent), 0644)
	if err != nil {
		t.logger.Warn("direct write failed, trying sudo tee", zap.Error(err))

		// Fall back to sudo tee
		result, err := t.runner.RunWithStdin(context.Background(), exec.RunOpts{
			JobType: "tunnel_config",
			Command: "/usr/bin/sudo",
			Args:    []string{"/usr/bin/tee", t.cfg.ConfigPath},
			Timeout: 10 * time.Second,
		}, strings.NewReader(configContent))

		if err != nil {
			return fmt.Errorf("writing config via sudo tee: %w", err)
		}
		if !result.Success {
			return fmt.Errorf("sudo tee failed: %s", result.Error)
		}
	}

	return nil
}

// regenerateConfig regenerates config.yml from database and reloads cloudflared
func (t *TunnelService) regenerateConfig(ctx context.Context, tunnelID string) error {
	// Get tunnel config
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Regenerate config from database
	if err := t.writeConfig(tunnelID, config.Domain); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Reload cloudflared
	if err := t.RestartTunnel(ctx); err != nil {
		return fmt.Errorf("failed to reload cloudflared: %w", err)
	}

	return nil
}

// AddRoute adds a new ingress route
func (t *TunnelService) AddRoute(ctx context.Context, apiKey string, route *state.TunnelRoute) error {
	// Get tunnel config
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Validate API key is provided
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	// Validate port
	if route.LocalPort < 1 || route.LocalPort > 65535 {
		return fmt.Errorf("invalid port: %d", route.LocalPort)
	}
	if route.LocalPort == 3000 {
		return fmt.Errorf("port 3000 is reserved for the OpenDeploy dashboard and cannot be exposed")
	}

	// Set tunnel ID
	route.TunnelID = config.TunnelID

	// Get current max sort order
	routes, err := t.db.ListTunnelRoutes(config.TunnelID)
	if err != nil {
		return fmt.Errorf("failed to get routes: %w", err)
	}
	maxOrder := 0
	for _, r := range routes {
		if r.SortOrder > maxOrder {
			maxOrder = r.SortOrder
		}
	}
	route.SortOrder = maxOrder + 1

	// Create DNS record
	cfAPI := NewCloudflareAPI(apiKey)
	subdomain := strings.Split(route.Hostname, ".")[0]

	dnsRecord := DNSRecordCreateRequest{
		Type:    "CNAME",
		Name:    subdomain,
		Content: fmt.Sprintf("%s.cfargotunnel.com", config.TunnelID),
		Proxied: true,
		TTL:     1,
	}

	dnsResp, err := cfAPI.CreateDNSRecord(ctx, route.ZoneID, dnsRecord)
	if err != nil {
		return fmt.Errorf("failed to create DNS record: %w", err)
	}
	route.DNSRecordID = dnsResp.ID

	// Save to database
	if err := t.db.CreateTunnelRoute(route); err != nil {
		// Try to clean up DNS record
		_ = cfAPI.DeleteDNSRecord(ctx, route.ZoneID, route.DNSRecordID)
		return fmt.Errorf("failed to save route: %w", err)
	}

	// Regenerate config and reload
	if err := t.regenerateConfig(ctx, config.TunnelID); err != nil {
		return fmt.Errorf("failed to regenerate config: %w", err)
	}

	t.logger.Info("route added", zap.String("hostname", route.Hostname), zap.Int("port", route.LocalPort))
	return nil
}

// UpdateRoute updates an existing route
func (t *TunnelService) UpdateRoute(ctx context.Context, routeID string, updates map[string]interface{}) error {
	// Get existing route
	route, err := t.db.GetTunnelRoute(routeID)
	if err != nil {
		return fmt.Errorf("failed to get route: %w", err)
	}
	if route == nil {
		return fmt.Errorf("route not found")
	}

	// Apply updates
	if scheme, ok := updates["local_scheme"].(string); ok {
		route.LocalScheme = scheme
	}
	if port, ok := updates["local_port"].(int); ok {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid port: %d", port)
		}
		if port == 3000 {
			return fmt.Errorf("port 3000 is reserved for the OpenDeploy dashboard")
		}
		route.LocalPort = port
	}
	if pathPrefix, ok := updates["path_prefix"].(string); ok {
		route.PathPrefix = pathPrefix
	}

	// Update in database
	if err := t.db.UpdateTunnelRoute(route); err != nil {
		return fmt.Errorf("failed to update route: %w", err)
	}

	// Regenerate config and reload
	if err := t.regenerateConfig(ctx, route.TunnelID); err != nil {
		return fmt.Errorf("failed to regenerate config: %w", err)
	}

	t.logger.Info("route updated", zap.String("routeID", routeID))
	return nil
}

// CheckPortListening checks if a local port is accepting connections
func (t *TunnelService) CheckPortListening(ctx context.Context, port int) (bool, error) {
	if port < 1 || port > 65535 {
		return false, fmt.Errorf("invalid port: %d", port)
	}

	// Use ss command to check if port is listening
	result, err := t.runner.Run(ctx, exec.RunOpts{
		Command: "ss",
		Args:    []string{"-tlnp", fmt.Sprintf("sport = :%d", port)},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		// ss command failed
		return false, fmt.Errorf("failed to check port: %w", err)
	}

	// If output has more than just the header line, something is listening
	var output strings.Builder
	for _, line := range result.Lines {
		output.WriteString(line.Text)
		output.WriteString("\n")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	return len(lines) > 1, nil
}

// VerifyDNSRecord checks if a DNS record exists and points to the correct tunnel
func (t *TunnelService) VerifyDNSRecord(ctx context.Context, apiKey string, routeID string) (bool, error) {
	// Get the route
	route, err := t.db.GetTunnelRoute(routeID)
	if err != nil {
		return false, fmt.Errorf("failed to get route: %w", err)
	}
	if route == nil {
		return false, fmt.Errorf("route not found")
	}

	// Get tunnel config to get tunnel ID
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return false, fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return false, fmt.Errorf("tunnel not configured")
	}

	// If no DNS record ID stored, it doesn't exist
	if route.DNSRecordID == "" {
		return false, nil
	}

	// Check if DNS record exists in Cloudflare
	cfAPI := NewCloudflareAPI(apiKey)
	record, err := cfAPI.GetDNSRecord(ctx, route.ZoneID, route.DNSRecordID)
	if err != nil {
		// Record doesn't exist or API error
		return false, nil
	}

	// Verify it points to the correct tunnel
	expectedContent := fmt.Sprintf("%s.cfargotunnel.com", config.TunnelID)
	return record.Content == expectedContent && record.Type == "CNAME", nil
}

// DetectConfigDrift compares local config.yml with Cloudflare's stored configuration
func (t *TunnelService) DetectConfigDrift(ctx context.Context, apiKey string) (*ConfigDriftResult, error) {
	// Get tunnel config
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return nil, fmt.Errorf("tunnel not configured")
	}

	// Get local routes from database
	localRoutes, err := t.db.ListTunnelRoutes(config.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get local routes: %w", err)
	}

	// Get Cloudflare configuration
	cfAPI := NewCloudflareAPI(apiKey)
	cfConfig, err := cfAPI.GetTunnelConfiguration(ctx, config.AccountID, config.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get Cloudflare configuration: %w", err)
	}

	// Compare configurations
	result := &ConfigDriftResult{
		HasDrift:      false,
		LocalRoutes:   len(localRoutes),
		CloudflareRoutes: len(cfConfig.Config.Ingress) - 1, // Subtract catch-all rule
		MissingInCloudflare: []string{},
		ExtraInCloudflare:   []string{},
	}

	// Build maps for comparison
	localMap := make(map[string]*state.TunnelRoute)
	for i := range localRoutes {
		localMap[localRoutes[i].Hostname] = &localRoutes[i]
	}

	cfMap := make(map[string]IngressRule)
	for _, rule := range cfConfig.Config.Ingress {
		if rule.Hostname != "" { // Skip catch-all rule
			cfMap[rule.Hostname] = rule
		}
	}

	// Check for routes in local but not in Cloudflare
	for hostname := range localMap {
		if _, exists := cfMap[hostname]; !exists {
			result.HasDrift = true
			result.MissingInCloudflare = append(result.MissingInCloudflare, hostname)
		}
	}

	// Check for routes in Cloudflare but not in local
	for hostname := range cfMap {
		if _, exists := localMap[hostname]; !exists {
			result.HasDrift = true
			result.ExtraInCloudflare = append(result.ExtraInCloudflare, hostname)
		}
	}

	return result, nil
}

type ConfigDriftResult struct {
	HasDrift            bool     `json:"has_drift"`
	LocalRoutes         int      `json:"local_routes"`
	CloudflareRoutes    int      `json:"cloudflare_routes"`
	MissingInCloudflare []string `json:"missing_in_cloudflare"`
	ExtraInCloudflare   []string `json:"extra_in_cloudflare"`
}

// DeleteRoute deletes a route
func (t *TunnelService) DeleteRoute(ctx context.Context, apiKey string, routeID string) error {
	// Get route
	route, err := t.db.GetTunnelRoute(routeID)
	if err != nil {
		return fmt.Errorf("failed to get route: %w", err)
	}
	if route == nil {
		return fmt.Errorf("route not found")
	}

	// Get tunnel config
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("no tunnel configured")
	}

	// Delete DNS record if we have an API key and record ID
	if route.DNSRecordID != "" && apiKey != "" {
		cfAPI := NewCloudflareAPI(apiKey)
		if err := cfAPI.DeleteDNSRecord(ctx, route.ZoneID, route.DNSRecordID); err != nil {
			t.logger.Warn("failed to delete DNS record", zap.Error(err))
			// Continue with route deletion
		}
	}

	// Delete from database
	if err := t.db.DeleteTunnelRoute(routeID); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}

	// Regenerate config and reload
	if err := t.regenerateConfig(ctx, route.TunnelID); err != nil {
		return fmt.Errorf("failed to regenerate config: %w", err)
	}

	t.logger.Info("route deleted", zap.String("hostname", route.Hostname))
	return nil
}

// ReorderRoutes updates the sort order of routes
func (t *TunnelService) ReorderRoutes(ctx context.Context, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return nil
	}

	// Get first route to find tunnel ID
	firstRoute, err := t.db.GetTunnelRoute(orderedIDs[0])
	if err != nil {
		return fmt.Errorf("failed to get route: %w", err)
	}
	if firstRoute == nil {
		return fmt.Errorf("route not found")
	}

	// Update sort order for each route
	for i, id := range orderedIDs {
		if err := t.db.UpdateTunnelRouteSortOrder(id, i); err != nil {
			return fmt.Errorf("failed to update sort order: %w", err)
		}
	}

	// Regenerate config and reload
	if err := t.regenerateConfig(ctx, firstRoute.TunnelID); err != nil {
		return fmt.Errorf("failed to regenerate config: %w", err)
	}

	t.logger.Info("routes reordered", zap.Int("count", len(orderedIDs)))
	return nil
}

// ListRoutes returns all routes for the active tunnel
func (t *TunnelService) ListRoutes(ctx context.Context) ([]state.TunnelRoute, error) {
	// Get tunnel config
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel config: %w", err)
	}
	if config == nil {
		return []state.TunnelRoute{}, nil
	}

	// Get routes
	routes, err := t.db.ListTunnelRoutes(config.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	return routes, nil
}

// GetZonesFromStoredToken returns zones using the stored API token
func (t *TunnelService) GetZonesFromStoredToken(ctx context.Context, apiKey string) ([]Zone, error) {
	// Validate API key is provided
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	// Get zones from Cloudflare
	cfAPI := NewCloudflareAPI(apiKey)
	zones, err := cfAPI.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}

	return zones, nil
}

type TunnelWithDomains struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	Domains     []string `json:"domains"`
	AccountID   string   `json:"account_id"`
	IsManaged   bool     `json:"is_managed"` // true if this is the tunnel managed by OpenDeploy
}

func (t *TunnelService) ListAllTunnels(ctx context.Context, apiKey string) ([]TunnelWithDomains, error) {
	// Validate API key is provided
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	// Get tunnel config to get account ID
	config, err := t.db.GetTunnelConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel config: %w", err)
	}
	// Don't return error if no config - user might be viewing other tunnels without having OpenDeploy tunnel set up

	// Get tunnels from Cloudflare
	cfAPI := NewCloudflareAPI(apiKey)
	var accountID string
	if config != nil {
		accountID = config.AccountID
	} else {
		// If no config, we need to get the account ID another way - use the first available account
		accounts, err := cfAPI.ListAccounts(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list accounts: %w", err)
		}
		if len(accounts) == 0 {
			return nil, fmt.Errorf("no Cloudflare accounts found")
		}
		accountID = accounts[0].ID
	}

	tunnels, err := cfAPI.ListTunnels(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tunnels: %w", err)
	}

	// Get all zones to find DNS records pointing to tunnels
	zones, err := cfAPI.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}

	// Build result with domains and applications
	result := make([]TunnelWithDomains, 0, len(tunnels))
	for _, tunnel := range tunnels {
		domains := []string{}
		applications := []string{} // Store applications from ingress rules

		// Get tunnel configuration to extract applications
		tunnelConfig, err := cfAPI.GetTunnelConfiguration(ctx, accountID, tunnel.ID)
		if err != nil {
			t.logger.Warn("failed to get tunnel configuration", zap.String("tunnelID", tunnel.ID), zap.Error(err))
			// Continue without applications
		} else {
			// Extract hostnames from ingress rules (excluding catch-all rule)
			for _, rule := range tunnelConfig.Config.Ingress {
				if rule.Hostname != "" { // Skip catch-all rules like "service": "http_status:404"
					applications = append(applications, rule.Hostname)
					// We can also treat these as domains since they are valid hostnames served by this tunnel
					domains = append(domains, rule.Hostname)
				}
			}
		}

		// For additional domains not in ingress rules, check DNS records (backup method)
		domainSet := make(map[string]bool)
		for _, domain := range domains {
			domainSet[domain] = true
		}

		for _, zone := range zones {
			records, err := cfAPI.ListDNSRecords(ctx, zone.ID, "")
			if err != nil {
				continue // Skip zones we can't read
			}

			for _, record := range records {
				// CNAME records pointing to <tunnel-id>.cfargotunnel.com
				if record.Type == "CNAME" && record.Content == fmt.Sprintf("%s.cfargotunnel.com", tunnel.ID) {
					if !domainSet[record.Name] {
						domains = append(domains, record.Name)
					}
				}
			}
		}

		// Determine if this is our managed tunnel
		isManaged := false
		if config != nil {
			isManaged = tunnel.ID == config.TunnelID
		}

		result = append(result, TunnelWithDomains{
			ID:        tunnel.ID,
			Name:      tunnel.Name,
			Status:    tunnel.Status,
			CreatedAt: tunnel.CreatedAt.Format("2006-01-02 15:04:05"),
			Domains:   domains,
			AccountID: accountID,
			IsManaged: isManaged, // Mark if this is our managed tunnel
		})
	}

	return result, nil
}

func (t *TunnelService) StopRemoteTunnel(ctx context.Context, apiKey string, accountID, tunnelID string) error {
	// Validate API key is provided
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	// Delete the tunnel from Cloudflare
	cfAPI := NewCloudflareAPI(apiKey)
	if err := cfAPI.DeleteTunnel(ctx, accountID, tunnelID); err != nil {
		return fmt.Errorf("failed to delete tunnel: %w", err)
	}

	return nil
}

// AdoptTunnel adopts an existing Cloudflare tunnel into OpenDeploy management
func (t *TunnelService) AdoptTunnel(ctx context.Context, apiKey, accountID, tunnelID string) error {
	// Validate parameters
	if apiKey == "" || accountID == "" || tunnelID == "" {
		return fmt.Errorf("API key, account ID, and tunnel ID are required")
	}

	// Verify tunnel exists in Cloudflare
	cfAPI := NewCloudflareAPI(apiKey)
	_, err := cfAPI.GetTunnel(ctx, accountID, tunnelID)
	if err != nil {
		return fmt.Errorf("failed to verify tunnel: %w", err)
	}

	// Get tunnel configuration to extract routes
	_, err = cfAPI.GetTunnelConfiguration(ctx, accountID, tunnelID)
	if err != nil {
		t.logger.Warn("failed to get tunnel configuration", zap.String("tunnelID", tunnelID), zap.Error(err))
		// Continue without routes
	}

	// We can't get the tunnel token directly from the API for security reasons
	// In a real implementation, the user would need to provide it or we'd need to store it during original creation
	return fmt.Errorf("adopting existing tunnels requires tunnel token which is not available through Cloudflare API. This functionality would require a different approach.")
}

// GetTunnelTokenForAdoption would allow users to provide a token to adopt an existing tunnel
func (t *TunnelService) AdoptTunnelWithToken(ctx context.Context, tunnelID, tunnelToken, accountID, zoneID, tunnelName string, routes []state.TunnelRoute) error {
	// Validate parameters
	if tunnelID == "" || tunnelToken == "" {
		return fmt.Errorf("tunnel ID and token are required")
	}

	// Ensure cloudflared is installed
	if err := t.ensureCloudflaredInstalled(ctx); err != nil {
		return fmt.Errorf("cloudflared installation check failed: %w", err)
	}

	// Check if tunnel already exists in our database (don't duplicate)
	existing, err := t.db.GetTunnelConfig()
	if err != nil {
		return fmt.Errorf("checking existing config: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("a tunnel is already configured. Delete it first before adopting another tunnel.")
	}

	// Save tunnel configuration to database
	tunnelConfig := &state.TunnelConfig{
		TunnelID:    tunnelID,
		TunnelName:  tunnelName,
		TunnelToken: tunnelToken, // TODO: Encrypt before storing
		AccountID:   accountID,
		ZoneID:      zoneID,
		Domain:      "", // Will be set based on first route
		Status:      "active",
	}

	if err := t.db.SaveTunnelConfig(tunnelConfig); err != nil {
		t.logger.Error("failed to save tunnel config to database", zap.Error(err))
		return fmt.Errorf("failed to save tunnel config: %w", err)
	}

	// Save routes to database
	for i, route := range routes {
		route.TunnelID = tunnelID
		route.SortOrder = i // Set sort order based on position in array
		if err := t.db.CreateTunnelRoute(&route); err != nil {
			t.logger.Error("failed to save route", zap.Error(err))
			return fmt.Errorf("failed to save route: %w", err)
		}
	}

	// Write cloudflared config from routes - domain will be set from first route in writeConfig
	if err := t.writeConfig(tunnelID, ""); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Create systemd service
	if err := t.createSystemdService(tunnelToken); err != nil {
		return fmt.Errorf("failed to create systemd service: %w", err)
	}

	// Start the service
	if err := t.startService(ctx); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	t.logger.Info("tunnel adopted", zap.String("tunnelID", tunnelID))

	return nil
}

// createSystemdService creates the systemd unit file for cloudflared
func (t *TunnelService) createSystemdService(tunnelToken string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=OpenDeploy Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cloudflared tunnel run --token %s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=opendeploy-cloudflared

[Install]
WantedBy=multi-user.target
`, tunnelToken)

	servicePath := "/etc/systemd/system/opendeploy-cloudflared.service"

	// Write via sudo tee
	result, err := t.runner.RunWithStdin(context.Background(), exec.RunOpts{
		JobType: "tunnel_service_create",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/tee", servicePath},
		Timeout: 10 * time.Second,
	}, strings.NewReader(serviceContent))

	if err != nil {
		return fmt.Errorf("writing service file: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("failed to write service file: %s", result.Error)
	}

	// Set permissions
	_, _ = t.runner.Run(context.Background(), exec.RunOpts{
		JobType: "tunnel_service_chmod",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/chmod", "640", servicePath},
		Timeout: 5 * time.Second,
	})

	// Reload systemd
	_, err = t.runner.Run(context.Background(), exec.RunOpts{
		JobType: "tunnel_daemon_reload",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "daemon-reload"},
		Timeout: 10 * time.Second,
	})

	return err
}

// startService starts and enables the cloudflared service
func (t *TunnelService) startService(ctx context.Context) error {
	// Enable service
	_, _ = t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_enable",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "enable", "opendeploy-cloudflared"},
		Timeout: 10 * time.Second,
	})

	// Start service
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_start",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "start", "opendeploy-cloudflared"},
		Timeout: 15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("starting cloudflared service: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("cloudflared service failed to start: %s", result.Error)
	}

	return nil
}

// stopService stops the cloudflared service
func (t *TunnelService) stopService(ctx context.Context) error {
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_stop",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "stop", "opendeploy-cloudflared"},
		Timeout: 15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("stopping cloudflared service: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("cloudflared service failed to stop: %s", result.Error)
	}

	return nil
}

// disableService disables the cloudflared service
func (t *TunnelService) disableService(ctx context.Context) error {
	_, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_disable",
		Command: "/usr/bin/sudo",
		Args:    []string{"/usr/bin/systemctl", "disable", "opendeploy-cloudflared"},
		Timeout: 10 * time.Second,
	})
	return err
}

// verifyService checks if the service is running
func (t *TunnelService) verifyService(ctx context.Context) error {
	result, err := t.runner.Run(ctx, exec.RunOpts{
		JobType: "tunnel_verify",
		Command: "/usr/bin/systemctl",
		Args:    []string{"is-active", "opendeploy-cloudflared"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("checking service status: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("service is not active")
	}

	return nil
}

// hashToken creates a SHA256 hash of the API token for storage
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
