package services

import (
	"context"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"go.uber.org/zap"
)

type WifiMonitor struct {
	runner     *exec.Runner
	wifiSvc    *WifiService
	logger     *zap.Logger
	stopChan   chan struct{}
	apEnabled  bool
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

	// Check immediately on start
	m.checkAndCreateAP(ctx)

	// Then check every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
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

func (m *WifiMonitor) checkAndCreateAP(ctx context.Context) {
	status, err := m.wifiSvc.GetStatus(ctx)
	if err != nil {
		m.logger.Error("Failed to get WiFi status", zap.Error(err))
		return
	}

	// If connected, disable AP if it's running
	if status.Connected {
		if m.apEnabled {
			m.logger.Info("WiFi connected, disabling fallback AP")
			m.disableAP(ctx)
			m.apEnabled = false
		}
		return
	}

	// Not connected, enable AP if not already running
	if !m.apEnabled {
		m.logger.Info("WiFi not connected, enabling fallback AP")
		if err := m.enableAP(ctx); err != nil {
			m.logger.Error("Failed to enable fallback AP", zap.Error(err))
		} else {
			m.apEnabled = true
		}
	}
}

func (m *WifiMonitor) enableAP(ctx context.Context) error {
	// Create a hotspot with NetworkManager
	_, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_enable",
		Command: "sudo",
		Args: []string{
			"nmcli", "device", "wifi", "hotspot",
			"ifname", "wlan0",
			"ssid", "webel",
			"password", "webel123",
		},
		Timeout: 15 * time.Second,
	})

	if err != nil {
		return err
	}

	m.logger.Info("Fallback AP 'webel' enabled")
	return nil
}

func (m *WifiMonitor) disableAP(ctx context.Context) error {
	// Stop the hotspot
	_, err := m.runner.Run(ctx, exec.RunOpts{
		JobType: "wifi_ap_disable",
		Command: "sudo",
		Args:    []string{"nmcli", "connection", "down", "Hotspot"},
		Timeout: 10 * time.Second,
	})

	return err
}
