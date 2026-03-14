package services

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

type WifiNetwork struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"`
	Security  string `json:"security"`
	Connected bool   `json:"connected"`
	Saved     bool   `json:"saved"`
}

type WifiStatus struct {
	Connected bool   `json:"connected"`
	SSID      string `json:"ssid,omitempty"`
	IP        string `json:"ip,omitempty"`
	Signal    int    `json:"signal,omitempty"`
	State     string `json:"state"`
}

type WifiService struct {
	runner       *exec.Runner
	logger       *zap.Logger
	db           *state.DB
	avahiService *AvahiService
}

func NewWifiService(runner *exec.Runner, logger *zap.Logger, db *state.DB) *WifiService {
	ws := &WifiService{
		runner:       runner,
		logger:       logger,
		db:           db,
		avahiService: NewAvahiService(runner, logger),
	}

	return ws
}

func (w *WifiService) ScanNetworks(ctx context.Context) ([]WifiNetwork, error) {
	result, err := w.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_scan",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "yes"},
		Timeout: 20 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("scanning wifi: %w", err)
	}

	// Get current connection for marking
	currentSSID := w.getCurrentSSID()

	// Get saved networks
	savedNetworks := make(map[string]bool)
	if w.db != nil {
		saved, err := w.db.ListSavedWifiNetworks()
		if err == nil {
			for _, n := range saved {
				savedNetworks[n.SSID] = true
			}
		}
	}

	var networks []WifiNetwork
	seen := make(map[string]bool)

	for _, line := range result.Lines {
		if line.Stream != "stdout" || line.Text == "" {
			continue
		}
		parts := strings.SplitN(line.Text, ":", 3)
		if len(parts) < 3 {
			continue
		}
		ssid := strings.TrimSpace(parts[0])
		if ssid == "" || seen[ssid] {
			continue
		}
		seen[ssid] = true

		signal, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		security := strings.TrimSpace(parts[2])
		if security == "" {
			security = "Open"
		}

		networks = append(networks, WifiNetwork{
			SSID:      ssid,
			Signal:    signal,
			Security:  security,
			Connected: ssid == currentSSID,
			Saved:     savedNetworks[ssid],
		})
	}

	return networks, nil
}

func (w *WifiService) Connect(ctx context.Context, ssid, password, jobID string) (*exec.ExecResult, error) {
	if !isValidSSID(ssid) {
		return nil, fmt.Errorf("invalid SSID")
	}

	w.logger.Info("Connecting to WiFi", zap.String("ssid", ssid))

	// AP runs on separate virtual interface (ap0) — do NOT touch it here.
	// wlan0 is for STA (station) mode only.

	// Step 1: Detect security type of target network
	securityType, err := w.detectSecurityType(ctx, ssid)
	if err != nil {
		w.logger.Warn("Failed to detect security type, defaulting to WPA-PSK", zap.Error(err))
		securityType = "WPA2" // Default fallback
	}
	w.logger.Info("Detected security type", zap.String("ssid", ssid), zap.String("security", securityType))

	// Step 2: Ensure NetworkManager manages wlan0
	w.runner.Run(ctx, exec.RunOpts{
		JobType: "nm_manage",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", "wlan0", "managed", "yes"},
		Timeout: 5 * time.Second,
	})

	time.Sleep(1 * time.Second)

	// Step 3: Disconnect current WiFi cleanly (if any)
	w.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "wifi_disconnect",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "disconnect", "wlan0"},
		Timeout: 10 * time.Second,
	})
	time.Sleep(1 * time.Second)

	// Step 4: Delete old connection profiles for this SSID (including netplan variants)
	w.logger.Info("Deleting old connection profiles", zap.String("ssid", ssid))
	w.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "delete_profile",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "delete", ssid},
		Timeout: 5 * time.Second,
	})
	w.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "delete_netplan_profile",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "delete", "netplan-wlan0-" + ssid},
		Timeout: 5 * time.Second,
	})

	time.Sleep(1 * time.Second)

	// Step 5: Create new connection profile with HIGHEST priority
	// New connection gets priority 100; all others will be lowered afterward.
	w.logger.Info("Creating connection profile", zap.String("ssid", ssid), zap.String("security", securityType))

	var createResult *exec.ExecResult
	var createErr error

	if securityType == "Open" || securityType == "" {
		createResult, createErr = w.runner.Run(ctx, exec.RunOpts{
			JobID:   jobID,
			JobType: "create_profile",
			Command: "sudo",
			Args: []string{
				"nmcli", "connection", "add",
				"type", "wifi",
				"con-name", ssid,
				"ifname", "wlan0",
				"ssid", ssid,
				"connection.autoconnect", "yes",
				"connection.autoconnect-priority", "100",
			},
			Timeout: 10 * time.Second,
		})
	} else if strings.Contains(securityType, "WEP") {
		createResult, createErr = w.runner.Run(ctx, exec.RunOpts{
			JobID:   jobID,
			JobType: "create_profile",
			Command: "sudo",
			Args: []string{
				"nmcli", "connection", "add",
				"type", "wifi",
				"con-name", ssid,
				"ifname", "wlan0",
				"ssid", ssid,
				"wifi-sec.key-mgmt", "none",
				"wifi-sec.wep-key0", password,
				"connection.autoconnect", "yes",
				"connection.autoconnect-priority", "100",
			},
			Timeout: 10 * time.Second,
		})
	} else {
		createResult, createErr = w.runner.Run(ctx, exec.RunOpts{
			JobID:   jobID,
			JobType: "create_profile",
			Command: "sudo",
			Args: []string{
				"nmcli", "connection", "add",
				"type", "wifi",
				"con-name", ssid,
				"ifname", "wlan0",
				"ssid", ssid,
				"wifi-sec.key-mgmt", "wpa-psk",
				"wifi-sec.psk", password,
				"connection.autoconnect", "yes",
				"connection.autoconnect-priority", "100",
			},
			Timeout: 10 * time.Second,
		})
	}

	if createErr != nil || !createResult.Success {
		w.logger.Error("Failed to create connection profile", zap.Error(createErr))
		return &exec.ExecResult{
			Success: false,
			Lines: []exec.LogLine{
				{Stream: "stderr", Text: "Failed to create connection profile", Timestamp: time.Now()},
			},
		}, fmt.Errorf("failed to create connection profile")
	}

	time.Sleep(2 * time.Second)

	// Step 6: Activate the new connection
	w.logger.Info("Activating new connection", zap.String("ssid", ssid))
	connectResult, err := w.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "wifi_connect",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "up", ssid},
		Timeout: 45 * time.Second,
	})

	if err != nil || !connectResult.Success {
		w.logger.Error("Failed to activate connection", zap.Error(err))
		return &exec.ExecResult{
			Success: false,
			Lines: []exec.LogLine{
				{Stream: "stderr", Text: "Failed to activate WiFi connection", Timestamp: time.Now()},
			},
		}, fmt.Errorf("failed to activate connection")
	}

	// Step 7: Verify connection (check for IP assignment)
	w.logger.Info("Verifying connection", zap.String("ssid", ssid))
	time.Sleep(5 * time.Second)

	verified := false
	for i := 0; i < 10; i++ {
		status, err := w.GetStatus(ctx)
		if err == nil && status.Connected && status.SSID == ssid && status.IP != "" {
			w.logger.Info("WiFi connected and verified", zap.String("ssid", ssid), zap.String("ip", status.IP))
			verified = true
			break
		}
		w.logger.Debug("Waiting for connection to stabilize", zap.Int("attempt", i+1))
		time.Sleep(3 * time.Second)
	}

	if !verified {
		w.logger.Error("Connection verification failed - no IP assigned after 35 seconds")
		return &exec.ExecResult{
			Success: false,
			Lines: []exec.LogLine{
				{Stream: "stderr", Text: "Connection verification failed - no IP assigned after 35 seconds", Timestamp: time.Now()},
			},
		}, fmt.Errorf("connection verification failed")
	}

	// Step 8: Lower priority of ALL other WiFi connections
	// This ensures NetworkManager will auto-connect to the NEW network on reboot,
	// not the old one. The user can manually switch back if they want.
	w.lowerOtherWifiPriorities(ctx, ssid)

	// Success - save to database
	if w.db != nil {
		now := time.Now()
		w.db.SaveWifiNetwork(&state.SavedWifiNetwork{
			SSID:            ssid,
			Password:        password,
			Security:        securityType,
			LastConnectedAt: &now,
		})
	}

	// Refresh Avahi
	if err := w.avahiService.RefreshHostname(ctx); err != nil {
		w.logger.Warn("Failed to refresh Avahi", zap.Error(err))
	}

	connectResult.Success = true
	return connectResult, nil
}

// detectSecurityType scans for the network and returns its security type
func (w *WifiService) detectSecurityType(ctx context.Context, ssid string) (string, error) {
	result, err := w.runner.Run(ctx, exec.RunOpts{
		JobType: "detect_security",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "SSID,SECURITY", "device", "wifi", "list"},
		Timeout: 15 * time.Second,
	})
	if err != nil {
		return "", err
	}

	for _, line := range result.Lines {
		if line.Stream != "stdout" {
			continue
		}
		parts := strings.SplitN(line.Text, ":", 2)
		if len(parts) == 2 {
			networkSSID := strings.TrimSpace(parts[0])
			security := strings.TrimSpace(parts[1])

			if networkSSID == ssid {
				if security == "" || security == "--" {
					return "Open", nil
				}
				// Return the actual security string from nmcli
				return security, nil
			}
		}
	}

	return "", fmt.Errorf("network not found in scan")
}

// lowerOtherWifiPriorities sets all other WiFi connections to lower priority
// so NetworkManager will auto-connect to the most recently used network on reboot.
// The current SSID keeps priority 100; all others get decremented based on age.
func (w *WifiService) lowerOtherWifiPriorities(ctx context.Context, currentSSID string) {
	w.logger.Info("Updating WiFi priorities — new network gets highest",
		zap.String("highest", currentSSID))

	listResult, _ := w.runner.Run(ctx, exec.RunOpts{
		JobType: "list_connections",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "NAME,TYPE", "connection", "show"},
		Timeout: 10 * time.Second,
	})

	if listResult == nil {
		return
	}

	for _, line := range listResult.Lines {
		if line.Stream != "stdout" || !strings.Contains(line.Text, ":wifi") {
			continue
		}
		parts := strings.SplitN(line.Text, ":", 2)
		if len(parts) != 2 {
			continue
		}
		connName := strings.TrimSpace(parts[0])

		// Skip the new connection (it already has priority 100)
		if connName == currentSSID {
			continue
		}
		// Skip AP hotspot connections
		if connName == "webel-hotspot" || connName == "Hotspot" {
			continue
		}

		// Set other connections to priority 10 (still auto-connect, but lower than 100)
		// This means: if the priority-100 network is unavailable, NM will try these.
		w.runner.Run(ctx, exec.RunOpts{
			JobType: "lower_priority",
			Command: "sudo",
			Args:    []string{"nmcli", "connection", "modify", connName, "connection.autoconnect-priority", "10"},
			Timeout: 5 * time.Second,
		})
		w.logger.Debug("Lowered priority for WiFi connection",
			zap.String("name", connName), zap.Int("priority", 10))
	}
}

func (w *WifiService) Disconnect(ctx context.Context) error {
	_, err := w.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_disconnect",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "disconnect", "wlan0"},
		Timeout: 10 * time.Second,
	})
	return err
}

func (w *WifiService) GetStatus(ctx context.Context) (*WifiStatus, error) {
	result, err := w.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_status",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "GENERAL.STATE,IP4.ADDRESS,GENERAL.CONNECTION", "device", "show", "wlan0"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	status := &WifiStatus{State: "disconnected"}
	for _, line := range result.Lines {
		if line.Stream != "stdout" {
			continue
		}
		parts := strings.SplitN(line.Text, ":", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "GENERAL.STATE":
			if strings.Contains(val, "connected") {
				status.Connected = true
				status.State = "connected"
			}
		case "IP4.ADDRESS":
			// format: "192.168.1.45/24"
			if idx := strings.Index(val, "/"); idx > 0 {
				status.IP = val[:idx]
			} else {
				status.IP = val
			}
		case "GENERAL.CONNECTION":
			status.SSID = val
		}
	}

	return status, nil
}

func (w *WifiService) getCurrentSSID() string {
	result, err := w.runner.Run(context.Background(), exec.RunOpts{
		JobType: "wifi_current",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "ACTIVE,SSID", "device", "wifi"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return ""
	}
	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.HasPrefix(line.Text, "yes:") {
			parts := strings.SplitN(line.Text, ":", 2)
			if len(parts) == 2 {
				return parts[1]
			}
		}
	}
	return ""
}


var ssidRegex = regexp.MustCompile(`^[\w\s\-\.!@#$%^&*()\'":;,+=\[\]{}<>/\?\\]{1,64}$`)

func isValidSSID(ssid string) bool {
	return ssidRegex.MatchString(ssid)
}

func (w *WifiService) UpdatePassword(ssid, password string) error {
	if w.db == nil {
		return fmt.Errorf("database not available")
	}
	return w.db.UpdateWifiPassword(ssid, password)
}

func (w *WifiService) DeleteSavedNetwork(ssid string) error {
	if w.db == nil {
		return fmt.Errorf("database not available")
	}
	return w.db.DeleteSavedWifiNetwork(ssid)
}

func (w *WifiService) GetSavedNetworks() ([]state.SavedWifiNetwork, error) {
	if w.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	return w.db.ListSavedWifiNetworks()
}

// RefreshAvahiAfterNetworkChange refreshes Avahi hostname after network changes
func (w *WifiService) RefreshAvahiAfterNetworkChange(ctx context.Context) error {
	if w.avahiService == nil {
		return fmt.Errorf("avahi service not available")
	}
	return w.avahiService.RefreshHostname(ctx)
}


