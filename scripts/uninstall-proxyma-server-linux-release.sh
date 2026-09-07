#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${PROXYMA_SERVICE_NAME:-proxyma}"
SERVICE_USER="${PROXYMA_SERVICE_USER:-proxyma}"
SERVICE_GROUP="${PROXYMA_SERVICE_GROUP:-$SERVICE_USER}"
INSTALL_PREFIX="${PROXYMA_INSTALL_PREFIX:-/usr/local}"
LIFECYCLE_DIR="${PROXYMA_LIFECYCLE_DIR:-$INSTALL_PREFIX/lib/proxyma}"
CONFIG_DIR="${PROXYMA_CONFIG_DIR:-/etc/proxyma}"
DATA_DIR="${PROXYMA_DATA_DIR:-/var/lib/proxyma}"
SYSTEMD_TARGET="/etc/systemd/system/$SERVICE_NAME.service"

require_safe_absolute_dir() {
  local label="$1"
  local path="$2"

  case "$path" in
    /*) ;;
    *)
      echo "$label must be an absolute path: $path" >&2
      exit 1
      ;;
  esac

  case "$path" in
    /|/etc|/var|/var/lib|/usr|/usr/local)
      echo "$label is too broad and unsafe: $path" >&2
      exit 1
      ;;
  esac
}

require_safe_absolute_dir "PROXYMA_CONFIG_DIR" "$CONFIG_DIR"
require_safe_absolute_dir "PROXYMA_DATA_DIR" "$DATA_DIR"
require_safe_absolute_dir "PROXYMA_LIFECYCLE_DIR" "$LIFECYCLE_DIR"

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root, for example: sudo bash ./scripts/uninstall-proxyma-server-linux-release.sh --yes" >&2
  exit 1
fi

if [[ "${1:-}" != "--yes" && "${PROXYMA_UNINSTALL_YES:-}" != "1" ]]; then
  echo "This removes the $SERVICE_NAME service, installed binaries, lifecycle files, $CONFIG_DIR, $DATA_DIR, and the $SERVICE_USER user."
  echo "Re-run with --yes or set PROXYMA_UNINSTALL_YES=1 to continue."
  exit 2
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  systemctl disable "$SERVICE_NAME" >/dev/null 2>&1 || true
fi

rm -f "$SYSTEMD_TARGET"
rm -f "$INSTALL_PREFIX/bin/proxyma" "$INSTALL_PREFIX/bin/proxymactl"
rm -f "$LIFECYCLE_DIR/uninstall-proxyma-server-linux-release.sh"
rmdir "$LIFECYCLE_DIR" >/dev/null 2>&1 || true
rm -rf "$CONFIG_DIR" "$DATA_DIR"

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  systemctl reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true
fi

if id -u "$SERVICE_USER" >/dev/null 2>&1; then
  userdel "$SERVICE_USER"
fi

if getent group "$SERVICE_GROUP" >/dev/null && [[ "$SERVICE_GROUP" == "$SERVICE_USER" ]]; then
  groupdel "$SERVICE_GROUP" >/dev/null 2>&1 || true
fi

echo "Uninstalled $SERVICE_NAME."
