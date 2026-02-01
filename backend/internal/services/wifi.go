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
	runner *exec.Runner
	logger *zap.Logger
	db     *state.DB
}

func NewWifiService(runner *exec.Runner, logger *zap.Logger, db *state.DB) *WifiService {
	return &WifiService{runner: runner, logger: logger, db: db}
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

	// Check if we have a saved password
	if password == "" && w.db != nil {
		saved, err := w.db.GetSavedWifiNetwork(ssid)
		if err == nil && saved != nil {
			password = saved.Password
		}
	}

	args := []string{"nmcli", "device", "wifi", "connect", ssid}
	if password != "" {
		args = append(args, "password", password)
	}

	result, err := w.runner.Run(ctx, exec.RunOpts{
		JobID:   jobID,
		JobType: "wifi_connect",
		Command: "sudo",
		Args:    args,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	// Verify actual internet access
	if result.Success {
		if !w.verifyInternet() {
			result.Success = false
			result.Error = "WiFi connected but no internet access"
		} else {
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
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head("http://1.1.1.1")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
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
