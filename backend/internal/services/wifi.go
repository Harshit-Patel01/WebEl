package services

import (
	"context"
	"fmt"
	"net/http"
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
	wifiMonitor  *WifiMonitor
}

func NewWifiService(runner *exec.Runner, logger *zap.Logger, db *state.DB) *WifiService {
	ws := &WifiService{
		runner:       runner,
		logger:       logger,
		db:           db,
		avahiService: NewAvahiService(runner, logger),
		wifiMonitor:  nil, // Will be set later to avoid circular dependency
	}

	// Set high priority for all existing WiFi connections on startup
	go ws.updateExistingWifiPriorities()

	return ws
}

// SetWifiMonitor sets the WifiMonitor for this service to allow checking AP mode
func (w *WifiService) SetWifiMonitor(wifiMonitor *WifiMonitor) {
	w.wifiMonitor = wifiMonitor
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
	// Validate SSID
	if !isValidSSID(ssid) {
		return nil, fmt.Errorf("invalid SSID")
	}

	// Enable manual connect mode to prevent WiFi monitor from interfering
	if w.wifiMonitor != nil {
		w.wifiMonitor.SetManualConnectMode(90 * time.Second)
	}

	// First, bring down the hotspot if it's active to free up wlan0
	if w.wifiMonitor != nil && w.wifiMonitor.IsAPEnabled() {
		w.logger.Info("Bringing down hotspot before WiFi connection attempt")
		_, _ = w.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_down_hotspot_before_connect",
			Command: "sudo",
			Args:    []string{"nmcli", "connection", "down", "webel-hotspot"},
			Timeout: 10 * time.Second,
		})
		time.Sleep(2 * time.Second)
	}

	// Check if connection profile already exists
	checkResult, _ := w.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_check_existing",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "NAME", "connection", "show"},
		Timeout: 5 * time.Second,
	})

	connectionExists := false
	if checkResult != nil {
		for _, line := range checkResult.Lines {
			if line.Stream == "stdout" && strings.TrimSpace(line.Text) == ssid {
				connectionExists = true
				break
			}
		}
	}

	var result *exec.ExecResult
	var err error

	if connectionExists {
		// Connection exists, update password and priority, then activate
		w.logger.Info("Connection profile exists, updating credentials", zap.String("ssid", ssid))

		// Update password if provided
		if password != "" {
			_, _ = w.runner.Run(ctx, exec.RunOpts{
				JobType: "wifi_update_password",
				Command: "sudo",
				Args:    []string{"nmcli", "connection", "modify", ssid, "wifi-sec.psk", password},
				Timeout: 5 * time.Second,
			})
		}

		// Set high priority
		w.logger.Info("Setting high priority for existing connection", zap.String("ssid", ssid))
		_, _ = w.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_set_priority",
			Command: "sudo",
			Args: []string{
				"nmcli", "connection", "modify", ssid,
				"connection.autoconnect-priority", "100",
			},
			Timeout: 5 * time.Second,
		})

		// Disconnect wlan0 first
		w.logger.Info("Disconnecting wlan0 before activation")
		_, _ = w.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_disconnect_before_connect",
			Command: "sudo",
			Args:    []string{"nmcli", "device", "disconnect", "wlan0"},
			Timeout: 5 * time.Second,
		})

		time.Sleep(2 * time.Second)

		// Activate the connection
		w.logger.Info("Activating existing WiFi connection", zap.String("ssid", ssid))
		result, err = w.runner.Run(ctx, exec.RunOpts{
			JobID:   jobID,
			JobType: "wifi_connect",
			Command: "sudo",
			Args:    []string{"nmcli", "connection", "up", ssid},
			Timeout: 45 * time.Second,
		})
	} else {
		// Connection doesn't exist, create new one
		w.logger.Info("Creating new WiFi connection", zap.String("ssid", ssid))

		// Disconnect wlan0 to ensure clean state
		_, _ = w.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_disconnect_before_connect",
			Command: "sudo",
			Args:    []string{"nmcli", "device", "disconnect", "wlan0"},
			Timeout: 5 * time.Second,
		})

		time.Sleep(2 * time.Second)

		// Create and connect
		args := []string{"nmcli", "device", "wifi", "connect", ssid}
		if password != "" {
			args = append(args, "password", password)
		}

		result, err = w.runner.Run(ctx, exec.RunOpts{
			JobID:   jobID,
			JobType: "wifi_connect",
			Command: "sudo",
			Args:    args,
			Timeout: 45 * time.Second,
		})

		// Set high priority for new connection
		if err == nil && result.Success {
			w.logger.Info("Setting high priority for new connection", zap.String("ssid", ssid))
			_, _ = w.runner.Run(ctx, exec.RunOpts{
				JobType: "wifi_set_priority",
				Command: "sudo",
				Args: []string{
					"nmcli", "connection", "modify", ssid,
					"connection.autoconnect-priority", "100",
				},
				Timeout: 5 * time.Second,
			})
		}
	}

	if err != nil {
		return nil, err
	}

	// Wait for network to stabilize after connection
	if result.Success {
		w.logger.Info("WiFi connection successful, waiting for stabilization", zap.String("ssid", ssid))
		time.Sleep(5 * time.Second)
	}

	// Verify actual internet access
	if result.Success {
		// Give more time for internet to be available after connection
		w.logger.Info("Verifying internet connectivity", zap.String("ssid", ssid))
		time.Sleep(3 * time.Second)

		if !w.verifyInternet() {
			w.logger.Warn("WiFi connected but no internet access", zap.String("ssid", ssid))
			// Don't fail the connection if internet is not available
			// Some networks might have captive portals or delayed internet access
			result.Success = true
			result.Error = ""
		} else {
			w.logger.Info("WiFi connection verified with internet access", zap.String("ssid", ssid))
		}

		// Refresh Avahi to re-broadcast mDNS hostname after network change
		if err := w.avahiService.RefreshHostname(ctx); err != nil {
			w.logger.Warn("Failed to refresh Avahi hostname", zap.Error(err))
		}

		// Save the network and password on successful connection
		if w.db != nil {
			now := time.Now()
			_ = w.db.SaveWifiNetwork(&state.SavedWifiNetwork{
				SSID:            ssid,
				Password:        password,
				Security:        "WPA", // Default, could be improved by detecting actual security
				LastConnectedAt: &now,
			})
		}
	}

	return result, nil
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

func (w *WifiService) verifyInternet() bool {
	// If AP mode is enabled, the device won't have internet access by design
	// so we should return true to avoid disrupting the AP functionality
	// if w.wifiMonitor != nil && w.wifiMonitor.IsAPEnabled() {
	// 	w.logger.Debug("AP mode is enabled, skipping internet verification")
	// 	return true
	// }

	// Try multiple times with delays to allow network to stabilize
	for i := 0; i < 5; i++ {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Head("http://1.1.1.1")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return true
			}
		}
	}
	return false
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

// updateExistingWifiPriorities sets high priority for all existing WiFi connections
func (w *WifiService) updateExistingWifiPriorities() {
	ctx := context.Background()
	w.logger.Info("Updating priorities for existing WiFi connections")

	// Get all WiFi connections
	result, err := w.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_list_connections",
		Command: "sudo",
		Args:    []string{"nmcli", "-t", "-f", "NAME,TYPE", "connection", "show"},
		Timeout: 10 * time.Second,
	})

	if err != nil {
		w.logger.Error("Failed to list connections", zap.Error(err))
		return
	}

	for _, line := range result.Lines {
		if line.Stream != "stdout" || line.Text == "" {
			continue
		}

		parts := strings.SplitN(line.Text, ":", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		connType := strings.TrimSpace(parts[1])

		// Skip non-WiFi connections and the AP hotspot
		if connType != "wifi" || name == "webel-hotspot" || name == "Hotspot" {
			continue
		}

		// Set high priority for this WiFi connection
		w.logger.Info("Setting high priority for existing WiFi connection", zap.String("name", name))
		_, err := w.runner.Run(ctx, exec.RunOpts{
			JobType: "wifi_update_priority",
			Command: "sudo",
			Args: []string{
				"nmcli", "connection", "modify", name,
				"connection.autoconnect-priority", "100",
			},
			Timeout: 5 * time.Second,
		})

		if err != nil {
			w.logger.Warn("Failed to update priority for connection",
				zap.String("name", name),
				zap.Error(err))
		}
	}

	w.logger.Info("Finished updating WiFi connection priorities")
}
