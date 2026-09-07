#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/deployment-endpoint.sh"

SERVICE_NAME="${PROXYMA_SERVICE_NAME:-proxyma}"
SERVICE_USER="${PROXYMA_SERVICE_USER:-proxyma}"
SERVICE_GROUP="${PROXYMA_SERVICE_GROUP:-$SERVICE_USER}"
INSTALL_PREFIX="${PROXYMA_INSTALL_PREFIX:-/usr/local}"
LIFECYCLE_DIR="${PROXYMA_LIFECYCLE_DIR:-$INSTALL_PREFIX/lib/proxyma}"
CONFIG_DIR="${PROXYMA_CONFIG_DIR:-/etc/proxyma}"
DATA_DIR="${PROXYMA_DATA_DIR:-/var/lib/proxyma}"
DIST_DIR="${PROXYMA_DIST_DIR:-$ROOT/dist}"
TARGET_OS="${GOOS:-linux}"
TARGET_ARCH="${GOARCH:-}"
BOOTSTRAP_ADMIN=true
BOOTSTRAP_ADMIN_FILE=""
BIND_ADDRESS="0.0.0.0"
LISTEN_PORT="8080"
BIND_ADDRESS_SET=false
LISTEN_PORT_SET=false
TRANSPORT_ACTION=""
TLS_CERT_SOURCE=""
TLS_KEY_SOURCE=""
TLS_SERVER_NAME=""

usage() {
  cat <<EOF
Usage: sudo bash ./scripts/install-proxyma-server-linux-release.sh [options]

Options:
  --bind-address ADDRESS Bind Proxyma to ADDRESS (default: 0.0.0.0).
  --port PORT            Listen on PORT (default: 8080).
  --http                 Serve plain HTTP for development/debugging.
  --https                Serve HTTPS using the currently configured TLS mode.
  --self-signed          Serve HTTPS with a Proxyma-generated certificate.
  --tls-cert FILE        Install a provided PEM certificate or full chain.
  --tls-key FILE         Install the matching unencrypted PEM private key.
  --tls-server-name NAME Add NAME to a generated certificate's SANs.
  --no-bootstrap-admin  Do not prompt to create the first administrator.
  -h, --help            Show this help text.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bind-address)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--bind-address requires a non-empty address." >&2
        usage >&2
        exit 1
      fi
      BIND_ADDRESS="$2"
      BIND_ADDRESS_SET=true
      shift
      ;;
    --port)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--port requires a value." >&2
        usage >&2
        exit 1
      fi
      LISTEN_PORT="$2"
      LISTEN_PORT_SET=true
      shift
      ;;
    --http)
      if [[ -n "$TRANSPORT_ACTION" && "$TRANSPORT_ACTION" != "http" ]]; then
        echo "--http conflicts with another transport mode." >&2
        exit 1
      fi
      TRANSPORT_ACTION="http"
      ;;
    --https)
      if [[ -n "$TRANSPORT_ACTION" && "$TRANSPORT_ACTION" != "https" ]]; then
        echo "--https conflicts with another transport mode." >&2
        exit 1
      fi
      TRANSPORT_ACTION="https"
      ;;
    --self-signed)
      if [[ -n "$TRANSPORT_ACTION" && "$TRANSPORT_ACTION" != "self_signed" ]]; then
        echo "--self-signed conflicts with another transport mode." >&2
        exit 1
      fi
      TRANSPORT_ACTION="self_signed"
      ;;
    --tls-cert)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--tls-cert requires a file." >&2
        exit 1
      fi
      TLS_CERT_SOURCE="$2"
      shift
      ;;
    --tls-key)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--tls-key requires a file." >&2
        exit 1
      fi
      TLS_KEY_SOURCE="$2"
      shift
      ;;
    --tls-server-name)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--tls-server-name requires a non-empty DNS name or IP address." >&2
        exit 1
      fi
      TLS_SERVER_NAME="$2"
      shift
      ;;
    --no-bootstrap-admin)
      BOOTSTRAP_ADMIN=false
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

validate_bind_address "$BIND_ADDRESS"
validate_port "Port" "$LISTEN_PORT"
if [[ -n "$TLS_CERT_SOURCE" || -n "$TLS_KEY_SOURCE" ]]; then
  if [[ -z "$TLS_CERT_SOURCE" || -z "$TLS_KEY_SOURCE" ]]; then
    echo "--tls-cert and --tls-key must be supplied together." >&2
    exit 1
  fi
  if [[ -n "$TRANSPORT_ACTION" && "$TRANSPORT_ACTION" != "provided" ]]; then
    echo "A provided certificate conflicts with --http, --https, or --self-signed." >&2
    exit 1
  fi
  TRANSPORT_ACTION="provided"
fi
if [[ -n "$TLS_SERVER_NAME" ]]; then
  validate_bind_address "$TLS_SERVER_NAME"
  if [[ "$TRANSPORT_ACTION" == "provided" || "$TRANSPORT_ACTION" == "http" || "$TRANSPORT_ACTION" == "https" ]]; then
    echo "--tls-server-name may be used only with generated self-signed HTTPS." >&2
    exit 1
  fi
  TRANSPORT_ACTION="self_signed"
fi

if [[ "$TARGET_OS" != "linux" ]]; then
  echo "This installer expects Linux release artifacts; got GOOS=$TARGET_OS" >&2
  exit 1
fi

if [[ -z "$TARGET_ARCH" ]]; then
  case "$(uname -m)" in
    x86_64) TARGET_ARCH="amd64" ;;
    aarch64|arm64) TARGET_ARCH="arm64" ;;
    *)
      echo "Unsupported machine architecture: $(uname -m). Set GOARCH explicitly." >&2
      exit 1
      ;;
  esac
fi

SERVER_BIN="$DIST_DIR/Proxyma-linux-$TARGET_ARCH"
CTL_BIN="$DIST_DIR/proxymactl-linux-$TARGET_ARCH"
SYSTEMD_TARGET="/etc/systemd/system/$SERVICE_NAME.service"
INSTALLED_SERVER="$INSTALL_PREFIX/bin/proxyma"
INSTALLED_CTL="$INSTALL_PREFIX/bin/proxymactl"

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
  echo "Run as root, for example: sudo GOARCH=$TARGET_ARCH bash ./scripts/install-proxyma-server-linux-release.sh" >&2
  exit 1
fi

for required in "$SERVER_BIN" "$CTL_BIN"; do
  if [[ ! -f "$required" ]]; then
    echo "Missing required file: $required" >&2
    echo "Build first, for example: GOOS=linux GOARCH=$TARGET_ARCH bash ./scripts/build-server-release.sh" >&2
    exit 1
  fi
done
if [[ ! -f "$ROOT/scripts/uninstall-proxyma-server-linux-release.sh" ]]; then
  echo "Missing required file: $ROOT/scripts/uninstall-proxyma-server-linux-release.sh" >&2
  exit 1
fi

if [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  for source_file in "$TLS_CERT_SOURCE" "$TLS_KEY_SOURCE"; do
    if [[ -L "$source_file" || ! -f "$source_file" || ! -r "$source_file" ]]; then
      echo "TLS input must be a readable regular file and not a symbolic link: $source_file" >&2
      exit 1
    fi
  done
  "$CTL_BIN" certificates validate --cert-file "$TLS_CERT_SOURCE" --key-file "$TLS_KEY_SOURCE" >/dev/null
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
fi

if ! getent group "$SERVICE_GROUP" >/dev/null; then
  groupadd --system "$SERVICE_GROUP"
fi

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system \
    --gid "$SERVICE_GROUP" \
    --home-dir "$DATA_DIR" \
    --shell /usr/sbin/nologin \
    "$SERVICE_USER"
fi

install -d -m 0755 "$INSTALL_PREFIX/bin"
install -m 0755 "$SERVER_BIN" "$INSTALLED_SERVER"
install -m 0755 "$CTL_BIN" "$INSTALLED_CTL"
install -d -m 0755 "$LIFECYCLE_DIR"
install -m 0755 "$ROOT/scripts/uninstall-proxyma-server-linux-release.sh" "$LIFECYCLE_DIR/uninstall-proxyma-server-linux-release.sh"

install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0700 \
  "$CONFIG_DIR" \
  "$CONFIG_DIR/targets" \
  "$CONFIG_DIR/policies" \
  "$CONFIG_DIR/agents" \
  "$CONFIG_DIR/users" \
  "$CONFIG_DIR/interactions" \
  "$CONFIG_DIR/secrets" \
  "$CONFIG_DIR/secrets/private_keys" \
  "$CONFIG_DIR/tls" \
  "$DATA_DIR" \
  "$DATA_DIR/tasks" \
  "$DATA_DIR/interactions" \
  "$DATA_DIR/user_sessions" \
  "$DATA_DIR/credential_tokens" \
  "$DATA_DIR/.ssh"

TLS_DIR="$CONFIG_DIR/tls"
SELF_SIGNED_CERT="$TLS_DIR/self-signed.crt"
SELF_SIGNED_KEY="$TLS_DIR/self-signed.key"
PROVIDED_CERT="$TLS_DIR/provided.crt"
PROVIDED_KEY="$TLS_DIR/provided.key"

if [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  "$INSTALLED_CTL" certificates install \
    --cert-file "$TLS_CERT_SOURCE" \
    --key-file "$TLS_KEY_SOURCE" \
    --dest-cert-file "$PROVIDED_CERT" \
    --dest-key-file "$PROVIDED_KEY" >/dev/null
  chown "$SERVICE_USER:$SERVICE_GROUP" "$PROVIDED_CERT" "$PROVIDED_KEY"
  chmod 0644 "$PROVIDED_CERT"
  chmod 0600 "$PROVIDED_KEY"
  echo "Installed provided TLS certificate and private key in $TLS_DIR"
fi

KNOWN_HOSTS_PATH="$DATA_DIR/.ssh/known_hosts"
if [[ ! -e "$KNOWN_HOSTS_PATH" ]]; then
  install -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0600 /dev/null "$KNOWN_HOSTS_PATH"
else
  chown "$SERVICE_USER:$SERVICE_GROUP" "$KNOWN_HOSTS_PATH"
  chmod 0600 "$KNOWN_HOSTS_PATH"
fi

CONFIG_FILE="$CONFIG_DIR/proxyma.config.json"
CONFIG_CREATED=false
LEGACY_TRANSPORT=false
LISTEN_ADDRESS="$(format_listen_address "$BIND_ADDRESS" "$LISTEN_PORT")"
CONFIG_SCHEME="https"
CONFIG_TLS_MODE="self_signed"
CONFIG_TLS_CERT="$SELF_SIGNED_CERT"
CONFIG_TLS_KEY="$SELF_SIGNED_KEY"
if [[ "$TRANSPORT_ACTION" == "http" ]]; then
  CONFIG_SCHEME="http"
elif [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  CONFIG_TLS_MODE="provided"
  CONFIG_TLS_CERT="$PROVIDED_CERT"
  CONFIG_TLS_KEY="$PROVIDED_KEY"
fi
if [[ -L "$CONFIG_FILE" ]]; then
  echo "Refusing to use a symbolic link as the configuration file: $CONFIG_FILE" >&2
  exit 1
elif [[ ! -e "$CONFIG_FILE" ]]; then
  CONFIG_CREATED=true
  cat > "$CONFIG_FILE" <<EOF
{
  "server": {
    "listen_address": "$LISTEN_ADDRESS",
    "scheme": "$CONFIG_SCHEME",
    "tls": {
      "mode": "$CONFIG_TLS_MODE",
      "certificate_file": "$CONFIG_TLS_CERT",
      "private_key_file": "$CONFIG_TLS_KEY",
      "server_name": "$TLS_SERVER_NAME"
    }
  },
  "paths": {
    "target_dir": "$CONFIG_DIR/targets",
    "policy_dir": "$CONFIG_DIR/policies",
    "agent_dir": "$CONFIG_DIR/agents",
    "user_dir": "$CONFIG_DIR/users",
    "task_dir": "$DATA_DIR/tasks",
    "interaction_dir": "$DATA_DIR/interactions",
    "interaction_history_db": "$DATA_DIR/interaction-history.db",
    "interaction_config_dir": "$CONFIG_DIR/interactions",
    "user_session_dir": "$DATA_DIR/user_sessions",
    "credential_token_dir": "$DATA_DIR/credential_tokens",
    "known_hosts_path": "$DATA_DIR/.ssh/known_hosts"
  },
  "execution": {
    "command_timeout": "5m",
    "interaction_timeout": "24h",
    "output_limit": 1048576,
    "require_command_approval": true
  },
  "security": {
    "user_session_ttl": "12h",
    "user_activation_token_ttl": "12h",
    "user_activation_token_max_ttl": "48h"
  },
  "logging": {
    "debug": false
  }
}
EOF
  echo "Created $CONFIG_FILE"
elif [[ ! -f "$CONFIG_FILE" ]]; then
  echo "Expected an existing regular configuration file but found: $CONFIG_FILE" >&2
  exit 1
else
  if ! grep -Eq '"scheme"[[:space:]]*:' "$CONFIG_FILE"; then
    LEGACY_TRANSPORT=true
  fi
  if [[ "$BIND_ADDRESS_SET" == true || "$LISTEN_PORT_SET" == true ]]; then
    EXISTING_LISTEN_ADDRESS="$(read_json_listen_address "$CONFIG_FILE")"
    parse_listen_address "$EXISTING_LISTEN_ADDRESS"
    if [[ "$BIND_ADDRESS_SET" != true ]]; then
      BIND_ADDRESS="$PARSED_BIND_ADDRESS"
    fi
    if [[ "$LISTEN_PORT_SET" != true ]]; then
      LISTEN_PORT="$PARSED_PORT"
    fi
    LISTEN_ADDRESS="$(format_listen_address "$BIND_ADDRESS" "$LISTEN_PORT")"
    update_json_listen_address "$CONFIG_FILE" "$LISTEN_ADDRESS"
    echo "Updated Proxyma listen address in $CONFIG_FILE to $LISTEN_ADDRESS"
  else
    LISTEN_ADDRESS=""
    echo "Existing configuration found; preserving $CONFIG_FILE"
  fi
fi

TRANSPORT_UPDATE=("$INSTALLED_CTL" config update-transport --file "$CONFIG_FILE")
if [[ "$LEGACY_TRANSPORT" == true && ( -z "$TRANSPORT_ACTION" || "$TRANSPORT_ACTION" == "http" || "$TRANSPORT_ACTION" == "https" ) ]]; then
  LEGACY_SCHEME="http"
  if [[ "$TRANSPORT_ACTION" == "https" ]]; then
    LEGACY_SCHEME="https"
  fi
  TRANSPORT_UPDATE+=(--scheme "$LEGACY_SCHEME" --tls-mode self_signed --tls-cert-file "$SELF_SIGNED_CERT" --tls-key-file "$SELF_SIGNED_KEY" --tls-server-name=)
elif [[ "$TRANSPORT_ACTION" == "http" ]]; then
  TRANSPORT_UPDATE+=(--scheme http)
elif [[ "$TRANSPORT_ACTION" == "https" ]]; then
  TRANSPORT_UPDATE+=(--scheme https)
elif [[ "$TRANSPORT_ACTION" == "self_signed" ]]; then
  TRANSPORT_UPDATE+=(--scheme https --tls-mode self_signed --tls-cert-file "$SELF_SIGNED_CERT" --tls-key-file "$SELF_SIGNED_KEY" --tls-server-name "$TLS_SERVER_NAME")
elif [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  TRANSPORT_UPDATE+=(--scheme https --tls-mode provided --tls-cert-file "$PROVIDED_CERT" --tls-key-file "$PROVIDED_KEY" --tls-server-name=)
fi
"${TRANSPORT_UPDATE[@]}" >/dev/null
chown "$SERVICE_USER:$SERVICE_GROUP" "$CONFIG_FILE"
chmod 0600 "$CONFIG_FILE"

bootstrap_admin() {
  local admin_password admin_password_confirm

  if find "$CONFIG_DIR/users" -maxdepth 1 -type f -name '*.json' -print -quit | grep -q .; then
    echo "Existing user configuration found; skipping administrator bootstrap."
    return
  fi

  if [[ "$BOOTSTRAP_ADMIN" == false ]]; then
    echo "Administrator bootstrap disabled by --no-bootstrap-admin."
    return
  fi

  if [[ ! -t 0 ]]; then
    echo "Cannot bootstrap the first administrator without an interactive terminal." >&2
    echo "Run interactively, or pass --no-bootstrap-admin and create the administrator with proxymactl." >&2
    exit 1
  fi

  if ! command -v runuser >/dev/null 2>&1; then
    echo "Cannot bootstrap the first administrator: runuser is not installed." >&2
    echo "Install util-linux, or pass --no-bootstrap-admin and create the administrator with proxymactl." >&2
    exit 1
  fi

  echo "No users found. Create the first Proxyma administrator."
  while true; do
    read -rsp "Initial Proxyma admin password: " admin_password
    printf '\n'
    read -rsp "Confirm Proxyma admin password: " admin_password_confirm
    printf '\n'

    if [[ -z "$admin_password" ]]; then
      echo "Password cannot be empty." >&2
    elif [[ "$admin_password" != "$admin_password_confirm" ]]; then
      echo "Passwords do not match; try again." >&2
    else
      break
    fi
  done

  BOOTSTRAP_ADMIN_FILE="$(mktemp "$CONFIG_DIR/.bootstrap-admin.XXXXXX.json")"
  trap 'rm -f "${BOOTSTRAP_ADMIN_FILE:-}"' EXIT
  chown "$SERVICE_USER:$SERVICE_GROUP" "$BOOTSTRAP_ADMIN_FILE"
  chmod 0600 "$BOOTSTRAP_ADMIN_FILE"
  cat > "$BOOTSTRAP_ADMIN_FILE" <<'EOF'
{
  "username": "admin",
  "name": "Proxyma Administrator",
  "admin": true,
  "theme": "light"
}
EOF

  printf '%s\n' "$admin_password" | runuser -u "$SERVICE_USER" -- "$INSTALLED_CTL" \
    --user-dir "$CONFIG_DIR/users" \
    --agent-dir "$CONFIG_DIR/agents" \
    --target-dir "$CONFIG_DIR/targets" \
    --policy-dir "$CONFIG_DIR/policies" \
    users create --file "$BOOTSTRAP_ADMIN_FILE" --password-stdin

  admin_password=""
  admin_password_confirm=""
  rm -f "$BOOTSTRAP_ADMIN_FILE"
  BOOTSTRAP_ADMIN_FILE=""
  trap - EXIT
  echo "Created the initial Proxyma administrator with username: admin"
}

bootstrap_admin

cat > "$SYSTEMD_TARGET" <<EOF
[Unit]
Description=Proxyma privileged access manager for AI agents
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$CONFIG_DIR
ExecStart=$INSTALLED_SERVER --config $CONFIG_DIR/proxyma.config.json
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths=$CONFIG_DIR $DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
generated_working_directory="$(sed -n 's/^WorkingDirectory=//p' "$SYSTEMD_TARGET")"
require_safe_absolute_dir "generated systemd WorkingDirectory" "$generated_working_directory"
chmod 0644 "$SYSTEMD_TARGET"
systemctl daemon-reload
systemctl enable --now "$SERVICE_NAME"

echo "Installed $SERVICE_NAME."
echo "Service: systemctl status $SERVICE_NAME"
echo "Logs:    journalctl -u $SERVICE_NAME -f"
if [[ "$CONFIG_CREATED" != true && -z "$TRANSPORT_ACTION" ]]; then
  echo "Endpoint: see server.scheme and server.listen_address in $CONFIG_FILE"
else
  DISPLAY_SCHEME="https"
  if [[ "$TRANSPORT_ACTION" == "http" ]]; then
    DISPLAY_SCHEME="http"
  fi
  if [[ "$BIND_ADDRESS" == "0.0.0.0" || "$BIND_ADDRESS" == "[::]" ]]; then
    DISPLAY_ADDRESS="<server-ip>"
  else
    DISPLAY_ADDRESS="$BIND_ADDRESS"
  fi
  echo "Endpoint: $DISPLAY_SCHEME://$DISPLAY_ADDRESS:$LISTEN_PORT/"
fi
