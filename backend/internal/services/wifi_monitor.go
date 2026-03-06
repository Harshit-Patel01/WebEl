package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

type WifiMonitor struct {
	runner              *exec.Runner
	wifiSvc             *WifiService
	logger              *zap.Logger
	stopChan            chan struct{}
	apEnabled           bool
	lastWifiConnectTime time.Time
	wifiConnectGrace    time.Duration
	lastAPDisableTime   time.Time
	apDisableCooldown   time.Duration
	manualConnectMode   bool
	manualConnectUntil  time.Time
}

func NewWifiMonitor(runner *exec.Runner, wifiSvc *WifiService, logger *zap.Logger) *WifiMonitor {
	return &WifiMonitor{
		runner:            runner,
		wifiSvc:           wifiSvc,
		logger:            logger,
		stopChan:          make(chan struct{}),
		wifiConnectGrace:  60 * time.Second, // Give 60 seconds grace period for WiFi to stabilize
		apDisableCooldown: 30 * time.Second, // Wait 30 seconds after disabling AP before re-enabling
	}
}

func (m *WifiMonitor) Start(ctx context.Context) {
	m.logger.Info("Starting WiFi monitor")

	// Check immediately on start
	m.checkAndCreateAP(ctx)

	// Then check every 10 seconds for more responsive monitoring
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAndCreateAP(ctx)
		case <-m.stopChan:
			m.logger.Info("Stopping WiFi monitor")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (m *WifiMonitor) Stop() {
	close(m.stopChan)
}

// IsAPEnebled returns whether the AP mode is currently enabled
func (m *WifiMonitor) IsAPEnabled() bool {
	return m.apEnabled
}

func (m *WifiMonitor) checkAndCreateAP(ctx context.Context) {
	// Check if we're in manual connect mode - don't interfere
	if m.manualConnectMode && time.Now().Before(m.manualConnectUntil) {
		m.logger.Debug("In manual connect mode, skipping AP check")
		return
	}
	// Reset manual connect mode if expired
	if m.manualConnectMode && time.Now().After(m.manualConnectUntil) {
		m.logger.Info("Manual connect mode expired")
		m.manualConnectMode = false
	}

	status, err := m.wifiSvc.GetStatus(ctx)
	if err != nil {
		m.logger.Error("Failed to get WiFi status", zap.Error(err))
		// Only attempt to enable AP if we had a network error, not other errors
		if m.isNetworkError(err) && !m.apEnabled {
			m.logger.Info("Network error detected, enabling fallback AP")
			if err := m.enableAP(ctx); err != nil {
				m.logger.Error("Failed to enable fallback AP", zap.Error(err))
			} else {
				m.apEnabled = true
			}
		}
		return
	}

	// Check if we're currently in AP mode - don't disable it if we are
	if status.SSID == "webel-hotspot" || status.SSID == "webel" {
		m.logger.Debug("Currently in AP mode, keeping it active")
		m.apEnabled = true
		// Only verify if we think AP should be enabled
		if m.apEnabled && !m.verifyAPActive(ctx) {
			m.logger.Warn("AP connection exists but not active, re-enabling")
			if err := m.enableAP(ctx); err != nil {
				m.logger.Error("Failed to re-enable fallback AP", zap.Error(err))
			}
		}
		return
	}

	// If connected to a real WiFi network, handle grace period and AP disabling
	if status.Connected && status.SSID != "" && status.SSID != "webel-hotspot" && status.SSID != "webel" {
		// Record the connection time only once when first connected
		if m.lastWifiConnectTime.IsZero() {
			m.lastWifiConnectTime = time.Now()
			m.logger.Info("WiFi connection detected, starting grace period",
				zap.String("ssid", status.SSID),
				zap.Duration("gracePeriod", m.wifiConnectGrace),
			)

			// Immediately disable AP when external WiFi connects to avoid conflicts
			if m.apEnabled {
				m.logger.Info("External WiFi connected, immediately disabling AP to avoid conflicts",
					zap.String("ssid", status.SSID),
				)
				if err := m.disableAP(ctx); err != nil {
					m.logger.Error("Failed to disable fallback AP", zap.Error(err))
				} else {
					m.apEnabled = false
				}
			}
			return
		}

		// Check if we're still in grace period
		timeSinceConnect := time.Since(m.lastWifiConnectTime)
		if timeSinceConnect < m.wifiConnectGrace {
			m.logger.Debug("WiFi in grace period",
				zap.String("ssid", status.SSID),
				zap.Duration("elapsed", timeSinceConnect),
				zap.Duration("remaining", m.wifiConnectGrace-timeSinceConnect),
			)
			// Ensure AP is disabled during grace period
			if m.apEnabled {
				m.logger.Info("Ensuring AP is disabled during WiFi grace period")
				if err := m.disableAP(ctx); err != nil {
					m.logger.Error("Failed to disable AP during grace period", zap.Error(err))
				} else {
					m.apEnabled = false
				}
			}
			return
		}

		// Grace period passed, WiFi is stable
		if m.apEnabled {
			m.logger.Info("WiFi stable after grace period, ensuring AP is disabled",
				zap.String("ssid", status.SSID),
				zap.Duration("stableFor", timeSinceConnect),
			)
			if err := m.disableAP(ctx); err != nil {
				m.logger.Error("Failed to disable fallback AP", zap.Error(err))
			} else {
				m.apEnabled = false
				// After successfully disabling AP and WiFi is stable, refresh Avahi
				// to ensure hostname is properly announced on the new network
				m.logger.Info("Refreshing Avahi after stable WiFi connection")
				if err := m.wifiSvc.RefreshAvahiAfterNetworkChange(ctx); err != nil {
					m.logger.Warn("Failed to refresh Avahi after network change", zap.Error(err))
				}
			}
		}
		return
	}

	// Not connected, reset the WiFi connect time and enable AP if needed
	if !m.lastWifiConnectTime.IsZero() {
		m.logger.Info("WiFi disconnected, resetting grace period")
		m.lastWifiConnectTime = time.Time{}
	}

	// Check if we're in cooldown period after disabling AP
	if !m.lastAPDisableTime.IsZero() {
		timeSinceDisable := time.Since(m.lastAPDisableTime)
		if timeSinceDisable < m.apDisableCooldown {
			m.logger.Debug("In AP disable cooldown period, not re-enabling yet",
				zap.Duration("elapsed", timeSinceDisable),
				zap.Duration("remaining", m.apDisableCooldown-timeSinceDisable),
			)
			return
		}
	}

	if !m.apEnabled {
		m.logger.Info("WiFi not connected, enabling fallback AP")
		if err := m.enableAP(ctx); err != nil {
			m.logger.Error("Failed to enable fallback AP", zap.Error(err))
		} else {
			m.apEnabled = true
		}
	} else {
		// AP is already enabled, verify it's still active but don't spam re-enable
		m.logger.Debug("AP already enabled, verifying status")
		if !m.verifyAPActive(ctx) {
			m.logger.Warn("AP seems to have stopped, re-enabling")
			if err := m.enableAP(ctx); err != nil {
				m.logger.Error("Failed to re-enable fallback AP", zap.Error(err))
			}
		}
	}
}

// isNetworkError checks if the error is related to network connectivity
func (m *WifiMonitor) isNetworkError(err error) bool {
	errStr := err.Error()
	return containsAny(errStr, []string{
		"device not found",
		"not managed",
		"disconnected",
		"no carrier",
		"not connected",
		"no suitable device found",
	})
}

// verifyAPActive checks if the AP is still active
func (m *WifiMonitor) verifyAPActive(ctx context.Context) bool {
	// Check if the connection is active
	result, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_check",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "NAME,DEVICE,STATE", "connection", "show", "--active"},
		Timeout: 10 * time.Second,
	})

	if err != nil {
		m.logger.Warn("Failed to check active connections", zap.Error(err))
		return false
	}

	connectionActive := false
	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.Contains(line.Text, "webel-hotspot") {
			parts := strings.Split(line.Text, ":")
			if len(parts) >= 3 && (parts[2] == "activated" || strings.Contains(parts[2], "activating")) {
				connectionActive = true
				break
			}
		}
	}

	if !connectionActive {
		m.logger.Debug("AP connection not found in active connections")
		return false
	}

	// Additionally check if the interface is in AP mode
	result2, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_mode_check",
		Command: "sudo",
		Args:    []string{"iw", "dev", "wlan0", "info"},
		Timeout: 10 * time.Second,
	})

	if err != nil {
		m.logger.Warn("Failed to check interface mode", zap.Error(err))
		// If we can't check, but connection is active, assume it's working
		return connectionActive
	}

	// Check if the interface is in AP mode
	for _, line := range result2.Lines {
		if line.Stream == "stdout" && strings.Contains(line.Text, "type") && strings.Contains(line.Text, "AP") {
			m.logger.Debug("AP mode verified active")
			return true
		}
	}

	m.logger.Debug("Interface not in AP mode")
	return false
}

// containsAny checks if the string contains any of the substrings
func containsAny(str string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(str, substr) {
			return true
		}
	}
	return false
}

func (m *WifiMonitor) enableAP(ctx context.Context) error {
	m.logger.Info("Starting AP enablement process")

	// Check if webel-hotspot connection already exists and is active
	checkResult, _ := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_check_existing",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "NAME,DEVICE,STATE", "connection", "show", "--active"},
		Timeout: 5 * time.Second,
	})

	// If hotspot is already active, don't recreate it
	if checkResult != nil {
		for _, line := range checkResult.Lines {
			if line.Stream == "stdout" && strings.Contains(line.Text, "webel-hotspot") {
				parts := strings.Split(line.Text, ":")
				if len(parts) >= 3 && parts[2] == "activated" {
					m.logger.Info("Hotspot already active, skipping recreation")
					return nil
				}
			}
		}
	}

	// First, try to bring down any existing wlan0 connections to avoid conflicts
	_, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_cleanup",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "down", "Hotspot"},
		Timeout: 5 * time.Second,
	})

	// Remove any existing hotspot connection
	_, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_delete_old",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "delete", "Hotspot"},
		Timeout: 5 * time.Second,
	})

	// Remove any existing webel-hotspot connection to ensure clean state
	_, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_delete_old_webel",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "delete", "webel-hotspot"},
		Timeout: 5 * time.Second,
	})

	// Ensure wlan0 is managed by NetworkManager
	_, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_set_managed",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", "wlan0", "managed", "yes"},
		Timeout: 5 * time.Second,
	})

	// Create a new hotspot connection using nmcli with proper IP configuration
	m.logger.Info("Creating hotspot connection profile")
	_, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_create",
		Command: "sudo",
		Args: []string{
			"nmcli", "con", "add", "type", "wifi",
			"ifname", "wlan0",
			"con-name", "webel-hotspot",
			"autoconnect", "yes",
			"ssid", "webel",
			"mode", "ap",
			"ipv4.method", "shared",  // Enable IP sharing/DHCP server
			"ipv4.addresses", "10.42.0.1/24", // Set static IP for the AP
		},
		Timeout: 15 * time.Second,
	})

	if err != nil {
		m.logger.Error("Failed to create hotspot connection", zap.Error(err))
		return fmt.Errorf("failed to create hotspot connection: %w", err)
	}

	// Configure WiFi settings for the hotspot
	m.logger.Info("Configuring WiFi settings")
	_, err = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_set_wifi_settings",
		Command: "sudo",
		Args: []string{
			"nmcli", "con", "modify", "webel-hotspot",
			"wifi.band", "bg",  // Use 2.4GHz band for better compatibility
			"wifi.channel", "6", // Use channel 6 to avoid interference
			"wifi.hidden", "false", // Make SSID visible
		},
		Timeout: 10 * time.Second,
	})

	if err != nil {
		m.logger.Warn("Failed to set WiFi band/channel settings", zap.Error(err))
		// Continue anyway, these are optional
	}

	// Set the password for the hotspot with WPA2 security
	m.logger.Info("Setting hotspot security")
	_, err = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_set_password",
		Command: "sudo",
		Args: []string{
			"nmcli", "con", "modify", "webel-hotspot",
			"wifi-sec.key-mgmt", "wpa-psk",
			"wifi-sec.psk", "webel123",
			"wifi-sec.proto", "rsn", // Use RSN/WPA2
			"wifi-sec.pairwise", "ccmp", // Use CCMP encryption (AES)
			"wifi-sec.group", "ccmp",
		},
		Timeout: 15 * time.Second,
	})

	if err != nil {
		m.logger.Error("Failed to set hotspot password", zap.Error(err))
		// Clean up the connection if password setting fails
		_, _ = m.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_ap_cleanup_fail",
			Command: "sudo",
			Args:    []string{"nmcli", "connection", "delete", "webel-hotspot"},
			Timeout: 5 * time.Second,
		})
		return fmt.Errorf("failed to set hotspot password: %w", err)
	}

	// Set connection to never timeout but with VERY LOW priority so real WiFi takes precedence
	m.logger.Info("Configuring connection persistence with low priority")
	_, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_set_persistence",
		Command: "sudo",
		Args: []string{
			"nmcli", "con", "modify", "webel-hotspot",
			"connection.autoconnect", "no", // Don't auto-connect, only manual activation
			"connection.autoconnect-priority", "-999", // Very low priority so external WiFi always wins
			"ipv4.may-fail", "no", // Don't fail if IPv4 setup fails
		},
		Timeout: 10 * time.Second,
	})

	// Activate the hotspot connection
	m.logger.Info("Activating hotspot connection")
	result, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_activate",
		Command: "sudo",
		Args: []string{
			"nmcli", "connection", "up", "webel-hotspot",
		},
		Timeout: 30 * time.Second, // Increased timeout to allow DHCP server to start
	})

	if err != nil {
		m.logger.Error("Failed to activate hotspot", zap.Error(err))
		// Log the output for debugging
		for _, line := range result.Lines {
			if line.Stream == "stderr" {
				m.logger.Error("Activation error", zap.String("error", line.Text))
			}
		}
		return fmt.Errorf("failed to activate hotspot: %w", err)
	}

	// Wait for the interface to stabilize
	time.Sleep(3 * time.Second)

	// Verify the interface is up and has the correct IP
	m.logger.Info("Verifying AP configuration")
	ipResult, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_ip_check",
		Command: "ip",
		Args:    []string{"addr", "show", "wlan0"},
		Timeout: 10 * time.Second,
	})

	if err != nil {
		m.logger.Warn("Could not check IP configuration", zap.Error(err))
	} else {
		hasCorrectIp := false
		for _, line := range ipResult.Lines {
			if line.Stream == "stdout" && strings.Contains(line.Text, "inet 10.42.0.1") {
				hasCorrectIp = true
				m.logger.Info("AP IP address verified", zap.String("ip", "10.42.0.1"))
				break
			}
		}

		if !hasCorrectIp {
			m.logger.Warn("AP may not have correct IP address, attempting to bring interface up")
			// Try to bring up the interface manually
			_, _ = m.runner.Run(ctx, exec.RunOpts{
				JobType: "wifi_ap_up_interface",
				Command: "sudo",
				Args:    []string{"ip", "link", "set", "wlan0", "up"},
				Timeout: 10 * time.Second,
			})
		}
	}

	m.logger.Info("Fallback AP 'webel' enabled successfully with DHCP server on 10.42.0.1/24")
	return nil
}

// SetManualConnectMode prevents the monitor from interfering during manual connections
func (m *WifiMonitor) SetManualConnectMode(duration time.Duration) {
	m.manualConnectMode = true
	m.manualConnectUntil = time.Now().Add(duration)
	m.logger.Info("Manual connect mode enabled", zap.Duration("duration", duration))
}

func (m *WifiMonitor) disableAP(ctx context.Context) error {
	m.logger.Info("Disabling AP - starting shutdown sequence")

	// Deactivate the hotspot connection first
	_, err1 := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_down",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "down", "webel-hotspot"},
		Timeout: 10 * time.Second,
	})

	// Wait a moment for deactivation to complete
	time.Sleep(2 * time.Second)

	// Delete the hotspot connection
	_, err2 := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_delete",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "delete", "webel-hotspot"},
		Timeout: 10 * time.Second,
	})

	// Don't bring down the interface - let WiFi stay connected
	// This was causing WiFi to disconnect when AP was disabled

	// Return the first error if both failed, or nil if at least one succeeded
	if err1 != nil && err2 != nil {
		m.logger.Error("Failed to disable hotspot", zap.Error(err1), zap.Error(err2))
		return fmt.Errorf("failed to disable hotspot: %v, %v", err1, err2)
	}

	// Record the time we disabled AP to prevent immediate re-enabling
	m.lastAPDisableTime = time.Now()
	m.logger.Info("Fallback AP 'webel' disabled successfully - cooldown period started",
		zap.Duration("cooldown", m.apDisableCooldown),
	)
	return nil
}
