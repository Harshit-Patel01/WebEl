# OpenDeploy

OpenDeploy is a Next.js and Go-based dashboard designed to turn a Linux device, particularly a Raspberry Pi, into a self-hosted platform-as-a-service (PaaS). Users can plug in the device, open the dashboard, connect to WiFi or a Cloudflare Tunnel, and deploy GitHub applications into LXD containers situated behind an nginx reverse proxy.

## UI Screenshots

| | |
|:---:|:---:|
| ![Dashboard](assets/dashboard.png) | ![Deployments](assets/deployments.png) |
| **Dashboard** | **Deployments** |
| ![Settings](assets/settings.png) | ![WiFi Setup](assets/wifi-setup.png) |
| **Settings** | **WiFi Setup** |

## System Requirements

* **Operating Systems:** Raspberry Pi OS (Debian-based), Ubuntu, and Debian. The `apt-get` package manager is required.
* **Supported Architectures:** `arm64` (Pi 3/4/5 64-bit), `amd64`, `armv7` (32-bit Pi), and `x86` (32-bit x86).
* **Required Packages:** nginx (reverse proxy), NetworkManager and nmcli (client WiFi), hostapd and dnsmasq (hotspot), avahi-daemon (`*.local` resolution), LXD / LXC (app containers via snap or apt), git, python3, and iptables / iptables-persistent (Hotspot NAT).
* **Additional Dependencies:** Node/npm is recommended. The `cloudflared` package is automatically downloaded for the running architecture when setting up a tunnel.

## Installation Instructions

### One-Command Installation
For a fresh Raspberry Pi OS, Ubuntu, or Debian system, you can install OpenDeploy directly without cloning or pre-downloading any files. Run this command as root:

```bash
curl -fsSL https://raw.githubusercontent.com/Harshit-Patel01/WebEl/main/scripts/install.sh | sudo bash
```

The automated installer performs the following actions:
* Detects your OS and CPU architecture.
* Installs required `apt` packages, including nginx, NetworkManager, hostapd, dnsmasq, Avahi, git, python3, iptables, and LXD.
* Initializes LXD using `lxd init --auto` if it is not already configured.
* Downloads and installs the OpenDeploy binary, default configuration, and the systemd unit.
* Configures nginx to proxy port 80 to the dashboard, matching `webel.local`, specific device hostnames, and direct IP addresses.
* Enables auto-start on boot and prints a dependency health checklist.

### Advanced Installation Options
The installer script supports optional flags for local development or custom builds:

```bash
sudo ./scripts/install.sh --binary ./backend/opendeploy-linux-arm64
sudo ./scripts/install.sh --from-source
sudo ./scripts/install.sh --repo Harshit-Patel01/WebEl
OPENDEPLOY_VERSION=v1.2.0 ... | sudo bash
```

### Manual Installation
If dependencies are already installed on the system, you can set up OpenDeploy manually. With the matching binary present on the device, run:

```bash
sudo make -C backend install
sudo systemctl start opendeploy
```

Alternatively, to copy the files manually:

```bash
sudo install -m 755 opendeploy-linux-arm64 /usr/local/bin/opendeploy
sudo mkdir -p /etc/opendeploy /var/lib/opendeploy /var/log/opendeploy
sudo cp backend/config.example.yaml /etc/opendeploy/config.yaml
sudo cp backend/opendeploy.service /etc/systemd/system/opendeploy.service
sudo systemctl daemon-reload
sudo systemctl enable --now opendeploy
```

## Usage Guidelines

OpenDeploy runs as root via systemd, which allows it to properly manage WiFi, nginx, and LXD. 

### Accessing the Dashboard
Once installed, the installer will print three URLs, allowing you to access the dashboard via the following methods:
* **mDNS (fixed name):** `http://webel.local`.
* **mDNS (device hostname):** `http://<hostname>.local`.
* **Direct IP:** `http://<device-ip>`.

### WiFi Hotspot Feature
When the device is not connected to a network, OpenDeploy broadcasts a WiFi hotspot to provide immediate access to the dashboard.
* **SSID:** `webel`.
* **Password:** `webel123`.
* **Hotspot Dashboard URL:** `http://webel.local` (nginx routes port 80 traffic to the dashboard).

## Development and Building

### Running the Project Locally
To run the frontend:
```bash
cd frontend
npm install
npm run dev
```

To run the backend:
```bash
cd backend
go run ./cmd/opendeploy --config config.example.yaml
```

### Building a Release Binary
The frontend operates as a static Next.js export, meaning `npm run build` writes the embedded assets directly into the Go source tree without needing a manual copy step.

The most straightforward way to produce a release binary is via the provided Makefile, which handles the frontend build and cross-compilation simultaneously:

```bash
make -C backend build-release
make -C backend build-linux-amd64
make -C backend build-linux-arm
make -C backend build-linux-x86
make -C backend build-all
```

To build the project manually without utilizing Make (e.g., on Linux/macOS):

```bash
cd frontend && npm install && npm run build
cd ../backend
env GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o opendeploy-linux-arm64 ./cmd/opendeploy
```

### Building on Windows
If you are developing on Windows (where `make` is not available by default), we have provided a PowerShell script that mimics the Makefile:

```powershell
.\scripts\build.ps1                  # Default (build-all): frontend + all four Linux binaries
.\scripts\build.ps1 build-release    # Alias of build-all, prints the release artifact summary
.\scripts\build.ps1 build-frontend   # Frontend only (refreshes backend/static/frontend)
.\scripts\build.ps1 build-linux-amd64
```

The script checks every step and verifies `backend/static/frontend` byte-for-byte against
`frontend/out` before compiling, so a failed frontend build stops the script instead of
producing a binary with a stale (or missing) embedded frontend.
