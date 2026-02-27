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
