package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/opendeploy/opendeploy/internal/exec"
	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

const (
	apInterface   = "ap0"
	apIPAddress   = "192.168.4.1/24"
	apIPBase      = "192.168.4.1"
	apDHCPStart   = "192.168.4.2"
	apDHCPEnd     = "192.168.4.20"
	mainInterface = "wlan0"
)

// WifiAP manages a permanent virtual WiFi access point on a separate interface.
type WifiAP struct {
	runner  *exec.Runner
	logger  *zap.Logger
	db      *state.DB
	running bool
}

func NewWifiAP(runner *exec.Runner, logger *zap.Logger, db *state.DB) *WifiAP {
	return &WifiAP{
		runner: runner,
		logger: logger,
		db:     db,
	}
}

// EnsureAP checks the current state and brings up the AP if it's configured as enabled.
// Called once on startup. It is idempotent.
func (ap *WifiAP) EnsureAP(ctx context.Context) {
	cfg, err := ap.db.GetAPConfig()
	if err != nil {
		ap.logger.Error("Failed to load AP config", zap.Error(err))
		return
	}

	if !cfg.Enabled {
		ap.logger.Info("AP is disabled in config, skipping setup")
		return
	}

	ap.logger.Info("Ensuring AP is running", zap.String("ssid", cfg.SSID))

	// Check if AP interface already exists and hostapd is running
	if ap.isAPRunning(ctx) {
		ap.logger.Info("AP is already running")
		ap.running = true
		// Still ensure NAT forwarding is set up
		ap.setupNATForwarding(ctx)
		return
	}

	if err := ap.startAP(ctx, cfg); err != nil {
		ap.logger.Error("Failed to start AP", zap.Error(err))
		return
	}

	ap.running = true
	ap.logger.Info("AP started successfully",
		zap.String("ssid", cfg.SSID),
		zap.String("interface", apInterface),
		zap.String("ip", apIPBase))
}

// isAPRunning checks if the virtual interface exists and hostapd is active.
func (ap *WifiAP) isAPRunning(ctx context.Context) bool {
	// Check if the ap0 interface exists
	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "check_ap_iface",
		Command: "ip",
		Args:    []string{"link", "show", apInterface},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		return false
	}

	// Check if hostapd is active
	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "check_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "hostapd"},
		Timeout: 5 * time.Second,
	})
	if err != nil || result == nil {
		return false
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" && strings.TrimSpace(line.Text) == "active" {
			return true
		}
	}
	return false
}

// getSTAChannel returns the channel that wlan0 is currently operating on.
// When running AP+STA concurrently on the same radio, both must use the same channel.
func (ap *WifiAP) getSTAChannel(ctx context.Context) int {
	// Get the frequency of the current wlan0 connection
	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "get_sta_freq",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("iw dev %s link 2>/dev/null | grep -i freq | head -1 | awk '{print $2}'", mainInterface)},
		Timeout: 5 * time.Second,
	})
	if err != nil || result == nil {
		return 0
	}

	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			freqStr := strings.TrimSpace(line.Text)
			if freqStr == "" {
				return 0
			}
			freq, err := strconv.Atoi(freqStr)
			if err != nil {
				return 0
			}
			return freqToChannel(freq)
		}
	}
	return 0
}

// freqToChannel converts a WiFi frequency (MHz) to a channel number.
func freqToChannel(freq int) int {
	if freq >= 2412 && freq <= 2484 {
		if freq == 2484 {
			return 14
		}
		return (freq - 2412) / 5 + 1
	}
	// 5 GHz band
	if freq >= 5180 && freq <= 5825 {
		return (freq - 5000) / 5
	}
	return 0
}

// getMainMAC returns the MAC address of the main interface.
func (ap *WifiAP) getMainMAC(ctx context.Context) string {
	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "get_main_mac",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("cat /sys/class/net/%s/address 2>/dev/null", mainInterface)},
		Timeout: 5 * time.Second,
	})
	if err != nil || result == nil {
		return ""
	}
	for _, line := range result.Lines {
		if line.Stream == "stdout" {
			return strings.TrimSpace(line.Text)
		}
	}
	return ""
}

// generateAPMAC creates a unique MAC address for the AP interface by incrementing
// the last byte of the main interface MAC. The first byte must remain even (unicast).
func generateAPMAC(mainMAC string) string {
	parts := strings.Split(mainMAC, ":")
	if len(parts) != 6 {
		return ""
	}

	// Increment last byte
	lastByte, err := strconv.ParseUint(parts[5], 16, 8)
	if err != nil {
		return ""
	}
	lastByte = (lastByte + 1) & 0xFF
	parts[5] = fmt.Sprintf("%02x", lastByte)

	// Ensure first byte is even (unicast) and has local bit set
	firstByte, err := strconv.ParseUint(parts[0], 16, 8)
	if err != nil {
		return ""
	}
	firstByte = (firstByte | 0x02) & 0xFE // Set local bit, clear multicast bit
	parts[0] = fmt.Sprintf("%02x", firstByte)

	return strings.Join(parts, ":")
}

// startAP creates the virtual interface, writes configs, and starts hostapd + dnsmasq.
// This method properly configures ALL required system files:
//   - /etc/dhcpcd.conf        → static IP for ap0 (prevents dhcpcd from requesting DHCP on ap0)
//   - /etc/dnsmasq.conf       → main dnsmasq config (ensures conf-dir is enabled)
//   - /etc/dnsmasq.d/webel-ap.conf → AP-specific DHCP/DNS config
//   - /etc/hostapd/hostapd.conf    → hostapd AP config
//   - /etc/default/hostapd         → DAEMON_CONF pointer
func (ap *WifiAP) startAP(ctx context.Context, cfg *state.APConfig) error {
	ap.logger.Info("Creating virtual AP interface", zap.String("interface", apInterface))

	// ── Step 0: Determine channel — must match STA channel if connected ──
	channel := cfg.Channel
	staChannel := ap.getSTAChannel(ctx)
	if staChannel > 0 {
		ap.logger.Info("STA connected, forcing AP to same channel",
			zap.Int("sta_channel", staChannel),
			zap.Int("configured_channel", channel))
		channel = staChannel
	}

	// ── Step 1: Get main interface MAC for generating AP MAC ──
	mainMAC := ap.getMainMAC(ctx)
	apMAC := ""
	if mainMAC != "" {
		apMAC = generateAPMAC(mainMAC)
		ap.logger.Info("Generated AP MAC", zap.String("main_mac", mainMAC), zap.String("ap_mac", apMAC))
	}

	// ── Step 2: Stop services FIRST before touching interfaces/configs ──
	ap.logger.Info("Stopping existing services before reconfiguration")
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "hostapd"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "dnsmasq"},
		Timeout: 5 * time.Second,
	})
	time.Sleep(1 * time.Second)

	// ── Step 3: Remove old ap0 if it exists (clean slate) ──
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "cleanup_old_ap",
		Command: "sudo",
		Args:    []string{"iw", "dev", apInterface, "del"},
		Timeout: 5 * time.Second,
	})
	time.Sleep(500 * time.Millisecond)

	// ── Step 4: Create virtual interface using iw phy ──
	phyResult, _ := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "get_phy",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("cat /sys/class/net/%s/phy80211/name 2>/dev/null || echo phy0", mainInterface)},
		Timeout: 5 * time.Second,
	})
	phyName := "phy0"
	if phyResult != nil {
		for _, line := range phyResult.Lines {
			if line.Stream == "stdout" && strings.TrimSpace(line.Text) != "" {
				phyName = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	ap.logger.Info("Creating AP interface on phy", zap.String("phy", phyName))
	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "create_ap_iface",
		Command: "sudo",
		Args:    []string{"iw", "phy", phyName, "interface", "add", apInterface, "type", "__ap"},
		Timeout: 10 * time.Second,
	})
	if err != nil || (result != nil && !result.Success) {
		ap.logger.Warn("iw phy failed, trying iw dev fallback")
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "create_ap_iface_fallback",
			Command: "sudo",
			Args:    []string{"iw", "dev", mainInterface, "interface", "add", apInterface, "type", "__ap"},
			Timeout: 10 * time.Second,
		})
	}
	time.Sleep(1 * time.Second)

	// ── Step 5: Set unique MAC address on ap0 to avoid conflicts ──
	if apMAC != "" {
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "set_ap_mac",
			Command: "sudo",
			Args:    []string{"ip", "link", "set", "dev", apInterface, "address", apMAC},
			Timeout: 5 * time.Second,
		})
	}

	// ── Step 6: Tell NetworkManager to NOT manage ap0 ──
	ap.markUnmanagedByNM(ctx)

	// ── Step 7: Configure /etc/dhcpcd.conf — CRITICAL for IP assignment ──
	// On Raspberry Pi, dhcpcd is the DHCP client daemon that manages interface IPs.
	// Without a static IP block for ap0, dhcpcd will either:
	//   a) Try to get a DHCP lease on ap0 (which fails — no DHCP server on that network)
	//   b) Override the manual `ip addr add` we set later
	// We need: static ip_address, nohook wpa_supplicant (ap0 uses hostapd, not wpa_supplicant)
	ap.configureDhcpcd(ctx)

	// ── Step 8: Restart dhcpcd so the static IP takes effect on ap0 ──
	ap.logger.Info("Restarting dhcpcd to apply static IP for ap0")
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "restart_dhcpcd",
		Command: "sudo",
		Args:    []string{"systemctl", "restart", "dhcpcd"},
		Timeout: 15 * time.Second,
	})
	time.Sleep(3 * time.Second)

	// ── Step 9: Bring interface up and verify IP ──
	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "ap_iface_up",
		Command: "sudo",
		Args:    []string{"ip", "link", "set", apInterface, "up"},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		ap.logger.Error("Failed to bring up AP interface")
		return fmt.Errorf("failed to bring up %s", apInterface)
	}
	time.Sleep(1 * time.Second)

	// Verify the IP is assigned (dhcpcd should have done this, but fallback to manual)
	ap.ensureAPIP(ctx)

	// ── Step 10: Write hostapd config ──
	hostapdConfig := fmt.Sprintf(`interface=%s
driver=nl80211
ssid=%s
hw_mode=g
channel=%d
wmm_enabled=0
macaddr_acl=0
auth_algs=1
ignore_broadcast_ssid=0
wpa=2
wpa_passphrase=%s
wpa_key_mgmt=WPA-PSK
wpa_pairwise=TKIP
rsn_pairwise=CCMP`, apInterface, cfg.SSID, channel, cfg.Password)

	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "write_hostapd",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("echo '%s' | sudo tee /etc/hostapd/hostapd.conf > /dev/null", hostapdConfig)},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		ap.logger.Error("Failed to write hostapd config")
		return fmt.Errorf("failed to write hostapd config")
	}

	// Ensure DAEMON_CONF is set in /etc/default/hostapd
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "set_hostapd_default",
		Command: "bash",
		Args:    []string{"-c", `sudo sed -i 's|^#\?DAEMON_CONF=.*|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd 2>/dev/null || echo 'DAEMON_CONF="/etc/hostapd/hostapd.conf"' | sudo tee /etc/default/hostapd > /dev/null`},
		Timeout: 5 * time.Second,
	})

	// ── Step 11: Configure dnsmasq — both main config AND AP drop-in ──
	ap.configureDnsmasq(ctx)

	// ── Step 12: Prevent dnsmasq from conflicting with systemd-resolved on port 53 ──
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "disable_resolved_stub",
		Command: "bash",
		Args: []string{"-c", `
			if systemctl is-active --quiet systemd-resolved 2>/dev/null; then
				sudo mkdir -p /etc/systemd/resolved.conf.d
				echo -e "[Resolve]\nDNSStubListener=no" | sudo tee /etc/systemd/resolved.conf.d/no-stub.conf > /dev/null
				sudo systemctl restart systemd-resolved 2>/dev/null || true
			fi
		`},
		Timeout: 10 * time.Second,
	})

	time.Sleep(1 * time.Second)

	// ── Step 13: Unmask and start hostapd ──
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "unmask_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "unmask", "hostapd"},
		Timeout: 5 * time.Second,
	})

	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "start_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "start", "hostapd"},
		Timeout: 10 * time.Second,
	})
	if err != nil || !result.Success {
		ap.logger.Error("Failed to start hostapd")
		return fmt.Errorf("failed to start hostapd")
	}

	time.Sleep(3 * time.Second)

	// Verify hostapd
	checkResult, _ := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "verify_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "is-active", "hostapd"},
		Timeout: 5 * time.Second,
	})
	hostapdActive := ""
	if checkResult != nil {
		for _, line := range checkResult.Lines {
			if line.Stream == "stdout" {
				hostapdActive = strings.TrimSpace(line.Text)
				break
			}
		}
	}
	if hostapdActive != "active" {
		ap.logger.Error("hostapd not active after start", zap.String("status", hostapdActive))
		return fmt.Errorf("hostapd failed to start (status: %s)", hostapdActive)
	}

	// ── Step 14: Enable and start dnsmasq ──
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "enable_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "enable", "dnsmasq"},
		Timeout: 5 * time.Second,
	})
	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "start_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "restart", "dnsmasq"},
		Timeout: 10 * time.Second,
	})
	if err != nil || !result.Success {
		ap.logger.Warn("dnsmasq systemctl restart failed, trying manual start")
		// Fallback: kill any stale dnsmasq and start fresh
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "kill_dnsmasq",
			Command: "sudo",
			Args:    []string{"killall", "-9", "dnsmasq"},
			Timeout: 5 * time.Second,
		})
		time.Sleep(1 * time.Second)
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "start_dnsmasq_manual",
			Command: "sudo",
			Args:    []string{"dnsmasq", "--conf-file=/etc/dnsmasq.conf"},
			Timeout: 10 * time.Second,
		})
	}

	// Verify dnsmasq is actually running and serving DHCP
	time.Sleep(2 * time.Second)
	ap.verifyDnsmasq(ctx)

	// ── Step 15: Setup NAT forwarding ──
	ap.setupNATForwarding(ctx)

	return nil
}

// configureDhcpcd adds a static IP block for ap0 in /etc/dhcpcd.conf.
// This is CRITICAL on Raspberry Pi — without it, dhcpcd tries to get a DHCP lease
// on ap0 which fails (no DHCP server) and may override manually assigned IPs.
// The block is idempotent: if the ap0 section already exists, it's replaced.
func (ap *WifiAP) configureDhcpcd(ctx context.Context) {
	ap.logger.Info("Configuring /etc/dhcpcd.conf with static IP for ap0")

	// The dhcpcd.conf block we need:
	//   interface ap0
	//       static ip_address=192.168.4.1/24
	//       nohook wpa_supplicant
	//
	// "nohook wpa_supplicant" prevents dhcpcd from invoking wpa_supplicant on ap0
	// (we use hostapd instead). "static ip_address" prevents DHCP client from running.

	// Use a script that:
	// 1. Removes any existing ap0 block (between markers)
	// 2. Appends the new block with markers for easy identification
	script := fmt.Sprintf(`
# Remove existing ap0 configuration block (between markers) if present
sudo sed -i '/^# BEGIN WEBEL AP0/,/^# END WEBEL AP0/d' /etc/dhcpcd.conf 2>/dev/null

# Also remove any bare "interface ap0" blocks that might exist without markers
# This handles manual edits or configs from other tools
sudo python3 -c "
import re
try:
    with open('/etc/dhcpcd.conf', 'r') as f:
        content = f.read()
    # Remove 'interface ap0' and all indented lines following it
    content = re.sub(r'\ninterface %s\n(?:[ \t]+[^\n]*\n)*', '\n', content)
    content = re.sub(r'^interface %s\n(?:[ \t]+[^\n]*\n)*', '', content)
    with open('/tmp/dhcpcd_cleaned.conf', 'w') as f:
        f.write(content)
except Exception as e:
    print(f'Python cleanup failed: {e}')
    import shutil
    shutil.copy('/etc/dhcpcd.conf', '/tmp/dhcpcd_cleaned.conf')
" 2>/dev/null && sudo cp /tmp/dhcpcd_cleaned.conf /etc/dhcpcd.conf

# Append the static IP block with markers
cat <<'DHCPCD_BLOCK' | sudo tee -a /etc/dhcpcd.conf > /dev/null

# BEGIN WEBEL AP0
interface %s
    static ip_address=%s
    nohook wpa_supplicant
# END WEBEL AP0
DHCPCD_BLOCK
`, apInterface, apInterface, apInterface, apIPAddress)

	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "configure_dhcpcd",
		Command: "bash",
		Args:    []string{"-c", script},
		Timeout: 10 * time.Second,
	})
	if err != nil || (result != nil && !result.Success) {
		ap.logger.Warn("dhcpcd.conf configuration may have issues, falling back to simple append")
		// Fallback: just append if the script failed
		fallbackScript := fmt.Sprintf(`
grep -q 'interface %s' /etc/dhcpcd.conf 2>/dev/null || cat <<'EOF' | sudo tee -a /etc/dhcpcd.conf > /dev/null

# BEGIN WEBEL AP0
interface %s
    static ip_address=%s
    nohook wpa_supplicant
# END WEBEL AP0
EOF
`, apInterface, apInterface, apIPAddress)
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "configure_dhcpcd_fallback",
			Command: "bash",
			Args:    []string{"-c", fallbackScript},
			Timeout: 10 * time.Second,
		})
	}

	ap.logger.Info("dhcpcd.conf configured with static IP for ap0",
		zap.String("ip", apIPAddress))
}

// configureDnsmasq sets up BOTH the main /etc/dnsmasq.conf and the AP-specific
// drop-in config in /etc/dnsmasq.d/webel-ap.conf.
// On many Raspberry Pi setups, the main dnsmasq.conf doesn't include conf-dir,
// so drop-in files in dnsmasq.d/ are silently ignored. We fix both.
func (ap *WifiAP) configureDnsmasq(ctx context.Context) {
	ap.logger.Info("Configuring dnsmasq for AP DHCP/DNS")

	// ── Part A: Ensure /etc/dnsmasq.conf has conf-dir enabled ──
	// Check if conf-dir line exists and is uncommented
	ensureConfDirScript := `
# Ensure the conf-dir directive exists and is uncommented in /etc/dnsmasq.conf
if [ -f /etc/dnsmasq.conf ]; then
    # Check if conf-dir is already uncommented and pointing to dnsmasq.d
    if grep -qE '^\s*conf-dir\s*=\s*/etc/dnsmasq\.d' /etc/dnsmasq.conf; then
        echo "conf-dir already enabled"
    elif grep -qE '^\s*#\s*conf-dir\s*=\s*/etc/dnsmasq\.d' /etc/dnsmasq.conf; then
        # Uncomment the existing line
        sudo sed -i 's|^\s*#\s*\(conf-dir\s*=\s*/etc/dnsmasq\.d.*\)|\1|' /etc/dnsmasq.conf
        echo "conf-dir uncommented"
    else
        # Add conf-dir at the end
        echo 'conf-dir=/etc/dnsmasq.d/,*.conf' | sudo tee -a /etc/dnsmasq.conf > /dev/null
        echo "conf-dir added"
    fi
else
    # Create minimal dnsmasq.conf
    echo 'conf-dir=/etc/dnsmasq.d/,*.conf' | sudo tee /etc/dnsmasq.conf > /dev/null
    echo "dnsmasq.conf created"
fi

# Also ensure port=0 is NOT set (that disables DNS entirely)
sudo sed -i 's/^\s*port\s*=\s*0/#port=0/' /etc/dnsmasq.conf 2>/dev/null || true

# Ensure bind-dynamic or bind-interfaces is NOT set globally (we set it per-interface in drop-in)
# Leave global config alone — our drop-in handles interface binding
`
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "ensure_dnsmasq_confdir",
		Command: "bash",
		Args:    []string{"-c", ensureConfDirScript},
		Timeout: 10 * time.Second,
	})

	// ── Part B: Ensure /etc/dnsmasq.d/ directory exists ──
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "mkdir_dnsmasq_d",
		Command: "sudo",
		Args:    []string{"mkdir", "-p", "/etc/dnsmasq.d"},
		Timeout: 5 * time.Second,
	})

	// ── Part C: Write the AP-specific dnsmasq drop-in config ──
	dnsmasqConfig := fmt.Sprintf(`# WebEL AP dnsmasq config — managed by opendeploy
# DO NOT EDIT — this file is overwritten on AP startup

# Only bind to the AP interface (prevents conflicts with systemd-resolved, etc.)
bind-interfaces
listen-address=%s

# Serve DHCP only on the AP interface
interface=%s
no-dhcp-interface=lo
except-interface=lo

# DHCP range: .2 to .20, lease time 12 hours
dhcp-range=%s,%s,255.255.255.0,12h

# DHCP options:
#   option 3 = default gateway (route traffic through this device)
#   option 6 = DNS server (resolve names through this device)
dhcp-option=3,%s
dhcp-option=6,%s

# Authoritative mode — be the only DHCP server on this network
# This makes dnsmasq respond faster (no wait for other servers)
dhcp-authoritative

# Local domain
domain=wlan
local=/wlan/

# Resolve webel.local to the AP IP
address=/webel.local/%s

# Forward DNS to upstream when internet is available
server=1.1.1.1
server=8.8.8.8

# Logging for debugging (can be removed later)
log-dhcp
log-queries
log-facility=/var/log/dnsmasq-webel.log
`, apIPBase, apInterface, apDHCPStart, apDHCPEnd, apIPBase, apIPBase, apIPBase)

	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "write_dnsmasq_ap",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("cat <<'DNSMASQ_EOF' | sudo tee /etc/dnsmasq.d/webel-ap.conf > /dev/null\n%sDNSMASQ_EOF", dnsmasqConfig)},
		Timeout: 5 * time.Second,
	})
	if err != nil || (result != nil && !result.Success) {
		ap.logger.Error("Failed to write dnsmasq drop-in config")
		return
	}

	// ── Part D: Remove any OTHER dnsmasq configs that might conflict ──
	// (e.g., old configs binding to all interfaces or using same DHCP range)
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "cleanup_dnsmasq_conflicts",
		Command: "bash",
		Args: []string{"-c", `
			# Remove any other configs in dnsmasq.d that bind to the same interface
			for f in /etc/dnsmasq.d/*.conf; do
				[ "$f" = "/etc/dnsmasq.d/webel-ap.conf" ] && continue
				if grep -q "interface=ap0" "$f" 2>/dev/null; then
					sudo rm -f "$f"
				fi
			done
		`},
		Timeout: 5 * time.Second,
	})

	ap.logger.Info("dnsmasq configuration complete",
		zap.String("drop_in", "/etc/dnsmasq.d/webel-ap.conf"),
		zap.String("dhcp_range", apDHCPStart+"-"+apDHCPEnd))
}

// ensureAPIP verifies that ap0 has the correct static IP. If dhcpcd didn't assign it
// (e.g., dhcpcd not installed or not managing ap0), we manually assign it.
func (ap *WifiAP) ensureAPIP(ctx context.Context) {
	// Check if ap0 already has the correct IP
	result, err := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "check_ap_ip",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("ip addr show dev %s 2>/dev/null | grep -q '%s'", apInterface, apIPBase)},
		Timeout: 5 * time.Second,
	})
	if err == nil && result != nil && result.Success {
		ap.logger.Info("AP interface already has correct IP", zap.String("ip", apIPBase))
		return
	}

	// IP not assigned — do it manually
	ap.logger.Warn("dhcpcd did not assign IP to ap0, assigning manually")

	// Flush any wrong IPs first
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "flush_ap_ip",
		Command: "sudo",
		Args:    []string{"ip", "addr", "flush", "dev", apInterface},
		Timeout: 5 * time.Second,
	})

	result, err = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "set_ap_ip",
		Command: "sudo",
		Args:    []string{"ip", "addr", "add", apIPAddress, "dev", apInterface},
		Timeout: 5 * time.Second,
	})
	if err != nil || !result.Success {
		ap.logger.Error("Failed to manually set AP IP — clients will NOT get DHCP leases!")
	} else {
		ap.logger.Info("Manually assigned IP to ap0", zap.String("ip", apIPAddress))
	}
}

// verifyDnsmasq checks that dnsmasq is actually running and listening on the AP interface.
func (ap *WifiAP) verifyDnsmasq(ctx context.Context) {
	// Check if dnsmasq process is running
	result, _ := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "verify_dnsmasq_proc",
		Command: "bash",
		Args:    []string{"-c", "pgrep -x dnsmasq > /dev/null 2>&1 && echo running || echo stopped"},
		Timeout: 5 * time.Second,
	})
	dnsmasqStatus := "unknown"
	if result != nil {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				dnsmasqStatus = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	if dnsmasqStatus != "running" {
		ap.logger.Error("dnsmasq is NOT running — DHCP will not work for AP clients!")
		return
	}

	// Check if dnsmasq is listening on the AP IP
	result, _ = ap.runner.Run(ctx, exec.RunOpts{
		JobType: "verify_dnsmasq_listen",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("sudo ss -ulnp | grep -q '%s:' && echo listening || echo not_listening", apIPBase)},
		Timeout: 5 * time.Second,
	})
	listenStatus := "unknown"
	if result != nil {
		for _, line := range result.Lines {
			if line.Stream == "stdout" {
				listenStatus = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	if listenStatus == "listening" {
		ap.logger.Info("dnsmasq verified: running and listening on AP interface",
			zap.String("ip", apIPBase))
	} else {
		ap.logger.Warn("dnsmasq running but may not be listening on AP interface",
			zap.String("listen_status", listenStatus))
	}
}

// markUnmanagedByNM tells NetworkManager to ignore the AP interface so it doesn't
// interfere with hostapd.
func (ap *WifiAP) markUnmanagedByNM(ctx context.Context) {
	ap.logger.Info("Marking AP interface as unmanaged by NetworkManager")

	// Method 1: nmcli — directly set the device as unmanaged
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "nm_unmanage_ap",
		Command: "sudo",
		Args:    []string{"nmcli", "device", "set", apInterface, "managed", "no"},
		Timeout: 5 * time.Second,
	})

	// Method 2: Drop a config file — persistent across reboots
	nmConfig := fmt.Sprintf(`[keyfile]
unmanaged-devices=interface-name:%s`, apInterface)
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "nm_unmanage_conf",
		Command: "bash",
		Args:    []string{"-c", fmt.Sprintf("echo '%s' | sudo tee /etc/NetworkManager/conf.d/webel-ap-unmanaged.conf > /dev/null && sudo nmcli general reload conf 2>/dev/null || true", nmConfig)},
		Timeout: 5 * time.Second,
	})
}

// setupNATForwarding enables IP forwarding and masquerading so AP clients can access the internet
// through the main interface when it is connected. Dynamically detects the active internet interface.
func (ap *WifiAP) setupNATForwarding(ctx context.Context) {
	ap.logger.Info("Setting up NAT forwarding for AP")

	// Enable IP forwarding
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "enable_ip_forward",
		Command: "sudo",
		Args:    []string{"sysctl", "-w", "net.ipv4.ip_forward=1"},
		Timeout: 5 * time.Second,
	})

	// Make it persistent
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "persist_ip_forward",
		Command: "bash",
		Args:    []string{"-c", `sudo sed -i 's/^#net.ipv4.ip_forward=1/net.ipv4.ip_forward=1/' /etc/sysctl.conf 2>/dev/null; grep -q '^net.ipv4.ip_forward=1' /etc/sysctl.conf || echo 'net.ipv4.ip_forward=1' | sudo tee -a /etc/sysctl.conf > /dev/null`},
		Timeout: 5 * time.Second,
	})

	// Detect active internet interface (prefer wlan0, but fallback to any with default route)
	detectResult, _ := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "detect_internet_iface",
		Command: "bash",
		Args: []string{"-c", `
			# Check if wlan0 has a default route
			if ip route | grep -q "^default.*dev wlan0"; then
				echo "wlan0"
			else
				# Find any interface with default route (excluding ap0)
				ip route | grep "^default" | awk '{print $5}' | grep -v "^ap0$" | head -1
			fi
		`},
		Timeout: 5 * time.Second,
	})

	activeInterface := mainInterface // fallback to wlan0
	if detectResult != nil && detectResult.Success {
		for _, line := range detectResult.Lines {
			if line.Stream == "stdout" && line.Text != "" {
				activeInterface = strings.TrimSpace(line.Text)
				break
			}
		}
	}

	ap.logger.Info("Using interface for NAT", zap.String("interface", activeInterface))

	// Set up iptables masquerading (NAT from ap0 subnet through active interface)
	// First remove any existing rules to avoid duplicates (try both wlan0 and detected interface)
	for _, iface := range []string{"wlan0", activeInterface} {
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "remove_old_nat",
			Command: "sudo",
			Args:    []string{"iptables", "-t", "nat", "-D", "POSTROUTING", "-s", "192.168.4.0/24", "-o", iface, "-j", "MASQUERADE"},
			Timeout: 5 * time.Second,
		})
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "remove_old_forward1",
			Command: "sudo",
			Args:    []string{"iptables", "-D", "FORWARD", "-i", iface, "-o", apInterface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
			Timeout: 5 * time.Second,
		})
		ap.runner.Run(ctx, exec.RunOpts{
			JobType: "remove_old_forward2",
			Command: "sudo",
			Args:    []string{"iptables", "-D", "FORWARD", "-i", apInterface, "-o", iface, "-j", "ACCEPT"},
			Timeout: 5 * time.Second,
		})
	}

	// Add NAT rules for the active interface
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "add_nat",
		Command: "sudo",
		Args:    []string{"iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "192.168.4.0/24", "-o", activeInterface, "-j", "MASQUERADE"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "add_forward1",
		Command: "sudo",
		Args:    []string{"iptables", "-A", "FORWARD", "-i", activeInterface, "-o", apInterface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "add_forward2",
		Command: "sudo",
		Args:    []string{"iptables", "-A", "FORWARD", "-i", apInterface, "-o", activeInterface, "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})

	// Allow AP clients to reach the web GUI on this device (INPUT chain)
	// Remove old rule first
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_old_input",
		Command: "sudo",
		Args:    []string{"iptables", "-D", "INPUT", "-i", apInterface, "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "add_input",
		Command: "sudo",
		Args:    []string{"iptables", "-A", "INPUT", "-i", apInterface, "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})

	// Save iptables rules to persist across reboots
	ap.saveIptablesRules(ctx)
}

// saveIptablesRules persists iptables rules across reboots using iptables-persistent or manual save
func (ap *WifiAP) saveIptablesRules(ctx context.Context) {
	// Try iptables-persistent first (Debian/Ubuntu/Raspberry Pi OS)
	result, _ := ap.runner.Run(ctx, exec.RunOpts{
		JobType: "save_iptables_persistent",
		Command: "bash",
		Args: []string{"-c", `
			if command -v netfilter-persistent >/dev/null 2>&1; then
				sudo netfilter-persistent save
			elif command -v iptables-save >/dev/null 2>&1; then
				sudo iptables-save | sudo tee /etc/iptables/rules.v4 > /dev/null
			fi
		`},
		Timeout: 10 * time.Second,
	})
	if result != nil && result.Success {
		ap.logger.Info("iptables rules saved for persistence")
	}
}

// StopAP tears down the access point and cleans up system configurations.
func (ap *WifiAP) StopAP(ctx context.Context) error {
	ap.logger.Info("Stopping AP")

	// Stop services
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_hostapd",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "hostapd"},
		Timeout: 10 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "stop_dnsmasq",
		Command: "sudo",
		Args:    []string{"systemctl", "stop", "dnsmasq"},
		Timeout: 10 * time.Second,
	})

	// Remove virtual interface
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_ap_iface",
		Command: "sudo",
		Args:    []string{"iw", "dev", apInterface, "del"},
		Timeout: 5 * time.Second,
	})

	// Clean up dhcpcd.conf — remove the ap0 static IP block
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "cleanup_dhcpcd",
		Command: "bash",
		Args:    []string{"-c", "sudo sed -i '/^# BEGIN WEBEL AP0/,/^# END WEBEL AP0/d' /etc/dhcpcd.conf 2>/dev/null || true"},
		Timeout: 5 * time.Second,
	})

	// Restart dhcpcd to pick up the removal
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "restart_dhcpcd_cleanup",
		Command: "sudo",
		Args:    []string{"systemctl", "restart", "dhcpcd"},
		Timeout: 10 * time.Second,
	})

	// Remove NAT rules
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_nat",
		Command: "sudo",
		Args:    []string{"iptables", "-t", "nat", "-D", "POSTROUTING", "-s", "192.168.4.0/24", "-o", mainInterface, "-j", "MASQUERADE"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_forward1",
		Command: "sudo",
		Args:    []string{"iptables", "-D", "FORWARD", "-i", mainInterface, "-o", apInterface, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_forward2",
		Command: "sudo",
		Args:    []string{"iptables", "-D", "FORWARD", "-i", apInterface, "-o", mainInterface, "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})
	ap.runner.Run(ctx, exec.RunOpts{
		JobType: "remove_input",
		Command: "sudo",
		Args:    []string{"iptables", "-D", "INPUT", "-i", apInterface, "-j", "ACCEPT"},
		Timeout: 5 * time.Second,
	})

	ap.running = false
	return nil
}

// EnableAP enables the AP and starts it.
func (ap *WifiAP) EnableAP(ctx context.Context) error {
	cfg, err := ap.db.GetAPConfig()
	if err != nil {
		return fmt.Errorf("loading AP config: %w", err)
	}

	cfg.Enabled = true
	if err := ap.db.SaveAPConfig(cfg); err != nil {
		return fmt.Errorf("saving AP config: %w", err)
	}

	if err := ap.startAP(ctx, cfg); err != nil {
		return fmt.Errorf("starting AP: %w", err)
	}

	ap.running = true
	return nil
}

// DisableAP disables the AP and stops it.
func (ap *WifiAP) DisableAP(ctx context.Context) error {
	cfg, err := ap.db.GetAPConfig()
	if err != nil {
		return fmt.Errorf("loading AP config: %w", err)
	}

	cfg.Enabled = false
	if err := ap.db.SaveAPConfig(cfg); err != nil {
		return fmt.Errorf("saving AP config: %w", err)
	}

	return ap.StopAP(ctx)
}

// UpdateConfig updates the AP configuration and restarts if running.
func (ap *WifiAP) UpdateConfig(ctx context.Context, ssid, password string, channel int) error {
	cfg, err := ap.db.GetAPConfig()
	if err != nil {
		return fmt.Errorf("loading AP config: %w", err)
	}

	if ssid != "" {
		cfg.SSID = ssid
	}
	if password != "" {
		cfg.Password = password
	}
	if channel > 0 && channel <= 14 {
		cfg.Channel = channel
	}

	if err := ap.db.SaveAPConfig(cfg); err != nil {
		return fmt.Errorf("saving AP config: %w", err)
	}

	// If AP is enabled and running, restart with new config
	if cfg.Enabled && ap.running {
		ap.logger.Info("Restarting AP with updated config")
		ap.StopAP(ctx)
		time.Sleep(2 * time.Second)
		if err := ap.startAP(ctx, cfg); err != nil {
			return fmt.Errorf("restarting AP: %w", err)
		}
		ap.running = true
	}

	return nil
}

// GetConfig returns the current AP config from the database.
func (ap *WifiAP) GetConfig() (*state.APConfig, error) {
	return ap.db.GetAPConfig()
}

// APStatus represents the runtime status of the access point.
type APStatus struct {
	Running        bool   `json:"running"`
	Interface      string `json:"interface"`
	SSID           string `json:"ssid"`
	IPAddress      string `json:"ip_address"`
	ConnectedCount int    `json:"connected_clients"`
	Enabled        bool   `json:"enabled"`
}

// GetStatus returns the current runtime status of the AP.
func (ap *WifiAP) GetStatus(ctx context.Context) (*APStatus, error) {
	cfg, err := ap.db.GetAPConfig()
	if err != nil {
		return nil, fmt.Errorf("loading AP config: %w", err)
	}

	status := &APStatus{
		Running:   ap.isAPRunning(ctx),
		Interface: apInterface,
		SSID:      cfg.SSID,
		IPAddress: apIPBase,
		Enabled:   cfg.Enabled,
	}
	ap.running = status.Running

	// Count connected clients via hostapd station dump
	if status.Running {
		result, err := ap.runner.Run(ctx, exec.RunOpts{
			JobType: "ap_clients",
			Command: "bash",
			Args:    []string{"-c", fmt.Sprintf("iw dev %s station dump 2>/dev/null | grep -c 'Station' || echo 0", apInterface)},
			Timeout: 5 * time.Second,
		})
		if err == nil && result != nil {
			for _, line := range result.Lines {
				if line.Stream == "stdout" {
					count := 0
					fmt.Sscanf(strings.TrimSpace(line.Text), "%d", &count)
					status.ConnectedCount = count
					break
				}
			}
		}
	}

	return status, nil
}

// IsRunning returns whether the AP is currently active.
func (ap *WifiAP) IsRunning() bool {
	return ap.running
}
