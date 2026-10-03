!/usr/bin/env bash
# OpenDeploy one-command host bootstrap for Raspberry Pi / Debian / Ubuntu.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Harshit-Patel01/WebEl/main/scripts/install.sh | sudo bash
#   sudo ./scripts/install.sh
#   sudo ./scripts/install.sh --binary /path/to/opendeploy-linux-arm64
#   sudo ./scripts/install.sh --from-source
set -euo pipefail

OPENDEPLOY_VERSION="${OPENDEPLOY_VERSION:-latest}"
OPENDEPLOY_REPO="${OPENDEPLOY_REPO:-Harshit-Patel01/WebEl}"
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local}"
CONFIG_DIR="/etc/opendeploy"
DATA_DIR="/var/lib/opendeploy"
LOG_DIR="/var/log/opendeploy"
WWW_DIR="/var/www/opendeploy"
BINARY_DEST="${INSTALL_PREFIX}/bin/opendeploy"
SERVICE_DEST="/etc/systemd/system/opendeploy.service"

BINARY_PATH=""
FROM_SOURCE=0
SKIP_DEPS=0
SKIP_LXD=0
SKIP_START=0
ARCH_OVERRIDE=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." 2>/dev/null && pwd || true)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[opendeploy]${NC} $*" >&2; }
warn() { echo -e "${YELLOW}[opendeploy]${NC} $*" >&2; }
err()  { echo -e "${RED}[opendeploy]${NC} $*" >&2; }

usage() {
  cat <<'EOF'
OpenDeploy host installer

Options:
  --binary PATH       Install this pre-built binary (skips download/build)
  --from-source       Build from the local git checkout (requires Go + Node)
  --arch ARCH         Force arch: arm64|amd64|armv7|386 (default: auto-detect)
  --skip-deps         Skip apt package installation
  --skip-lxd          Skip LXD install/init
  --skip-start        Install but do not enable/start the service
  --repo OWNER/NAME   GitHub repo for release downloads (optional)
  -h, --help          Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY_PATH="$2"; shift 2 ;;
    --from-source) FROM_SOURCE=1; shift ;;
    --arch) ARCH_OVERRIDE="$2"; shift 2 ;;
    --skip-deps) SKIP_DEPS=1; shift ;;
    --skip-lxd) SKIP_LXD=1; shift ;;
    --skip-start) SKIP_START=1; shift ;;
    --repo) OPENDEPLOY_REPO="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) err "Unknown option: $1"; usage; exit 1 ;;
  esac
done

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    err "Run as root (sudo)."
    exit 1
  fi
}

detect_arch() {
  if [[ -n "${ARCH_OVERRIDE}" ]]; then
    echo "${ARCH_OVERRIDE}"
    return
  fi
  local m
  m="$(uname -m)"
  case "${m}" in
    aarch64|arm64) echo "arm64" ;;
    x86_64|amd64)  echo "amd64" ;;
    armv7l|armhf)  echo "armv7" ;;
    i386|i686)     echo "386" ;;
    *)
      err "Unsupported architecture: ${m}"
      exit 1
      ;;
  esac
}

detect_os() {
  if [[ ! -f /etc/os-release ]]; then
    err "Cannot detect OS (/etc/os-release missing). Debian/Ubuntu/Raspberry Pi OS required."
    exit 1
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    debian|ubuntu|raspbian)
      log "Detected OS: ${PRETTY_NAME:-$ID}"
      ;;
    *)
      warn "OS '${ID}' is untested; continuing with apt-based install."
      ;;
  esac
  if ! command -v apt-get >/dev/null 2>&1; then
    err "apt-get not found. This installer supports Debian-family systems only."
    exit 1
  fi
}

binary_suffix() {
  local arch="$1"
  case "${arch}" in
    arm64) echo "linux-arm64" ;;
    amd64) echo "linux-amd64" ;;
    armv7) echo "linux-armv7" ;;
    386)   echo "linux-x86" ;;
    *) err "Unknown arch mapping: ${arch}"; exit 1 ;;
  esac
}

install_packages() {
  if [[ "${SKIP_DEPS}" -eq 1 ]]; then
    warn "Skipping OS dependency installation (--skip-deps)"
    return
  fi

  log "Updating apt and installing dependencies..."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y

  local pkgs=(
    ca-certificates
    curl
    wget
    gnupg
    git
    nginx
    network-manager
    hostapd
    dnsmasq
    avahi-daemon
    avahi-utils
    iptables
    iptables-persistent
    netfilter-persistent
    sudo
    python3
    build-essential
  )

  # Node.js â€” prefer distro package; NodeSource optional later
  if apt-cache show nodejs >/dev/null 2>&1; then
    pkgs+=(nodejs)
  fi
  if apt-cache show npm >/dev/null 2>&1; then
    pkgs+=(npm)
  fi

  apt-get install -y "${pkgs[@]}"

  # LXD: snap on Ubuntu/Pi OS when available, else apt
  if [[ "${SKIP_LXD}" -eq 0 ]]; then
    if command -v snap >/dev/null 2>&1; then
      log "Installing LXD via snap..."
      snap install lxd || apt-get install -y lxd lxd-client || true
    else
      log "Installing LXD via apt..."
      apt-get install -y lxd lxd-client || apt-get install -y lxc || true
    fi
  fi

  systemctl enable --now NetworkManager 2>/dev/null || true
  systemctl enable --now avahi-daemon 2>/dev/null || true
  systemctl enable nginx 2>/dev/null || true
  systemctl start nginx 2>/dev/null || true

  # hostapd must not auto-start until OpenDeploy configures it
  systemctl unmask hostapd 2>/dev/null || true
  systemctl disable hostapd 2>/dev/null || true
  systemctl stop hostapd 2>/dev/null || true
  systemctl disable dnsmasq 2>/dev/null || true
  systemctl stop dnsmasq 2>/dev/null || true
}

init_lxd() {
  if [[ "${SKIP_LXD}" -eq 1 ]]; then
    warn "Skipping LXD init (--skip-lxd)"
    return
  fi

  if ! command -v lxd >/dev/null 2>&1 && ! command -v lxc >/dev/null 2>&1; then
    warn "LXD/LXC not found after package install; deploys will not work until LXD is installed."
    return
  fi

  systemctl enable --now snap.lxd.daemon 2>/dev/null || systemctl enable --now lxd 2>/dev/null || true

  if command -v lxd >/dev/null 2>&1; then
    # `lxc info` succeeds even with no storage pool, so it is not a valid
    # readiness check — a fresh host passes it and then fails container
    # creation with "No root device could be found". Test the two things that
    # actually matter: a default storage pool, and a root disk on the default
    # profile. Both are required before `lxc init` can create a container.
    if ! lxc storage show default >/dev/null 2>&1; then
      log "LXD has no default storage pool, creating one..."
      for driver in zfs dir btrfs; do
        if lxc storage create default "$driver" >/dev/null 2>&1; then
          log "Created LXD storage pool (driver: ${driver})"
          break
        fi
      done
      if ! lxc storage show default >/dev/null 2>&1; then
        warn "Could not create a storage pool; falling back to 'lxd init --auto'"
        lxd init --auto || warn "lxd init --auto failed; run 'sudo lxd init' manually."
      fi
    fi

    # A pool is not sufficient on its own: with an empty default profile the
    # instance still has no root disk.
    if ! lxc profile device get default root pool >/dev/null 2>&1; then
      log "Attaching root disk to the default LXD profile..."
      lxc profile device add default root disk path=/ pool=default >/dev/null 2>&1 \
        || warn "Could not attach a root disk; run 'sudo lxc profile device add default root disk path=/ pool=default'"
    fi

    if lxc storage show default >/dev/null 2>&1; then
      log "LXD ready (default storage pool present)"
    else
      warn "LXD still has no default storage pool; deploys that use LXD will fail."
    fi
  fi
}

resolve_binary() {
  local arch="$1"
  local suffix
  suffix="$(binary_suffix "${arch}")"
  local tmp

  if [[ -n "${BINARY_PATH}" ]]; then
    if [[ ! -f "${BINARY_PATH}" ]]; then
      err "Binary not found: ${BINARY_PATH}"
      exit 1
    fi
    echo "${BINARY_PATH}"
    return
  fi

  # Prefer already-built artifacts in the repo checkout
  if [[ -n "${REPO_ROOT}" ]]; then
    for candidate in \
      "${REPO_ROOT}/backend/opendeploy-${suffix}" \
      "${REPO_ROOT}/backend/opendeploy" \
      "${REPO_ROOT}/opendeploy-${suffix}" \
      "${REPO_ROOT}/opendeploy"; do
      if [[ -f "${candidate}" ]]; then
        log "Using local binary: ${candidate}"
        echo "${candidate}"
        return
      fi
    done
  fi

  if [[ "${FROM_SOURCE}" -eq 1 ]]; then
    if [[ -z "${REPO_ROOT}" || ! -f "${REPO_ROOT}/backend/go.mod" ]]; then
      err "--from-source requires running from a full OpenDeploy checkout"
      exit 1
    fi
    if ! command -v go >/dev/null 2>&1; then
      err "Go is required for --from-source"
      exit 1
    fi
    if ! command -v npm >/dev/null 2>&1; then
      err "npm is required for --from-source"
      exit 1
    fi
    log "Building frontend + backend from source (${arch})..."
    (
      cd "${REPO_ROOT}/frontend"
      if [[ -f package-lock.json ]]; then
        npm ci
      else
        npm install
      fi
      npm run build
    )
    (
      cd "${REPO_ROOT}/backend"
      case "${arch}" in
        arm64) GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o "opendeploy-${suffix}" ./cmd/opendeploy ;;
        amd64) GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "opendeploy-${suffix}" ./cmd/opendeploy ;;
        armv7) GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o "opendeploy-${suffix}" ./cmd/opendeploy ;;
        386)   GOOS=linux GOARCH=386 go build -ldflags="-s -w" -o "opendeploy-${suffix}" ./cmd/opendeploy ;;
      esac
    )
    echo "${REPO_ROOT}/backend/opendeploy-${suffix}"
    return
  fi

  if [[ -n "${OPENDEPLOY_REPO}" ]]; then
    tmp="$(mktemp -d)"
    local url asset
    asset="opendeploy-${suffix}"
    if [[ "${OPENDEPLOY_VERSION}" == "latest" ]]; then
      url="https://github.com/${OPENDEPLOY_REPO}/releases/latest/download/${asset}"
    else
      url="https://github.com/${OPENDEPLOY_REPO}/releases/download/${OPENDEPLOY_VERSION}/${asset}"
    fi
    log "Downloading ${url}..."
    if curl -fsSL -o "${tmp}/${asset}" "${url}"; then
      chmod +x "${tmp}/${asset}"
      echo "${tmp}/${asset}"
      return
    fi
    warn "Release download failed; place a binary and re-run with --binary"
  fi

  err "No OpenDeploy binary found."
  err "Build on a dev machine (make -C backend build-linux-${arch}) or pass --binary / --from-source / --repo OWNER/NAME"
  exit 1
}

install_files() {
  local src="$1"

  log "Installing directories and config..."
  mkdir -p "${CONFIG_DIR}" "${DATA_DIR}" "${LOG_DIR}" "${WWW_DIR}" \
    "${INSTALL_PREFIX}/bin"

  install -m 755 "${src}" "${BINARY_DEST}"

  if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
    if [[ -n "${REPO_ROOT}" && -f "${REPO_ROOT}/backend/config.example.yaml" ]]; then
      install -m 644 "${REPO_ROOT}/backend/config.example.yaml" "${CONFIG_DIR}/config.yaml"
    else
      cat > "${CONFIG_DIR}/config.yaml" <<'YAML'
server:
  port: 3000
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s

database:
  path: "/var/lib/opendeploy/state.db"

deploy:
  workspace_root: "/var/lib/opendeploy/projects"
  max_concurrent_builds: 1
  build_timeout: 60m
  git_binary: "/usr/bin/git"
  node_binary: "/usr/bin/node"
  npm_binary: "/usr/bin/npm"
  python_binary: "/usr/bin/python3"
  lxd_enabled: true
  output_root: "/var/www/opendeploy"
  port_pool_start: 8000
  port_pool_end: 9000

nginx:
  sites_available: "/etc/nginx/sites-available"
  sites_enabled: "/etc/nginx/sites-enabled"
  log_path: "/var/log/nginx"

cloudflared:
  binary: "/usr/local/bin/cloudflared"
  config_path: "/etc/cloudflared/config.yml"
  credentials_dir: "/etc/cloudflared"

logging:
  level: "info"
  log_dir: "/var/log/opendeploy"
  max_log_size_mb: 50
  max_log_files: 5

security:
  session_duration: 24h
  bcrypt_cost: 12
  lan_only: false
YAML
      chmod 644 "${CONFIG_DIR}/config.yaml"
    fi
  else
    log "Keeping existing ${CONFIG_DIR}/config.yaml"
  fi

  if [[ -n "${REPO_ROOT}" && -f "${REPO_ROOT}/backend/opendeploy.service" ]]; then
    install -m 644 "${REPO_ROOT}/backend/opendeploy.service" "${SERVICE_DEST}"
  else
    cat > "${SERVICE_DEST}" <<'UNIT'
[Unit]
Description=opendeploy Hosting Dashboard
After=local-fs.target network-online.target avahi-daemon.service NetworkManager.service
Wants=network-online.target avahi-daemon.service

[Service]
Type=simple
User=root
Group=root

ExecStartPre=/usr/bin/mkdir -p /var/lib/opendeploy /var/log/opendeploy
ExecStart=/usr/local/bin/opendeploy --config /etc/opendeploy/config.yaml

Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=opendeploy

Environment=OPENDEPLOY_ENV=production
Environment=GODEBUG=netdns=go

[Install]
WantedBy=multi-user.target
UNIT
  fi

  # Root-owned data dirs (service runs as root for WiFi / LXD / nginx)
  chown -R root:root "${DATA_DIR}" "${LOG_DIR}" "${CONFIG_DIR}"
  chmod 755 "${DATA_DIR}" "${LOG_DIR}" "${CONFIG_DIR}"
}

enable_service() {
  if [[ "${SKIP_START}" -eq 1 ]]; then
    warn "Skipping service enable/start (--skip-start)"
    systemctl daemon-reload
    return
  fi

  log "Enabling and starting opendeploy..."
  systemctl daemon-reload
  systemctl enable opendeploy
  systemctl restart opendeploy
}

write_nginx_config() {
  local _hn
  _hn="$(hostname 2>/dev/null | tr -d '[:space:]')"

  local _server_names="webel.local"
  if [[ -n "${_hn}" ]]; then
    _server_names="webel.local ${_hn}.local"
  fi

  local nginx_conf="/etc/nginx/sites-available/opendeploy"

  log "Writing nginx config (${_server_names} + catch-all)..."

  local _tmp_conf
  _tmp_conf="$(mktemp)"

  cat > "${_tmp_conf}" <<'NGINX'
# Drop connections for unknown domains (catch-all)
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    return 444;
}

server {
    listen 80;
    listen [::]:80;
    # Allow specific hostnames, localhost, IPv4, and IPv6 addresses
    server_name __SERVER_NAMES__ localhost 127.0.0.1 ~^[0-9\.]+$ ~^\[[a-fA-F0-9:]+\]$;

    access_log  /var/log/nginx/opendeploy-access.log;
    error_log   /var/log/nginx/opendeploy-error.log;

    location / {
        proxy_pass            http://127.0.0.1:3000;
        proxy_http_version    1.1;
        proxy_set_header      Upgrade           $http_upgrade;
        proxy_set_header      Connection        "upgrade";
        proxy_set_header      Host              $host;
        proxy_set_header      X-Real-IP         $remote_addr;
        proxy_set_header      X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header      X-Forwarded-Proto $scheme;
        proxy_read_timeout    60s;
        proxy_send_timeout    60s;
        proxy_cache_bypass    $http_upgrade;
    }
}
NGINX

  sed -i "s/__SERVER_NAMES__/${_server_names}/g" "${_tmp_conf}"
  mv "${_tmp_conf}" "${nginx_conf}"

  chmod 644 "${nginx_conf}"

  rm -f /etc/nginx/sites-enabled/default
  rm -f /etc/nginx/sites-available/default
  rm -f /etc/nginx/conf.d/default.conf

  if [[ -f /etc/nginx/nginx.conf ]]; then
    if grep -q 'listen.*80.*default_server' /etc/nginx/nginx.conf 2>/dev/null; then
      log "Disabling embedded default server in nginx.conf..."
      sed -i '/^[^#]*listen.*80.*default_server/,/^[[:space:]]*\}/{ s/^/#OD# /; }' /etc/nginx/nginx.conf
    fi
  fi

  ln -sf "${nginx_conf}" /etc/nginx/sites-enabled/opendeploy

  if nginx -t 2>&1; then
    systemctl reload nginx 2>/dev/null || systemctl restart nginx 2>/dev/null || true
    log "nginx: port 80 -> OpenDeploy portal (${_server_names} + catch-all)"
  else
    warn "nginx config test failed -- run 'nginx -t' to debug"
  fi
}

health_check() {
  echo
  log "=== Dependency / service health ==="
  local ok=0 fail=0

  check_cmd() {
    local name="$1"
    shift
    if "$@" >/dev/null 2>&1; then
      echo -e "  ${GREEN}OK${NC}   ${name}"
      ok=$((ok + 1))
    else
      echo -e "  ${RED}MISS${NC} ${name}"
      fail=$((fail + 1))
    fi
  }

  check_svc() {
    local name="$1"
    if systemctl is-active --quiet "$name" 2>/dev/null; then
      echo -e "  ${GREEN}OK${NC}   service ${name}"
      ok=$((ok + 1))
    else
      echo -e "  ${YELLOW}DOWN${NC} service ${name}"
      fail=$((fail + 1))
    fi
  }

  check_cmd "opendeploy binary" test -x "${BINARY_DEST}"
  check_cmd "config.yaml" test -f "${CONFIG_DIR}/config.yaml"
  check_cmd "nginx" command -v nginx
  check_cmd "nmcli (NetworkManager)" command -v nmcli
  check_cmd "hostapd" command -v hostapd
  check_cmd "dnsmasq" command -v dnsmasq
  check_cmd "avahi-daemon" command -v avahi-daemon
  check_cmd "git" command -v git
  check_cmd "python3" command -v python3
  check_cmd "lxc/lxd" bash -c 'command -v lxc >/dev/null || command -v lxd >/dev/null'
  if command -v node >/dev/null 2>&1; then
    echo -e "  ${GREEN}OK${NC}   node (optional)"
    ok=$((ok + 1))
  else
    echo -e "  ${YELLOW}SKIP${NC} node (optional â€” used for host-side builds)"
  fi

  check_svc "opendeploy"
  check_svc "NetworkManager"
  check_svc "avahi-daemon"
  check_svc "nginx"

  echo
  log "System info:"
  echo "  Arch:     $(uname -m) â†’ installer arch $(detect_arch)"
  echo "  Kernel:   $(uname -r)"
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "  OS:       ${PRETTY_NAME:-unknown}"
  fi
  if [[ -f /proc/device-tree/model ]]; then
    echo "  Model:    $(tr -d '\0' </proc/device-tree/model)"
  fi
  hostname -I 2>/dev/null | awk '{print "  IP:       "$1}'
  echo "  Hostname: $(hostname)"

  echo
  if systemctl is-active --quiet opendeploy 2>/dev/null; then
    local _ip
    _ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
    log "OpenDeploy is running."
    log "Dashboard: http://webel.local  or  http://$(hostname).local  or  http://${_ip}"
    log "Hotspot SSID (when AP mode): webel / webel123 â†’ http://webel.local"
  else
    warn "opendeploy service is not active. Check: journalctl -u opendeploy -e"
  fi

  if [[ "${fail}" -gt 0 ]]; then
    warn "Health check reported ${fail} missing/down items. Review above before deploying apps."
  else
    log "All critical checks passed (${ok} OK)."
  fi
}

main() {
  require_root
  detect_os
  local arch
  arch="$(detect_arch)"
  log "Target architecture: ${arch}"

  install_packages
  init_lxd

  local bin
  bin="$(resolve_binary "${arch}")"
  install_files "${bin}"
  enable_service
  write_nginx_config
  health_check
}

main
