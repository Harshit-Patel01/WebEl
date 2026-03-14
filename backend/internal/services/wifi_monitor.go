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
	manualConnectMode   bool
	manualConnectUntil  time.Time
	apEnabled           bool
}

func NewWifiMonitor(runner *exec.Runner, wifiSvc *WifiService, logger *zap.Logger) *WifiMonitor {
	return &WifiMonitor{
		runner:   runner,
		wifiSvc:  wifiSvc,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

func (m *WifiMonitor) Start(ctx context.Context) {
	m.logger.Info("Starting WiFi monitor")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	time.Sleep(20 * time.Second)
	m.check(ctx)

	for {
		select {
		case <-ticker.C:
			m.check(ctx)
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

func (m *WifiMonitor) check(ctx context.Context) {
	if m.manualConnectMode && time.Now().Before(m.manualConnectUntil) {
		m.logger.Debug("Manual connect mode active, skipping check")
		return
	}
	if m.manualConnectMode {
		m.manualConnectMode = false
		m.logger.Info("Manual connect mode expired, resuming monitoring")
	}

	status, err := m.wifiSvc.GetStatus(ctx)
	if err != nil {
		m.logger.Warn("Failed to get WiFi status", zap.Error(err))
		// If we can't get status and AP is not enabled, try to enable it
		if !m.apEnabled {
			m.logger.Info("Cannot get WiFi status and AP not enabled, enabling AP as fallback")
			if err := m.enableAP(ctx); err != nil {
				m.logger.Error("Failed to enable AP", zap.Error(err))
			} else {
				m.apEnabled = true
			}
		}
		return
	}

	// Connected to real WiFi - disable AP
	if status.Connected && status.SSID != "" && status.SSID != "webel" {
		m.logger.Debug("Connected to WiFi", zap.String("ssid", status.SSID))
		if m.apEnabled {
			m.logger.Info("Disabling AP since WiFi is connected")
			m.disableAP(ctx)
			m.apEnabled = false
		}
		return
	}

	// AP is active
	if status.SSID == "webel" {
		m.logger.Debug("AP is active")
		m.apEnabled = true
		return
	}

	// Not connected - enable AP
	if !m.apEnabled {
		m.logger.Info("Not connected to any WiFi, enabling AP as fallback")
		if err := m.enableAP(ctx); err != nil {
			m.logger.Error("Failed to enable AP", zap.Error(err))
			// Retry AP creation with cleanup
			m.logger.Info("Retrying AP creation with full cleanup")
			m.cleanupBeforeAP(ctx)
			time.Sleep(3 * time.Second)
			if err := m.enableAP(ctx); err != nil {
				m.logger.Error("AP retry also failed", zap.Error(err))
			} else {
				m.apEnabled = true
			}
		} else {
			m.apEnabled = true
		}
	}
}

// cleanupBeforeAP ensures clean state before creating AP
func (m *WifiMonitor) cleanupBeforeAP(ctx context.Context) {
	m.logger.Info("Cleaning up before AP creation")

	// Stop any running services
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "hostapd"},
		Timeout: 10 * time.Second,
	})
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "dnsmasq"},
		Timeout: 10 * time.Second,
	})

	// Kill any stray processes
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "kill_hostapd",
		Command: "sudo",
		Args:    []string{"pkill", "-9", "hostapd"},
		Timeout: 5 * time.Second,
	})
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "kill_dnsmasq",
		Command: "sudo",
		Args:    []string{"pkill", "-9", "dnsmasq"},
		Timeout: 5 * time.Second,
	})

	// Flush interface
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "flush_wlan0",
		Command: "sudo",
		Args:    []string{"ip", "addr", "flush", "dev", "wlan0"},
		Timeout: 5 * time.Second,
	})

	// Bring interface down and up
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "ifdown",
		Command: "sudo",
		Args:    []string{"ip", "link", "set", "wlan0", "down"},
		Timeout: 5 * time.Second,
	})
	time.Sleep(1 * time.Second)
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "ifup",
		Command: "sudo",
		Args:    []string{"ip", "link", "set", "wlan0", "up"},
		Timeout: 5 * time.Second,
	})
}

func (m *WifiMonitor) enableAP(ctx context.Context) error {
	m.logger.Info("Creating AP with hostapd + dnsmasq")

	// Stop NetworkManager from managing wlan0
	result, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "nm_unmanage",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", "wlan0", "managed", "no"},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		m.logger.Warn("Failed to unmanage wlan0, continuing anyway")
	}

	time.Sleep(2 * time.Second)

	// Bring down interface
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "ifdown",
		Command: "sudo",
		Args:    []string{"ip", "link", "set", "wlan0", "down"},
		Timeout: 5 * time.Second,
	})

	time.Sleep(1 * time.Second)

	// Bring up interface
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "ifup",
		Command: "sudo",
		Args:    []string{"ip", "link", "set", "wlan0", "up"},
		Timeout: 5 * time.Second,
	})

	time.Sleep(2 * time.Second)

	// Flush any existing IPs
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "flush_ip",
		Command: "sudo",
		Args:    []string{"ip", "addr", "flush", "dev", "wlan0"},
		Timeout: 5 * time.Second,
	})

	// Set static IP
	result, err = m.runner.Run(ctx, exec.RunOpts{
		JobType: "set_ip",
		Command: "sudo",
		Args:    []string{"ip", "addr", "add", "192.168.4.1/24", "dev", "wlan0"},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		m.logger.Error("Failed to set IP address for AP")
		return fmt.Errorf("failed to set IP address")
	}

	// Create hostapd config
	hostapdConfig := `interface=wlan0
driver=nl80211
ssid=webel
hw_mode=g
channel=6
wmm_enabled=0
macaddr_acl=0
auth_algs=1
ignore_broadcast_ssid=0
wpa=2
wpa_passphrase=webel123
wpa_key_mgmt=WPA-PSK
wpa_pairwise=TKIP
rsn_pairwise=CCMP`

	// Write hostapd config using printf to avoid heredoc issues
	result, err = m.runner.Run(ctx, exec.RunOpts{
		JobType:   "write_hostapd",
		Command:   "bash",
		Args:      []string{"-c", `printf '%s\n' "` + strings.ReplaceAll(hostapdConfig, "'", "'\\''") + `" | sudo tee /etc/hostapd/hostapd.conf > /dev/null`},
		Timeout:   5 * time.Second,
	})

	if err != nil || !result.Success {
		m.logger.Error("Failed to write hostapd config")
		return fmt.Errorf("failed to write hostapd config")
	}

	// Create dnsmasq config
	dnsmasqConfig := `interface=wlan0
dhcp-range=192.168.4.2,192.168.4.20,255.255.255.0,24h
dhcp-option=3,192.168.4.1
dhcp-option=6,1.1.1.1,8.8.8.8
domain=wlan
address=/webel.local/192.168.4.1`

	// Write dnsmasq config using printf
	result, err = m.runner.Run(ctx, exec.RunOpts{
		JobType:   "write_dnsmasq",
		Command:   "bash",
		Args:      []string{"-c", `printf '%s\n' "` + strings.ReplaceAll(dnsmasqConfig, "'", "'\\''") + `" | sudo tee /etc/dnsmasq.d/webel-ap.conf > /dev/null`},
		Timeout:   5 * time.Second,
	})

	if err != nil || !result.Success {
		m.logger.Error("Failed to write dnsmasq config")
		return fmt.Errorf("failed to write dnsmasq config")
	}

	// Stop any existing dnsmasq/hostapd first to ensure clean state
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "hostapd"},
		Timeout: 5 * time.Second,
	})
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "dnsmasq"},
		Timeout: 5 * time.Second,
	})

	time.Sleep(2 * time.Second)

	// Start hostapd
	m.logger.Info("Starting hostapd")
	result, err = m.runner.Run(ctx, exec.RunOpts{
		JobType: "start_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "start", "hostapd"},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		m.logger.Error("Failed to start hostapd")
		// Check hostapd status for debugging
		statusResult, _ := m.runner.Run(ctx, exec.RunOpts{
			JobType: "hostapd_status",
			Command: "sudo",
			Args:    []string{"systemctl", "status", "hostapd"},
			Timeout: 5 * time.Second,
		})
		if statusResult != nil {
			for _, line := range statusResult.Lines {
				m.logger.Debug("hostapd status", zap.String("line", line.Text))
			}
		}
		return fmt.Errorf("failed to start hostapd")
	}

	// Wait for hostapd to initialize
	time.Sleep(3 * time.Second)

	// Verify hostapd is running
	checkResult, _ := m.runner.Run(ctx, exec.RunOpts{
		JobType: "check_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "hostapd"},
		Timeout: 5 * time.Second,
	})

	hostapdActive := ""
	if checkResult != nil && len(checkResult.Lines) > 0 {
		for _, line := range checkResult.Lines {
			if line.Stream == "stdout" {
				hostapdActive = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	if hostapdActive != "active" {
		m.logger.Error("hostapd is not active", zap.String("status", hostapdActive))
		return fmt.Errorf("hostapd failed to start properly")
	}

	// Start dnsmasq
	m.logger.Info("Starting dnsmasq")
	result, err = m.runner.Run(ctx, exec.RunOpts{
		JobType: "start_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "start", "dnsmasq"},
		Timeout: 10 * time.Second,
	})

	if err != nil || !result.Success {
		m.logger.Error("Failed to start dnsmasq")
		// Try to restart dnsmasq if start fails
		m.logger.Info("Attempting to restart dnsmasq")
		result, err = m.runner.Run(ctx, exec.RunOpts{
			JobType: "restart_dnsmasq",
			Command: "sudo",
			Args:    []string{"systemctl", "restart", "dnsmasq"},
			Timeout: 10 * time.Second,
		})
		if err != nil || !result.Success {
			m.logger.Warn("dnsmasq failed to start, but AP may still work for connectivity")
			// Don't return error - AP can work without DHCP if clients use static IP
		}
	}

	// Verify dnsmasq is running
	time.Sleep(2 * time.Second)
	checkResult, _ = m.runner.Run(ctx, exec.RunOpts{
		JobType: "check_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "dnsmasq"},
		Timeout: 5 * time.Second,
	})

	dnsmasqActive := ""
	if checkResult != nil && len(checkResult.Lines) > 0 {
		for _, line := range checkResult.Lines {
			if line.Stream == "stdout" {
				dnsmasqActive = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	if dnsmasqActive == "active" {
		m.logger.Info("AP fully enabled: SSID=webel, Password=webel123, IP=192.168.4.1")
	} else {
		m.logger.Warn("AP enabled but dnsmasq may not be running", zap.String("status", dnsmasqActive))
	}

	return nil
}

func (m *WifiMonitor) disableAP(ctx context.Context) error {
	m.logger.Info("Disabling AP")

	// Stop hostapd
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "hostapd"},
		Timeout: 10 * time.Second,
	})

	// Stop dnsmasq
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "dnsmasq"},
		Timeout: 10 * time.Second,
	})

	// Flush IP
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "flush_ip",
		Command: "sudo",
		Args:    []string{"ip", "addr", "flush", "dev", "wlan0"},
		Timeout: 5 * time.Second,
	})

	// Let NetworkManager manage wlan0 again
	m.runner.Run(ctx, exec.RunOpts{
		JobType: "nm_manage",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", "wlan0", "managed", "yes"},
		Timeout: 5 * time.Second,
	})

	return nil
}

func (m *WifiMonitor) SetManualConnectMode(duration time.Duration) {
	m.manualConnectMode = true
	m.manualConnectUntil = time.Now().Add(duration)
	m.logger.Info("Manual connect mode enabled", zap.Duration("duration", duration))
}

func (m *WifiMonitor) IsAPEnabled() bool {
	return m.apEnabled
}
