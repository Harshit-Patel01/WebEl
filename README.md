# OpenDeploy

opendeploy is a beautiful, brutalist-inspired Next.js + Go dashboard designed to turn any Raspberry Pi into a self-hosted platform-as-a-service (PaaS). No terminal or SSH needed—just plug it in, hit the dashboard, and deploy your apps directly from GitHub via Cloudflare Tunnels.

## Features

- **Single Binary**: The entire Next.js frontend is statically built and embedded directly into the Go backend. You only have to run one file.
- **Cross Platform**: Fully written in Go without any CGO dependencies (uses pure Go SQLite), meaning you can compile the ARM64 Linux binary from a Windows, Mac, or Linux machine effortlessly.
- **Built-in System Management**:
  - Manage WiFi (using `nmcli`)
  - Create free, secure public URLs via Cloudflare Tunnels (`cloudflared`)
  - Automatically configure Nginx as a reverse proxy
  - Clone from GitHub, install dependencies, and run builds
- **Live Terminal & Metrics**: WebSocket integration for streaming live terminal build logs and live system metrics (CPU, RAM, Temp).

## Folder Structure

- `/frontend`: Next.js 14 App Router frontend. Fully static export.
- `/backend`: Go single-binary backend serving REST APIs, WebSockets, and the frontend files.

## Development

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

Because opendeploy uses `modernc.org/sqlite`, it requires absolutely zero C dependencies. You can cross-compile it for a Raspberry Pi 5 from any OS.

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

3. **Cross-Compile Go for Raspberry Pi 5 (Linux ARM64):**
```bash
cd ../backend

# If on Windows (PowerShell):
$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o opendeploy-linux-arm64 ./cmd/opendeploy

# If on Windows (Git Bash / Unix shell) OR Linux/Mac:
env GOOS=linux GOARCH=arm64 go build -o opendeploy-linux-arm64 ./cmd/opendeploy
```

## Deploying to the Pi

Copy `opendeploy-linux-arm64` and `config.example.yaml` (renamed to `config.yaml`) to your Raspberry Pi.

```bash
chmod +x opendeploy-linux-arm64
./opendeploy-linux-arm64
```

To run it automatically on boot, copy `backend/opendeploy.service` to `/etc/systemd/system/opendeploy.service` on the Pi, then run:
```bash
sudo systemctl enable opendeploy
sudo systemctl start opendeploy
```

## Creating a Flashable Image ("Golden Master")

If you want to create an image that you can burn to SD cards and hand to people:

1. Flash standard Raspberry Pi OS Lite (64-bit) to an SD card.
2. Boot the Pi, install `cloudflared` and `nginx`.
3. Set up the `opendeploy.service` as shown above.
4. Shut down the Pi and pull the SD card.
5. On Windows, use **Win32 Disk Imager** to read the SD card back into a `.img` file.
6. Use WSL to shrink the image using `pishrink.sh`:
   ```bash
   wget https://raw.githubusercontent.com/Drewsif/PiShrink/master/pishrink.sh
   chmod +x pishrink.sh
   sudo ./pishrink.sh opendeploy-master.img
   ```
7. Flash the shrunken `.img` onto any new SD card using Raspberry Pi Imager or BalenaEtcher.
