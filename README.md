# OpenDeploy

OpenDeploy is a beautiful, brutalist-inspired Next.js + Go dashboard designed to turn any linux device into a self-hosted platform-as-a-service (PaaS). No terminal or SSH needed—just plug it in, hit the dashboard, and deploy your apps directly from GitHub via Cloudflare Tunnels.

### Frontend
```bash
cd frontend
npm install
npm run dev
```

### Backend
```bash
cd backend
go run ./cmd/opendeploy
```

## Building the Release Binary (Windows/Mac/Linux)

Because opendeploy uses `modernc.org/sqlite`, it requires absolutely zero C dependencies. You can cross-compile it for a ubuntu/debian from any OS.

1. **Build the Next.js Frontend:**
```bash
cd frontend
npm run build
```

2. **Move Frontend to Backend Static Directory:**
```bash
# Clear old static files
rm -rf ../backend/static/frontend
mkdir -p ../backend/static/frontend

# Copy new static files
cp -r build/* ../backend/static/frontend/
```

3. **Cross-Compile Go for (Linux ARM64):**
```bash
cd ../backend

# If on Windows (PowerShell):
$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o opendeploy-linux-arm64 ./cmd/opendeploy

# If on Windows (Git Bash / Unix shell) OR Linux/Mac:
env GOOS=linux GOARCH=arm64 go build -o opendeploy-linux-arm64 ./cmd/opendeploy
```

## Deploying to the Pi / Linux

Copy `opendeploy-linux-arm64` and `config.example.yaml` (renamed to `config.yaml`) to your Raspberry Pi.

```bash
chmod +x opendeploy-linux-arm64
./opendeploy-linux-arm64
```

To run it automatically on boot, copy `backend/opendeploy.service` to `/etc/systemd/system/opendeploy.service` on the ubuntu/linux, then run:
```bash
sudo systemctl enable opendeploy
sudo systemctl start opendeploy
```

## WiFi Hotspot Feature

OpenDeploy automatically creates a fallback WiFi hotspot when your device is not connected to any WiFi network. This allows you to access the dashboard even without an existing WiFi connection.

### Hotspot Details
- **SSID:** `webel`
- **Password:** `webel123`
- **IP Address:** `10.42.0.1`
- **Dashboard URL:** `http://10.42.0.1:8080` or `http://webel.local:8080`

### Setup Requirements

Before the hotspot feature can work, you need to install and configure the required dependencies:

```bash
# Run the automated setup script
sudo bash scripts/setup-wifi-hotspot.sh
```

This script will:
- Install NetworkManager and required packages
- Disable conflicting network services
- Configure WiFi power management
- Set up proper permissions
- Test the hotspot functionality

### Manual Setup

If you prefer to set up manually, install these packages:

```bash
sudo apt-get update
sudo apt-get install -y network-manager dnsmasq-base iw wireless-tools
```

Then enable NetworkManager:

```bash
sudo systemctl enable NetworkManager
sudo systemctl start NetworkManager
sudo nmcli device set wlan0 managed yes
```

### How It Works

1. The application monitors WiFi connectivity every 10 seconds
2. When no WiFi connection is detected, it automatically creates a hotspot
3. Clients connecting to the hotspot receive IP addresses via DHCP (10.42.0.x)
4. The hotspot remains active until the device connects to a WiFi network
5. When WiFi is restored, the hotspot is automatically disabled

### Troubleshooting

If the hotspot is not working:

1. **Check NetworkManager status:**
```bash
sudo systemctl status NetworkManager
```

2. **Verify wlan0 is managed:**
```bash
nmcli device status
```

3. **Check application logs:**
```bash
sudo journalctl -u opendeploy -f
```

4. **Test manual hotspot creation:**
```bash
sudo nmcli device wifi hotspot ssid test password test12345
```

For detailed troubleshooting and configuration options, see [WIFI_HOTSPOT_SETUP.md](WIFI_HOTSPOT_SETUP.md).
