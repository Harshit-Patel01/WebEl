#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="/etc/opendeploy"
DATA_DIR="/var/lib/opendeploy"
LOG_DIR="/var/log/opendeploy"
WWW_DIR="/var/www/opendeploy"
BINARY_DEST="/usr/local/bin/opendeploy"
SERVICE_DEST="/etc/systemd/system/opendeploy.service"
NGINX_AVAILABLE="/etc/nginx/sites-available/opendeploy"
NGINX_ENABLED="/etc/nginx/sites-enabled/opendeploy"

PURGE_DATA=0
KEEP_NGINX=0
KEEP_CONTAINERS=0

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[opendeploy]${NC} $*" >&2; }
warn() { echo -e "${YELLOW}[opendeploy]${NC} $*" >&2; }
err()  { echo -e "${RED}[opendeploy]${NC} $*" >&2; }

usage() {
  cat <<'EOF'
Usage: sudo ./scripts/uninstall.sh [--purge-data] [--keep-nginx] [--keep-containers]

  --purge-data      delete /var/lib/opendeploy (projects, state.db, logs)
  --keep-nginx      leave the nginx site configuration in place
  --keep-containers leave LXD containers created by OpenDeploy in place
EOF
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    err "Run as root (sudo)."
    exit 1
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --purge-data) PURGE_DATA=1; shift ;;
      --keep-nginx) KEEP_NGINX=1; shift ;;
      --keep-containers) KEEP_CONTAINERS=1; shift ;;
      -h|--help)   usage; exit 0 ;;
      *) err "Unknown option: $1"; usage; exit 1 ;;
    esac
  done
}

stop_service() {
  if systemctl list-unit-files opendeploy.service >/dev/null 2>&1; then
    log "Stopping and disabling opendeploy.service..."
    systemctl disable --now opendeploy.service >/dev/null 2>&1 || \
      warn "service was not running"
  fi

  if [[ -f "${SERVICE_DEST}" ]]; then
    rm -f "${SERVICE_DEST}"
    log "Removed systemd unit."
  fi
  systemctl daemon-reload || warn "daemon-reload failed"
  systemctl reset-failed opendeploy.service >/dev/null 2>&1 || true
}

remove_containers() {
  if [[ "${KEEP_CONTAINERS}" -eq 1 ]]; then
    warn "Keeping LXD containers (--keep-containers)."
    return
  fi
  if ! command -v lxc >/dev/null 2>&1; then
    log "lxc not found, skipping container cleanup."
    return
  fi
  local containers=""
  containers="$(lxc list --format csv --columns n 2>/dev/null | grep '^opendeploy-' || true)"
  if [[ -z "${containers}" ]]; then
    log "No OpenDeploy containers to remove."
    return
  fi
  local c
  for c in ${containers}; do
    log "Stopping container ${c}..."
    lxc stop --force "${c}" >/dev/null 2>&1 || warn "could not stop ${c}, trying delete anyway"
    log "Deleting container ${c}..."
    lxc delete --force "${c}" >/dev/null 2>&1 || warn "could not delete ${c}"
  done
  log "Removed OpenDeploy containers."
}

remove_binary() {
  if [[ -f "${BINARY_DEST}" ]]; then
    rm -f "${BINARY_DEST}"
    log "Removed ${BINARY_DEST}"
  fi
}

remove_config() {
  if [[ -d "${CONFIG_DIR}" ]]; then
    rm -rf "${CONFIG_DIR}"
    log "Removed ${CONFIG_DIR}"
  fi
  if [[ -d "${LOG_DIR}" ]]; then
    rm -rf "${LOG_DIR}"
    log "Removed ${LOG_DIR}"
  fi
  if [[ -d "${WWW_DIR}" ]]; then
    rm -rf "${WWW_DIR}"
    log "Removed ${WWW_DIR}"
  fi
}

remove_data() {
  if [[ "${PURGE_DATA}" -eq 1 ]]; then
    if [[ -d "${DATA_DIR}" ]]; then
      rm -rf "${DATA_DIR}"
      log "Removed ${DATA_DIR} (purge)"
    fi
  elif [[ -d "${DATA_DIR}" ]]; then
    warn "Keeping ${DATA_DIR} (pass --purge-data to delete projects and state.db)"
  fi
}

remove_nginx_site() {
  if [[ "${KEEP_NGINX}" -eq 1 ]]; then
    warn "Keeping nginx site (--keep-nginx)."
    return
  fi

  local removed=0
  if [[ -e "${NGINX_ENABLED}" || -L "${NGINX_ENABLED}" ]]; then
    rm -f "${NGINX_ENABLED}"
    removed=1
  fi
  if [[ -e "${NGINX_AVAILABLE}" || -L "${NGINX_AVAILABLE}" ]]; then
    rm -f "${NGINX_AVAILABLE}"
    removed=1
  fi
  local f
  for f in /etc/nginx/sites-enabled/frontend-* /etc/nginx/sites-enabled/backend-* /etc/nginx/sites-available/frontend-* /etc/nginx/sites-available/backend-*; do
    if [[ -e "${f}" || -L "${f}" ]]; then
      rm -f "${f}"
      removed=1
    fi
  done
  local db_path="${DATA_DIR}/state.db"
  if [[ -f "${db_path}" ]] && command -v sqlite3 >/dev/null 2>&1; then
    local domains=""
    domains="$(sqlite3 "${db_path}" "SELECT domain FROM projects WHERE domain IS NOT NULL AND domain != ''; SELECT domain FROM nginx_sites WHERE domain IS NOT NULL AND domain != '';" 2>/dev/null | sort -u || true)"
    local d
    for d in ${domains}; do
      [[ "${d}" == *"/"* ]] && continue
      [[ "${d}" == *".."* ]] && continue
      if [[ -e "/etc/nginx/sites-enabled/${d}" || -L "/etc/nginx/sites-enabled/${d}" ]]; then
        rm -f "/etc/nginx/sites-enabled/${d}"
        removed=1
      fi
      if [[ -e "/etc/nginx/sites-available/${d}" || -L "/etc/nginx/sites-available/${d}" ]]; then
        rm -f "/etc/nginx/sites-available/${d}"
        removed=1
      fi
      if [[ -e "/etc/nginx/sites-enabled/frontend-${d}" || -L "/etc/nginx/sites-enabled/frontend-${d}" ]]; then
        rm -f "/etc/nginx/sites-enabled/frontend-${d}"
        removed=1
      fi
      if [[ -e "/etc/nginx/sites-available/frontend-${d}" || -L "/etc/nginx/sites-available/frontend-${d}" ]]; then
        rm -f "/etc/nginx/sites-available/frontend-${d}"
        removed=1
      fi
      if [[ -e "/etc/nginx/sites-enabled/backend-${d}" || -L "/etc/nginx/sites-enabled/backend-${d}" ]]; then
        rm -f "/etc/nginx/sites-enabled/backend-${d}"
        removed=1
      fi
      if [[ -e "/etc/nginx/sites-available/backend-${d}" || -L "/etc/nginx/sites-available/backend-${d}" ]]; then
        rm -f "/etc/nginx/sites-available/backend-${d}"
        removed=1
      fi
    done
  fi
  if [[ "${removed}" -eq 1 ]]; then
    log "Removed nginx site."
  fi

  if command -v nginx >/dev/null 2>&1 && systemctl is-active --quiet nginx; then
    if nginx -t >/dev/null 2>&1; then
      systemctl reload nginx || warn "nginx reload failed"
      log "Reloaded nginx."
    else
      err "nginx config test failed; not reloading. Run: sudo nginx -t"
    fi
  fi
}

summary() {
  log ""
  log "Uninstall complete."
  log "Kept: LXD, nginx, NetworkManager and the apt packages from install.sh"
  log "      (shared with the rest of the system). Use 'apt autoremove'"
  log "      if you also want those gone."
}

main() {
  require_root
  parse_args "$@"

  stop_service
  remove_containers
  remove_nginx_site
  remove_binary
  remove_config
  remove_data
  summary
}

main "$@"
