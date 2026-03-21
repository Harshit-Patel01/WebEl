# Open-Source PaaS Platform — Deployment Infrastructure Guide

> **Scope:** This document describes the complete deployment infrastructure for the self-hosted
> PaaS binary (Go backend + Next.js frontend served by Go) that provisions LXD system containers
> per deployment, handles three deployment types (Web App / Web Service / App Service), exposes
> services via per-app Nginx virtual hosts, and makes them internet-accessible through a single
> Cloudflare Tunnel. No assumptions are made; every design decision is traceable to a real
> implementation reference.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [LXD Container Provisioning](#2-lxd-container-provisioning)
3. [Dependency Injection into Containers](#3-dependency-injection-into-containers)
4. [Deployment Types — Logic & Problems](#4-deployment-types--logic--problems)
   - 4.1 [Web App (Frontend) Deployment](#41-web-app-frontend-deployment)
   - 4.2 [Web Service (Backend) Deployment](#42-web-service-backend-deployment)
   - 4.3 [App Service Deployment](#43-app-service-deployment)
5. [Framework Detection](#5-framework-detection)
6. [Process Supervision inside Containers](#6-process-supervision-inside-containers)
7. [Port Management & LXD Proxy Devices](#7-port-management--lxd-proxy-devices)
8. [Nginx Configuration on the Host](#8-nginx-configuration-on-the-host)
9. [Full-Stack Deployment: Unified Domain Routing](#9-full-stack-deployment-unified-domain-routing)
10. [Cloudflare Tunnel Integration](#10-cloudflare-tunnel-integration)
11. [Service Lifecycle — Start / Stop / Restart from UI](#11-service-lifecycle--start--stop--restart-from-ui)
12. [Go Backend Implementation Reference](#12-go-backend-implementation-reference)
13. [What Is Missing / Gap Analysis](#13-what-is-missing--gap-analysis)
14. [Recommended Fixes & Minimum-Change Path](#14-recommended-fixes--minimum-change-path)

---

## 1. Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                       HOST MACHINE                       │
│  (Ubuntu / Debian — runs the Go binary + Nginx + LXD)   │
│                                                          │
│  ┌─────────────────┐     ┌──────────────────────────┐   │
│  │  Go Binary       │────▶│  LXD daemon (Unix socket)│   │
│  │  (job queue +    │     │  /var/snap/lxd/common/   │   │
│  │   Next.js static)│     │  lxd/unix.socket         │   │
│  └────────┬────────┘     └──────────┬───────────────┘   │
│           │                         │                    │
│           │ manages                 │ creates            │
│           ▼                         ▼                    │
│  ┌────────────────┐   ┌───────────────────────────────┐ │
│  │  Nginx (host)  │   │  LXD System Containers        │ │
│  │                │   │  ┌──────────┐ ┌──────────┐    │ │
│  │  port 8001 ──▶│◀──│──│ app-abc  │ │ app-xyz  │    │ │
│  │  port 8002 ──▶│   │  │ :3000    │ │ :5000    │    │ │
│  │  port 8005 ──▶│   │  └──────────┘ └──────────┘    │ │
│  └──────┬─────────┘   └───────────────────────────────┘ │
│         │                                                │
│  cloudflared tunnel                                      │
│         │                                                │
└─────────┼────────────────────────────────────────────────┘
          │
          ▼ (outbound, no open inbound ports)
     Cloudflare Edge
          │
          ▼
     main.hael.in  →  localhost:8005  (Nginx full-stack vhost)
     api.hael.in   →  localhost:8001  (standalone backend)
     web.hael.in   →  localhost:8002  (standalone frontend)
```

**Key principle:** LXD proxy devices replace iptables for port exposure. The host Nginx sits
between the LXD proxy-exposed ports and the Cloudflare tunnel. This gives you: header rewriting,
`/api/` path routing, buffer tuning, and a single tunnel entry point per "project".

---

## 2. LXD Container Provisioning

### Official Go SDK — already what you should be using

The canonical package is `github.com/canonical/lxd/client`
([pkg.go.dev](https://pkg.go.dev/github.com/canonical/lxd/client)).
It connects over the local Unix socket — no HTTP server needed:

```go
// Connect to LXD over the Unix socket
c, err := lxd.ConnectLXDUnix("", nil)
if err != nil { return err }

// Create container
req := api.InstancesPost{
    Name: "deploy-" + jobID,
    Source: api.InstanceSource{
        Type:  "image",
        Alias: "ubuntu/22.04",
    },
    Type: "container",
}
op, err := c.CreateInstance(req)
op.Wait()

// Start container
reqState := api.InstanceStatePut{Action: "start", Timeout: -1}
op, err = c.UpdateInstanceState("deploy-"+jobID, reqState, "")
op.Wait()
```

Source: [canonical/lxd client pkg.go.dev snippet](https://pkg.go.dev/github.com/canonical/lxd/client)

### Container naming convention (suggestion)

```
deploy-{jobID}-{type}
  e.g.  deploy-a1b2c3-web
        deploy-a1b2c3-api
        deploy-a1b2c3-app
```

This makes cleanup, log lookup, and proxy-device naming deterministic.

---

## 3. Dependency Injection into Containers

### How injection currently works (confirmed correct approach)

You use `lxc file push` (via the Go SDK `c.CreateInstanceFile(...)`) to copy binaries into
the container's rootfs, then execute them with `c.ExecInstance(...)`.

### What to copy — language-conditional matrix

| Project Type | Always copy | Conditionally copy |
|---|---|---|
| Node / React / Next / Vue / Angular | `node`, `npm` (or `pnpm`/`yarn`) | — |
| Python (Flask, Django, FastAPI, etc.) | `python3`, `pip3` | `uvicorn`, `gunicorn` if detected |
| Go | `go` toolchain or pre-built binary | — |
| Static / plain HTML | — (just `nginx` or `caddy`) | — |

### Copy strategy — bind mount vs file push

For large runtimes (Node ≥ 20 MB), copying the entire binary tree on every container is
wasteful. The LXD documentation recommends **disk devices** (bind mounts):

```bash
# Mount the host's cached node runtime into every container read-only
lxc config device add deploy-abc nodebin disk \
  source=/opt/runtimes/node20 \
  path=/usr/local/node \
  readonly=true
```

Then inside the container: `export PATH=/usr/local/node/bin:$PATH`.

This is a zero-copy approach — the runtime is on the host once and shared read-only across
all containers simultaneously.

---

## 4. Deployment Types — Logic & Problems

### 4.1 Web App (Frontend) Deployment

**Current broken parts you described:**
1. Framework not detected after clone
2. Wrong working directory if user specified a subdirectory (e.g., `frontend/`)
3. Install command blank → nothing runs
4. Build command blank → nothing runs
5. Static output not served

#### Resolved flow

```
clone repo
  └─▶ cd user_work_dir  (default: repo root; user override: "frontend/")
        └─▶ detect framework          (see §5)
              └─▶ resolve install_cmd  ("npm install" unless user override)
                    └─▶ resolve build_cmd  ("npm run build" unless user override)
                          └─▶ run install_cmd
                                └─▶ run build_cmd
                                      └─▶ detect output dir  ("dist/" or "out/" or ".next/")
                                            └─▶ serve static files OR start SSR process
```

#### Working directory handling in Go

```go
// buildDir is what user typed in the UI, default ""
func resolveWorkDir(repoRoot, userSubdir string) string {
    if userSubdir == "" {
        return repoRoot
    }
    return filepath.Join(repoRoot, filepath.Clean(userSubdir))
}
```

The `ExecInstance` call must set `Cwd` to this resolved path:

```go
req := api.InstanceExecPost{
    Command:     []string{"/bin/bash", "-c", installCmd},
    WaitForWS:   true,
    Interactive: false,
    Cwd:         resolvedWorkDir,
    Environment: map[string]string{"HOME": "/root", "PATH": "/usr/local/node/bin:/usr/bin:/bin"},
}
```

#### Serving static output after build

For **Vite/React/Vue/Angular** the build output is in `dist/`. For **Next.js static export**
(`output: 'export'`) it is `out/`. For **Next.js SSR** you must run `next start`.

Serve strategy:
- **Static only** (`dist/` / `out/`): copy a lightweight Caddy or `serve` (npx serve) config,
  or use a simple Go HTTP file server process.
- **SSR (Next.js, Nuxt, etc.)**: must run `npm start` / `node .next/standalone/server.js` as a
  persistent service → see §6 for process supervision.

---

### 4.2 Web Service (Backend) Deployment

Flow is: clone → cd workdir → install deps → build → start service and keep running.

The **start command** must be supervised (not just `exec` and abandon). See §6.

---

### 4.3 App Service Deployment

No build step. Clone → cd workdir → start the process directly as a supervised service.
The user provides the start command (e.g. `python3 main.py`, `./mybin`, `node server.js`).
See §6 for supervision.

---

## 5. Framework Detection

### Detection logic — parse `package.json` dependencies

This is the same approach used by Vercel, Netlify, and Render in their build system auto-detection.
The single source of truth is `package.json` in the working directory.

```go
// DetectFramework reads package.json and returns a FrameworkInfo
type FrameworkInfo struct {
    Name       string
    InstallCmd string
    BuildCmd   string
    StartCmd   string
    OutputDir  string
}

// Priority order matters — check more specific frameworks first
var frameworkRules = []struct {
    dep     string
    info    FrameworkInfo
}{
    {"next",      FrameworkInfo{"next.js",  "npm install", "npm run build", "npm start",            ".next"}},
    {"nuxt",      FrameworkInfo{"nuxt",     "npm install", "npm run build", "node .output/server/index.mjs", ".output"}},
    {"@sveltejs/kit", FrameworkInfo{"sveltekit","npm install","npm run build","node build/index.js","build"}},
    {"gatsby",    FrameworkInfo{"gatsby",   "npm install", "npm run build", "",                     "public"}},
    {"@angular/core", FrameworkInfo{"angular","npm install","npm run build","",                     "dist"}},
    {"vue",       FrameworkInfo{"vue",      "npm install", "npm run build", "",                     "dist"}},
    {"react",     FrameworkInfo{"react",    "npm install", "npm run build", "",                     "dist"}},  // CRA fallback
    {"vite",      FrameworkInfo{"vite",     "npm install", "npm run build", "",                     "dist"}},
    {"express",   FrameworkInfo{"express",  "npm install", "",              "node index.js",        ""}},
    {"fastify",   FrameworkInfo{"fastify",  "npm install", "",              "node server.js",       ""}},
}

func DetectFramework(workDir string) (*FrameworkInfo, error) {
    data, err := os.ReadFile(filepath.Join(workDir, "package.json"))
    if err != nil {
        return nil, err   // not a Node project
    }
    var pkg struct {
        Dependencies    map[string]string `json:"dependencies"`
        DevDependencies map[string]string `json:"devDependencies"`
    }
    if err := json.Unmarshal(data, &pkg); err != nil {
        return nil, err
    }
    allDeps := pkg.Dependencies
    for k, v := range pkg.DevDependencies {
        allDeps[k] = v
    }
    for _, rule := range frameworkRules {
        if _, ok := allDeps[rule.dep]; ok {
            info := rule.info
            return &info, nil
        }
    }
    return nil, nil  // unknown Node project
}
```

### Fallback detection for non-Node projects

```go
func DetectLanguage(workDir string) string {
    checks := []struct{ file, lang string }{
        {"requirements.txt", "python"},
        {"pyproject.toml",   "python"},
        {"Pipfile",          "python"},
        {"go.mod",           "go"},
        {"Cargo.toml",       "rust"},
        {"pom.xml",          "java"},
        {"build.gradle",     "java"},
        {"Gemfile",          "ruby"},
        {"composer.json",    "php"},
    }
    for _, c := range checks {
        if _, err := os.Stat(filepath.Join(workDir, c.file)); err == nil {
            return c.lang
        }
    }
    return "unknown"
}
```

### Command resolution priority (user override wins)

```go
func resolveCmd(userOverride, detected string) string {
    if strings.TrimSpace(userOverride) != "" {
        return userOverride
    }
    return detected
}

installCmd := resolveCmd(job.CustomInstallCmd, detected.InstallCmd)
buildCmd   := resolveCmd(job.CustomBuildCmd,   detected.BuildCmd)
startCmd   := resolveCmd(job.CustomStartCmd,   detected.StartCmd)
```

This is the exact model Heroku's buildpack system uses: always allow user override, fall
back to auto-detected.

---

## 6. Process Supervision inside Containers

### Why not just `exec`?

`ExecInstance` with the start command is fire-and-forget unless you hold the websocket
connection open. If your Go process dies or the job context is cancelled, the supervised
process in the container dies too.

### Recommendation: systemd inside the LXD container (already there — use it)

LXD system containers run a **full OS including systemd** as PID 1. This is different from
Docker. You do not need to install anything extra.

> "LXD containers can only run on Linux... LXD system containers run a full Linux OS"
> — [The New Stack, LXD intro](https://thenewstack.io/how-to-deploy-containers-with-lxd/)

The correct approach is to **write a systemd unit file into the container** and enable it.
This gives you autostart on container boot, and allows start/stop/restart from the Go backend
via `systemctl` commands over `ExecInstance`.

#### Step-by-step: writing the unit file into the container

```go
// 1. Render the unit file content
const unitTemplate = `[Unit]
Description=Deploy service {{.Name}}
After=network.target

[Service]
Type=simple
WorkingDirectory={{.WorkDir}}
ExecStart={{.StartCmd}}
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
Environment={{.EnvString}}

[Install]
WantedBy=multi-user.target
`

// 2. Push the file into the container
unitContent := renderTemplate(unitTemplate, serviceData)
c.CreateInstanceFile("deploy-abc", "/etc/systemd/system/app.service",
    lxd.InstanceFileArgs{
        Content:  strings.NewReader(unitContent),
        Mode:     0644,
        Type:     "file",
    })

// 3. Enable and start it
c.ExecInstance("deploy-abc", api.InstanceExecPost{
    Command: []string{"systemctl", "daemon-reload"},
}, nil)
c.ExecInstance("deploy-abc", api.InstanceExecPost{
    Command: []string{"systemctl", "enable", "--now", "app.service"},
}, nil)
```

#### Why not supervisord or s6?

- **supervisord** explicitly states it is "not meant to be run as a substitute for init
  as process id 1" — [Supervisor docs](http://supervisord.org/introduction.html).
  In a system container where systemd is already PID 1, adding supervisord is redundant.
- **s6-overlay** is excellent for Docker (where there is no init), but adds 2-3 MB of
  binary overhead and a different service directory structure that you'd need to manage.
- **runit** is a valid lightweight choice on containers that do NOT have systemd (e.g., Alpine,
  Void Linux containers). For Ubuntu/Debian LXD containers, systemd is present and is the
  correct tool.

**Conclusion: use `systemctl` over `ExecInstance`. Zero extra dependencies.**

---

## 7. Port Management & LXD Proxy Devices

### Current model (as described)

Your Go backend maintains a pool of available host-side ports (e.g., 8000–9000). When a
deployment completes, it picks the next free port from the pool and maps it.

### Correct LXD command to expose a container port on the host

From the [LXD proxy device documentation](https://lxdware.com/forwarding-host-ports-to-lxd-instances/):

```bash
# Expose container's port 3000 on host port 8004
lxc config device add deploy-abc-web webport proxy \
  listen=tcp:127.0.0.1:8004 \
  connect=tcp:127.0.0.1:3000
```

- `listen=tcp:127.0.0.1:8004` — bind only on localhost so it is never directly internet-exposed
- `connect=tcp:127.0.0.1:3000` — forward into the container at its app port

> **Important:** Use `127.0.0.1` not `localhost` in both fields. LXD dropped hostname
> support for security reasons.
> Source: [blog.simos.info proxy device guide](https://blog.simos.info/how-to-use-the-lxd-proxy-device-to-map-ports-between-the-host-and-the-containers/)

### Go SDK equivalent

```go
func AddProxyDevice(c lxd.InstanceServer, containerName, deviceName string, hostPort, containerPort int) error {
    device := map[string]string{
        "type":    "proxy",
        "listen":  fmt.Sprintf("tcp:127.0.0.1:%d", hostPort),
        "connect": fmt.Sprintf("tcp:127.0.0.1:%d", containerPort),
    }
    return c.UpdateInstanceDevice(containerName, deviceName, device, "")
}
```

### Port pool management in Go (existing logic — keep as-is)

Your current logic to find the next available port from the pool is correct. The proxy
device approach supersedes iptables; you do not need to manage firewall rules.

---

## 8. Nginx Configuration on the Host

### Why Nginx on the host when Cloudflare Tunnel can proxy directly?

You raised this yourself — the answer is **path-based routing** for full-stack deployments.
Cloudflare Tunnel's `ingress` rules only do hostname-level routing. To split `/` and `/api/`
at the domain level to different backend containers, you need a local reverse proxy.
Nginx is the right tool.

Additionally: request buffering, `proxy_read_timeout` tuning, WebSocket upgrade headers,
and `X-Real-IP` forwarding are all easier to manage in one Nginx config than distributed
across a tunnel config.

### Config template — standalone single-service app

Generated per deployment. File: `/etc/nginx/sites-available/deploy-{jobID}.conf`

```nginx
# Generated by platform for job: {{.JobID}}
# Type: {{.DeployType}}  Host port: {{.HostPort}}

server {
    listen {{.NginxListenPort}};           # e.g. 8004
    server_name _;

    location / {
        proxy_pass         http://127.0.0.1:{{.HostPort}};   # LXD proxy-exposed port e.g. 8003
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 60s;
    }
}
```

**Port layout for a standalone service:**

```
Container internal port  3000
  → LXD proxy device:    host 127.0.0.1:8003
    → Nginx listen:      127.0.0.1:8004
      → Cloudflare tunnel: app.hael.in → localhost:8004
```

You are right that the two-hop through LXD proxy → Nginx looks redundant for a standalone
service. You could skip Nginx and tunnel directly to the LXD proxy port. The reason to keep
Nginx even for standalone services is **operational consistency**: all your tunnel entries
point to Nginx virtual hosts, and you can add rate limiting, auth headers, or WebSocket
upgrades later without touching the tunnel config.

### Reloading Nginx from Go without root

```go
func reloadNginx() error {
    cmd := exec.Command("nginx", "-s", "reload")
    return cmd.Run()
}
```

If the Go binary runs as a non-root user, grant it specific sudo access:

```
# /etc/sudoers.d/platform-nginx
platform-user ALL=(ALL) NOPASSWD: /usr/sbin/nginx -s reload
platform-user ALL=(ALL) NOPASSWD: /usr/sbin/nginx -t
```

---

## 9. Full-Stack Deployment: Unified Domain Routing

### Your described model — verified correct

You described: `main.hael.in` → Nginx port `8005` → `/` proxies to frontend container
(LXD proxy-exposed at `8002`) → `/api/` proxies to backend container (LXD proxy-exposed
at `8001`).

This is a standard and well-supported Nginx pattern:

```nginx
# /etc/nginx/sites-available/fullstack-{jobID}.conf
# Generated for full-stack project: {{.ProjectName}}

server {
    listen {{.NginxListenPort}};          # e.g. 8005 — this is what the CF tunnel points at
    server_name _;

    # Backend API  ──────────────────────────────────────────
    location /api/ {
        proxy_pass         http://127.0.0.1:{{.BackendHostPort}}/;   # e.g. 8001
        proxy_http_version 1.1;
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 120s;

        # Strip /api prefix before forwarding to backend
        # If your backend is mounted at / internally, use rewrite:
        rewrite ^/api/(.*)$ /$1 break;
    }

    # Frontend  ─────────────────────────────────────────────
    location / {
        proxy_pass         http://127.0.0.1:{{.FrontendHostPort}}/;  # e.g. 8002
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 60s;
    }
}
```

### Complete port allocation map for a full-stack project

```
FRONTEND CONTAINER
  app runs on :3000 (Next.js default)
  LXD proxy:    listen 127.0.0.1:8002  →  connect 127.0.0.1:3000

BACKEND CONTAINER
  app runs on :4000 (user-provided)
  LXD proxy:    listen 127.0.0.1:8001  →  connect 127.0.0.1:4000

NGINX (host)
  server { listen 8005; }
  location /      →  proxy_pass http://127.0.0.1:8002;
  location /api/  →  proxy_pass http://127.0.0.1:8001;

CLOUDFLARE TUNNEL
  main.hael.in  →  http://localhost:8005
```

> **Note on `/api/` prefix stripping:** The `rewrite ^/api/(.*)$ /$1 break;` line strips the
> `/api` prefix before forwarding. Only include this if your backend is designed to handle routes
> at `/` not `/api/`. If your backend is already mounted at `/api/` internally, remove the
> rewrite and point `proxy_pass` directly.

---

## 10. Cloudflare Tunnel Integration

### Why Cloudflare Tunnel instead of direct port exposure

- No public IP required, no firewall rule management.
- Free TLS termination at the edge.
- Your host Nginx does not need to listen on 80/443.
- Outbound-only connection from host to Cloudflare edge.

Source: [tech.aufomm.com — Cloudflare Tunnel multiple services](https://tech.aufomm.com/how-to-use-cloudflare-tunnel-to-expose-multiple-local-services/)

### Install `cloudflared` as a system service

```bash
# Ubuntu/Debian
curl -L --output cloudflared.deb \
  https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
dpkg -i cloudflared.deb

cloudflared tunnel login
cloudflared tunnel create platform-tunnel
```

### Tunnel config file: `/etc/cloudflared/config.yml`

```yaml
tunnel: <YOUR-TUNNEL-UUID>
credentials-file: /etc/cloudflared/<YOUR-TUNNEL-UUID>.json

ingress:
  # Full-stack project
  - hostname: main.hael.in
    service: http://localhost:8005

  # Standalone frontend
  - hostname: web.hael.in
    service: http://localhost:8004

  # Standalone backend API
  - hostname: api.hael.in
    service: http://localhost:8003

  # Required catch-all
  - service: http_status:404
```

Source: [Cloudflare Community — multiple nginx sites on different ports](https://community.cloudflare.com/t/cloudflared-multiple-nginx-websites-on-different-ports/626595)

### DNS records (automatic via cloudflared)

```bash
cloudflared tunnel route dns platform-tunnel main.hael.in
cloudflared tunnel route dns platform-tunnel web.hael.in
cloudflared tunnel route dns platform-tunnel api.hael.in
```

Each creates a CNAME pointing to `<tunnel-uuid>.cfargotunnel.com`.

### Run tunnel as systemd service

```bash
cloudflared service install
systemctl enable --now cloudflared
```

### Dynamic tunnel config update from Go

When a new deployment is complete, your Go backend must:
1. Append the new hostname+port entry to `config.yml`
2. Reload cloudflared: `systemctl reload cloudflared`
   (or `POST http+unix:///run/cloudflared/cloudflared.sock/config`)

```go
func appendTunnelIngress(hostname string, localPort int) error {
    entry := fmt.Sprintf("  - hostname: %s\n    service: http://localhost:%d\n", hostname, localPort)
    // Read existing config, insert before the catch-all "- service: http_status:404"
    // Write back, then reload
    exec.Command("systemctl", "reload", "cloudflared").Run()
    return nil
}
```

> **Important:** The catch-all `- service: http_status:404` entry must always be the last
> entry in the `ingress` list. Your config writer must ensure this invariant.

---

## 11. Service Lifecycle — Start / Stop / Restart from UI

### How the Next.js frontend communicates with the Go backend

The Next.js UI calls a REST endpoint (e.g. `POST /api/deployments/{id}/action`) with
body `{"action": "stop" | "start" | "restart"}`.

The Go backend translates this to a `systemctl` command sent into the container:

```go
func (s *Service) HandleLifecycle(containerName, action string) error {
    validActions := map[string]bool{"start": true, "stop": true, "restart": true}
    if !validActions[action] {
        return fmt.Errorf("invalid action: %s", action)
    }

    c, _ := lxd.ConnectLXDUnix("", nil)

    req := api.InstanceExecPost{
        Command:     []string{"systemctl", action, "app.service"},
        WaitForWS:   true,
        Interactive: false,
    }
    op, err := c.ExecInstance(containerName, req, nil)
    if err != nil {
        return err
    }
    return op.Wait()
}
```

### Checking current service status

```go
func (s *Service) GetStatus(containerName string) (string, error) {
    c, _ := lxd.ConnectLXDUnix("", nil)
    var stdout bytes.Buffer
    req := api.InstanceExecPost{
        Command:     []string{"systemctl", "is-active", "app.service"},
        WaitForWS:   true,
        Interactive: false,
    }
    // capture stdout ...
    // returns "active", "inactive", "failed", "activating"
}
```

### Container-level start/stop (stop the whole container, not just the service)

```go
func StopContainer(c lxd.InstanceServer, name string) error {
    req := api.InstanceStatePut{Action: "stop", Timeout: 30, Force: false}
    op, err := c.UpdateInstanceState(name, req, "")
    if err != nil { return err }
    return op.Wait()
}
```

---

## 12. Go Backend Implementation Reference

### Recommended directory structure additions (minimum changes)

```
internal/
  deployer/
    detect.go        ← DetectFramework(), DetectLanguage()
    provision.go     ← CreateContainer(), InjectDependencies()
    build.go         ← RunInstall(), RunBuild()
    service.go       ← WriteSystemdUnit(), EnableService()
    proxy.go         ← AddLXDProxyDevice(), RemoveProxyDevice()
    nginx.go         ← WriteNginxConfig(), ReloadNginx()
    tunnel.go        ← AppendTunnelIngress(), ReloadCloudflared()
    lifecycle.go     ← Start(), Stop(), Restart(), GetStatus()
  ports/
    pool.go          ← existing port pool logic — keep as-is
```

### Nginx config generation — use Go `text/template`

```go
const nginxStandaloneTemplate = `
server {
    listen {{.NginxPort}};
    server_name _;
    location / {
        proxy_pass         http://127.0.0.1:{{.LXDProxyPort}};
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_read_timeout 60s;
    }
}
`

func WriteNginxConfig(data NginxConfigData) error {
    t := template.Must(template.New("nginx").Parse(nginxStandaloneTemplate))
    path := fmt.Sprintf("/etc/nginx/sites-available/deploy-%s.conf", data.JobID)
    f, _ := os.Create(path)
    defer f.Close()
    if err := t.Execute(f, data); err != nil { return err }
    // Symlink to sites-enabled
    os.Symlink(path, strings.Replace(path, "sites-available", "sites-enabled", 1))
    return reloadNginx()
}
```

---

## 13. What Is Missing / Gap Analysis

Based on your description, here is what is not yet implemented or has gaps:

| # | Gap | Severity | Location |
|---|-----|----------|----------|
| 1 | Framework detection not implemented | **Critical** | `deployer/detect.go` (missing) |
| 2 | Working directory not resolved before exec | **Critical** | `deployer/build.go` — add `Cwd` to ExecPost |
| 3 | Blank install/build cmd falls through silently | **High** | `deployer/build.go` — add `resolveCmd()` |
| 4 | No systemd unit file written to container | **High** | `deployer/service.go` (missing) |
| 5 | Service supervision: app dies on container reboot | **High** | Systemd `[Install] WantedBy=multi-user.target` missing |
| 6 | Nginx config not generated/reloaded after deploy | **High** | `deployer/nginx.go` (missing) |
| 7 | Cloudflare tunnel config not updated after deploy | **Medium** | `deployer/tunnel.go` (missing) |
| 8 | `/api/` path stripping in full-stack Nginx config | **Medium** | Nginx template needs `rewrite` rule clarification |
| 9 | Static output directory detection after build | **Medium** | `deployer/build.go` — need to check `dist/`,`out/`,`.next/` |
| 10 | Start/stop/restart endpoints in Go backend | **Medium** | `deployer/lifecycle.go` (missing) |
| 11 | Port pool not cleaned up on container delete | **Low** | `ports/pool.go` — add `ReleasePort()` call in teardown |

---

## 14. Recommended Fixes & Minimum-Change Path

The following is ordered by impact. Do NOT re-architect what is already working.

### Step 1 — Add framework detection (unblocks Web App deploys)

Add `detect.go` with `DetectFramework()` as shown in §5. Call it after `git clone` before
the install step. If it returns `nil` and the user has not provided a custom install cmd,
log a warning and skip the install step gracefully rather than erroring.

### Step 2 — Fix Cwd in ExecInstance calls (unblocks sub-directory projects)

In every `ExecInstance` call that runs `install`, `build`, or `start`, set:

```go
req.Cwd = resolveWorkDir(repoRoot, job.WorkDir)
```

This is a one-line change per exec call.

### Step 3 — Write systemd unit file after build (unblocks App Service + Web Service)

After the build completes (or immediately for App Service), push the unit file and run
`systemctl enable --now`. Template is in §6.

### Step 4 — Add Nginx config generation after deploy (unblocks external access)

After the LXD proxy device is configured, render the Nginx config template, write it to
`sites-available/`, symlink to `sites-enabled/`, and call `nginx -s reload`. Template is in §8.

### Step 5 — Add Cloudflare tunnel ingress append (unblocks public URLs)

After Nginx config is written, append the new hostname entry to `config.yml` and reload
cloudflared. Logic is in §10.

### Step 6 — Add lifecycle API endpoints

Wire `GET /api/deployments/{id}/status` and `POST /api/deployments/{id}/action` to
`deployer/lifecycle.go` as shown in §11. The Next.js frontend can then call these to
display and control service state.

---

## References

| Topic | Source |
|---|---|
| LXD Go SDK — `ConnectLXDUnix`, `CreateInstance` | https://pkg.go.dev/github.com/canonical/lxd/client |
| LXD proxy device port forwarding | https://lxdware.com/forwarding-host-ports-to-lxd-instances/ |
| LXD proxy device — use 127.0.0.1 not localhost | https://blog.simos.info/how-to-use-the-lxd-proxy-device-to-map-ports-between-the-host-and-the-containers/ |
| LXD network forward port add | https://linuxcontainers.org/lxd/docs/latest/howto/network_forwards/ |
| supervisord not suitable as PID 1 | http://supervisord.org/introduction.html |
| s6 designed for PID 1 in containers | https://www.sliceofexperiments.com/p/s6-run-multiple-processes-in-your |
| LXD system containers include full OS + systemd | https://thenewstack.io/how-to-deploy-containers-with-lxd/ |
| Cloudflare Tunnel multiple local services | https://tech.aufomm.com/how-to-use-cloudflare-tunnel-to-expose-multiple-local-services/ |
| Cloudflare multi-Nginx-port config example | https://community.cloudflare.com/t/cloudflared-multiple-nginx-websites-on-different-ports/626595 |
| Framework detection via package.json | https://gist.github.com/rambabusaravanan/1d594bd8d1c3153bc8367753b17d074b |

---

*Last updated: March 2026 | Platform: Ubuntu 22.04 LTS / Debian 12 | LXD 5.x | Go 1.22+ | Nginx 1.24+*
