#!/usr/bin/env bash
set -euo pipefail

SOURCE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_ROOT="${PROXYMA_DOCKER_INSTALL_ROOT:-/opt/proxyma}"
source "$SOURCE_ROOT/scripts/deployment-endpoint.sh"

SOURCE_COMPOSE_FILE="$SOURCE_ROOT/docker-compose.example.yml"
SOURCE_INSTALLER="$SOURCE_ROOT/scripts/install-proxyma-server-docker.sh"
SOURCE_UNINSTALLER="$SOURCE_ROOT/scripts/uninstall-proxyma-server-docker.sh"
SOURCE_HELPER="$SOURCE_ROOT/scripts/deployment-endpoint.sh"
COMPOSE_FILE="$INSTALL_ROOT/docker-compose.example.yml"
CONFIG_DIR="$INSTALL_ROOT/proxyma-config"
DATA_DIR="$INSTALL_ROOT/proxyma-data"
COMPOSE_ENV_FILE="$INSTALL_ROOT/proxyma-compose.env"
CONTAINER_UID=10001
CONTAINER_GID=10001
BOOTSTRAP_ADMIN=true
BOOTSTRAP_ADMIN_FILE=""
RELEASE_VERSION=""
if [[ -f "$SOURCE_ROOT/RELEASE" ]]; then
  RELEASE_VERSION="$(sed -n 's/^version=//p' "$SOURCE_ROOT/RELEASE")"
fi
if [[ "$RELEASE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  DEFAULT_IMAGE="ghcr.io/ascemama/proxyma:${RELEASE_VERSION#v}"
else
  DEFAULT_IMAGE="ghcr.io/ascemama/proxyma:latest"
fi
IMAGE_REFERENCE="${PROXYMA_IMAGE:-$DEFAULT_IMAGE}"
IMAGE_REFERENCE_SET=false
if [[ -n "${PROXYMA_IMAGE:-}" ]]; then
  IMAGE_REFERENCE_SET=true
fi
PULL_IMAGE=true
BIND_ADDRESS="0.0.0.0"
HOST_PORT="8080"
BIND_ADDRESS_SET=false
HOST_PORT_SET=false
TRANSPORT_ACTION=""
TLS_CERT_SOURCE=""
TLS_KEY_SOURCE=""
TLS_SERVER_NAME=""

usage() {
  cat <<EOF
Usage: sudo bash ./scripts/install-proxyma-server-docker.sh [options]

Options:
  --bind-address ADDRESS Publish Proxyma on host ADDRESS (default: 0.0.0.0).
  --host-port PORT       Publish Proxyma on host PORT (default: 8080).
  --http                 Serve plain HTTP for development/debugging.
  --https                Serve HTTPS using the currently configured TLS mode.
  --self-signed          Serve HTTPS with a Proxyma-generated certificate.
  --tls-cert FILE        Install a provided PEM certificate or full chain.
  --tls-key FILE         Install the matching unencrypted PEM private key.
  --tls-server-name NAME Add NAME to a generated certificate's SANs.
  --image IMAGE          Run IMAGE instead of the image matching this release.
  --no-pull              Use an image already present on the host without pulling it.
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
    --host-port)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--host-port requires a value." >&2
        usage >&2
        exit 1
      fi
      HOST_PORT="$2"
      HOST_PORT_SET=true
      shift
      ;;
    --image)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--image requires a non-empty image reference." >&2
        usage >&2
        exit 1
      fi
      IMAGE_REFERENCE="$2"
      IMAGE_REFERENCE_SET=true
      shift
      ;;
    --no-pull)
      PULL_IMAGE=false
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
validate_port "Host port" "$HOST_PORT"
if [[ -z "$IMAGE_REFERENCE" || "$IMAGE_REFERENCE" =~ [[:space:]] ]]; then
  echo "The Docker image reference must be non-empty and contain no whitespace." >&2
  exit 1
fi
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

validate_tls_source() {
  local label="$1"
  local path="$2"
  if [[ -L "$path" ]]; then
    echo "Refusing to use a symbolic link as the $label: $path" >&2
    exit 1
  fi
  if [[ ! -f "$path" || ! -r "$path" ]]; then
    echo "The $label must be a readable regular file: $path" >&2
    exit 1
  fi
}

if [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  validate_tls_source "TLS certificate" "$TLS_CERT_SOURCE"
  validate_tls_source "TLS private key" "$TLS_KEY_SOURCE"
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This installer supports Linux hosts only." >&2
  exit 1
fi

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root, for example: sudo bash ./scripts/install-proxyma-server-docker.sh" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is not installed or is not available in PATH." >&2
  echo "Install Docker Engine and the Docker Compose v2 plugin, then retry." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose v2 is not available." >&2
  echo "Install the Docker Compose plugin so 'docker compose' is available." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Cannot connect to the Docker daemon." >&2
  echo "Confirm that rootful Docker is running, then retry." >&2
  exit 1
fi

docker_security_options="$(docker info --format '{{json .SecurityOptions}}' 2>/dev/null)"
if grep -qi rootless <<<"$docker_security_options"; then
  echo "Rootless Docker is not supported by this installer." >&2
  echo "Use a standard rootful Docker daemon so bind mounts can be owned by UID $CONTAINER_UID." >&2
  exit 1
fi

if grep -qi 'name=userns' <<<"$docker_security_options"; then
  echo "Docker userns-remap is not supported by this installer." >&2
  echo "Use a standard rootful Docker daemon without user namespace remapping." >&2
  exit 1
fi

if ! docker compose up --help 2>/dev/null | grep -q -- '--wait'; then
  echo "This installer requires a Docker Compose v2 release that supports 'up --wait'." >&2
  echo "Upgrade the Docker Compose plugin, then retry." >&2
  exit 1
fi

for required in install find mktemp; do
  if ! command -v "$required" >/dev/null 2>&1; then
    echo "Required host command is not installed: $required" >&2
    exit 1
  fi
done

for required_file in "$SOURCE_COMPOSE_FILE" "$SOURCE_INSTALLER" "$SOURCE_UNINSTALLER" "$SOURCE_HELPER"; do
  if [[ ! -f "$required_file" || -L "$required_file" ]]; then
    echo "Missing or unsafe Docker release file: $required_file" >&2
    exit 1
  fi
done

if [[ "$INSTALL_ROOT" != /* || "$INSTALL_ROOT" == "/" ]]; then
  echo "The Docker installation root must be an absolute path other than /: $INSTALL_ROOT" >&2
  exit 1
fi
if [[ -L "$INSTALL_ROOT" ]]; then
  echo "Refusing to use a symbolic link as the Docker installation root: $INSTALL_ROOT" >&2
  exit 1
fi
install -d -o root -g root -m 0755 "$INSTALL_ROOT" "$INSTALL_ROOT/scripts"

install_release_file() {
  local mode="$1"
  local source_path="$2"
  local destination_path="$3"
  if [[ -e "$destination_path" && "$source_path" -ef "$destination_path" ]]; then
    chmod "$mode" "$destination_path"
  else
    install -o root -g root -m "$mode" "$source_path" "$destination_path"
  fi
}

install_release_file 0644 "$SOURCE_COMPOSE_FILE" "$COMPOSE_FILE"
install_release_file 0755 "$SOURCE_INSTALLER" "$INSTALL_ROOT/scripts/install-proxyma-server-docker.sh"
install_release_file 0755 "$SOURCE_UNINSTALLER" "$INSTALL_ROOT/scripts/uninstall-proxyma-server-docker.sh"
install_release_file 0644 "$SOURCE_HELPER" "$INSTALL_ROOT/scripts/deployment-endpoint.sh"
if [[ -f "$SOURCE_ROOT/RELEASE" && ! -L "$SOURCE_ROOT/RELEASE" ]]; then
  install_release_file 0644 "$SOURCE_ROOT/RELEASE" "$INSTALL_ROOT/RELEASE"
fi

read_compose_endpoint() {
  local key="$1"
  local matches=()
  mapfile -t matches < <(sed -n "s/^${key}=//p" "$COMPOSE_ENV_FILE")
  if [[ "${#matches[@]}" -ne 1 || -z "${matches[0]}" ]]; then
    echo "Expected exactly one non-empty $key setting in $COMPOSE_ENV_FILE." >&2
    exit 1
  fi
  printf '%s' "${matches[0]}"
}

read_optional_compose_setting() {
  local key="$1"
  local matches=()
  mapfile -t matches < <(sed -n "s/^${key}=//p" "$COMPOSE_ENV_FILE")
  if [[ "${#matches[@]}" -gt 1 ]]; then
    echo "Expected at most one $key setting in $COMPOSE_ENV_FILE." >&2
    exit 1
  fi
  if [[ "${#matches[@]}" -eq 1 ]]; then
    printf '%s' "${matches[0]}"
  fi
}

if [[ -L "$COMPOSE_ENV_FILE" ]]; then
  echo "Refusing to use a symbolic link as the Compose environment file: $COMPOSE_ENV_FILE" >&2
  exit 1
elif [[ -e "$COMPOSE_ENV_FILE" && ! -f "$COMPOSE_ENV_FILE" ]]; then
  echo "Expected a regular Compose environment file but found: $COMPOSE_ENV_FILE" >&2
  exit 1
elif [[ -f "$COMPOSE_ENV_FILE" ]]; then
  EXISTING_BIND_ADDRESS="$(read_compose_endpoint PROXYMA_BIND_ADDRESS)"
  EXISTING_HOST_PORT="$(read_compose_endpoint PROXYMA_HOST_PORT)"
  validate_bind_address "$EXISTING_BIND_ADDRESS"
  validate_port "Existing host port" "$EXISTING_HOST_PORT"
  if [[ "$BIND_ADDRESS_SET" != true ]]; then
    BIND_ADDRESS="$EXISTING_BIND_ADDRESS"
  fi
  if [[ "$HOST_PORT_SET" != true ]]; then
    HOST_PORT="$EXISTING_HOST_PORT"
  fi
  EXISTING_IMAGE_REFERENCE="$(read_optional_compose_setting PROXYMA_IMAGE)"
  if [[ "$IMAGE_REFERENCE_SET" != true && -z "$RELEASE_VERSION" && -n "$EXISTING_IMAGE_REFERENCE" ]]; then
    IMAGE_REFERENCE="$EXISTING_IMAGE_REFERENCE"
  fi
fi

if [[ -z "$IMAGE_REFERENCE" || "$IMAGE_REFERENCE" =~ [[:space:]] ]]; then
  echo "The Docker image reference in $COMPOSE_ENV_FILE is invalid." >&2
  exit 1
fi

COMPOSE_ENV_TEMP="$(mktemp "$INSTALL_ROOT/.proxyma-compose.env.XXXXXX")"
trap 'rm -f "${COMPOSE_ENV_TEMP:-}" "${BOOTSTRAP_ADMIN_FILE:-}"' EXIT
cat > "$COMPOSE_ENV_TEMP" <<EOF
PROXYMA_BIND_ADDRESS=$BIND_ADDRESS
PROXYMA_HOST_PORT=$HOST_PORT
PROXYMA_IMAGE=$IMAGE_REFERENCE
EOF
chmod 0644 "$COMPOSE_ENV_TEMP"
mv -f "$COMPOSE_ENV_TEMP" "$COMPOSE_ENV_FILE"
COMPOSE_ENV_TEMP=""
echo "Saved Docker publication settings in $COMPOSE_ENV_FILE"

deployment_dirs=(
  "$CONFIG_DIR"
  "$CONFIG_DIR/targets"
  "$CONFIG_DIR/policies"
  "$CONFIG_DIR/agents"
  "$CONFIG_DIR/users"
  "$CONFIG_DIR/interactions"
  "$CONFIG_DIR/secrets"
  "$CONFIG_DIR/secrets/private_keys"
  "$CONFIG_DIR/tls"
  "$DATA_DIR"
  "$DATA_DIR/tasks"
  "$DATA_DIR/interactions"
  "$DATA_DIR/user_sessions"
  "$DATA_DIR/credential_tokens"
  "$DATA_DIR/.ssh"
)

for deployment_dir in "${deployment_dirs[@]}"; do
  if [[ -L "$deployment_dir" ]]; then
    echo "Refusing to use a symbolic link as a deployment directory: $deployment_dir" >&2
    exit 1
  fi
  if [[ -e "$deployment_dir" && ! -d "$deployment_dir" ]]; then
    echo "Expected a deployment directory but found another file type: $deployment_dir" >&2
    exit 1
  fi
done

install -d -o "$CONTAINER_UID" -g "$CONTAINER_GID" -m 0700 "${deployment_dirs[@]}"

CONFIG_FILE="$CONFIG_DIR/proxyma.config.json"
CONFIG_CREATED=false
LEGACY_TRANSPORT=false
TLS_DIR="$CONFIG_DIR/tls"
SELF_SIGNED_CERT="/etc/proxyma/tls/self-signed.crt"
SELF_SIGNED_KEY="/etc/proxyma/tls/self-signed.key"
PROVIDED_CERT="/etc/proxyma/tls/provided.crt"
PROVIDED_KEY="/etc/proxyma/tls/provided.key"
if [[ -L "$CONFIG_FILE" ]]; then
  echo "Refusing to use a symbolic link as the configuration file: $CONFIG_FILE" >&2
  exit 1
fi
if [[ -e "$CONFIG_FILE" && ! -f "$CONFIG_FILE" ]]; then
  echo "Expected a regular configuration file but found: $CONFIG_FILE" >&2
  exit 1
fi

if [[ ! -e "$CONFIG_FILE" ]]; then
  CONFIG_CREATED=true
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

  cat > "$CONFIG_FILE" <<EOF
{
  "server": {
    "listen_address": "0.0.0.0:8080",
    "scheme": "$CONFIG_SCHEME",
    "tls": {
      "mode": "$CONFIG_TLS_MODE",
      "certificate_file": "$CONFIG_TLS_CERT",
      "private_key_file": "$CONFIG_TLS_KEY",
      "server_name": "$TLS_SERVER_NAME"
    }
  },
  "paths": {
    "target_dir": "/etc/proxyma/targets",
    "policy_dir": "/etc/proxyma/policies",
    "agent_dir": "/etc/proxyma/agents",
    "user_dir": "/etc/proxyma/users",
    "task_dir": "/var/lib/proxyma/tasks",
    "interaction_dir": "/var/lib/proxyma/interactions",
    "interaction_history_db": "/var/lib/proxyma/interaction-history.db",
    "interaction_config_dir": "/etc/proxyma/interactions",
    "user_session_dir": "/var/lib/proxyma/user_sessions",
    "credential_token_dir": "/var/lib/proxyma/credential_tokens",
    "known_hosts_path": "/var/lib/proxyma/.ssh/known_hosts"
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
  chown "$CONTAINER_UID:$CONTAINER_GID" "$CONFIG_FILE"
  chmod 0600 "$CONFIG_FILE"
  echo "Created $CONFIG_FILE"
else
  if ! grep -Eq '"scheme"[[:space:]]*:' "$CONFIG_FILE"; then
    LEGACY_TRANSPORT=true
  fi
  echo "Existing configuration found; preserving $CONFIG_FILE"
fi

chown -R "$CONTAINER_UID:$CONTAINER_GID" "$CONFIG_DIR" "$DATA_DIR"
chmod -R u=rwX,g=,o= "$CONFIG_DIR" "$DATA_DIR"

export PROXYMA_IMAGE="$IMAGE_REFERENCE"

compose() {
  docker compose --env-file "$COMPOSE_ENV_FILE" --project-directory "$INSTALL_ROOT" -f "$COMPOSE_FILE" "$@"
}

if [[ "$PULL_IMAGE" == true ]]; then
  echo "Pulling $PROXYMA_IMAGE"
  compose pull Proxyma
else
  if ! docker image inspect "$PROXYMA_IMAGE" >/dev/null 2>&1; then
    echo "The image selected with --no-pull is not present locally: $PROXYMA_IMAGE" >&2
    exit 1
  fi
  echo "Using local image without pulling: $PROXYMA_IMAGE"
fi

if [[ "$TRANSPORT_ACTION" == "provided" ]]; then
  CERT_TEMP="$(mktemp "$TLS_DIR/.provided-cert.XXXXXX")"
  KEY_TEMP="$(mktemp "$TLS_DIR/.provided-key.XXXXXX")"
  trap 'rm -f "${CERT_TEMP:-}" "${KEY_TEMP:-}" "${BOOTSTRAP_ADMIN_FILE:-}"' EXIT
  install -o "$CONTAINER_UID" -g "$CONTAINER_GID" -m 0644 "$TLS_CERT_SOURCE" "$CERT_TEMP"
  install -o "$CONTAINER_UID" -g "$CONTAINER_GID" -m 0600 "$TLS_KEY_SOURCE" "$KEY_TEMP"
  compose run --rm --no-deps -T --entrypoint /usr/local/bin/proxymactl Proxyma \
    certificates install \
    --cert-file "/etc/proxyma/tls/$(basename "$CERT_TEMP")" \
    --key-file "/etc/proxyma/tls/$(basename "$KEY_TEMP")" \
    --dest-cert-file "$PROVIDED_CERT" \
    --dest-key-file "$PROVIDED_KEY" >/dev/null
  rm -f "$CERT_TEMP" "$KEY_TEMP"
  CERT_TEMP=""
  KEY_TEMP=""
  echo "Installed provided TLS certificate and private key in $TLS_DIR"
fi

TRANSPORT_UPDATE=(config update-transport --file /etc/proxyma/proxyma.config.json)
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
compose run --rm --no-deps -T --entrypoint /usr/local/bin/proxymactl Proxyma "${TRANSPORT_UPDATE[@]}" >/dev/null

cleanup_bootstrap_file() {
  if [[ -n "$BOOTSTRAP_ADMIN_FILE" ]]; then
    rm -f "$BOOTSTRAP_ADMIN_FILE"
  fi
}

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
  chown "$CONTAINER_UID:$CONTAINER_GID" "$BOOTSTRAP_ADMIN_FILE"
  chmod 0600 "$BOOTSTRAP_ADMIN_FILE"
  cat > "$BOOTSTRAP_ADMIN_FILE" <<'EOF'
{
  "username": "admin",
  "name": "Proxyma Administrator",
  "admin": true,
  "theme": "light"
}
EOF

  printf '%s\n' "$admin_password" | compose run --rm --no-deps -T \
    --entrypoint /usr/local/bin/proxymactl \
    Proxyma \
    --user-dir /etc/proxyma/users \
    --agent-dir /etc/proxyma/agents \
    --target-dir /etc/proxyma/targets \
    --policy-dir /etc/proxyma/policies \
    users create --file "/etc/proxyma/$(basename "$BOOTSTRAP_ADMIN_FILE")" --password-stdin

  admin_password=""
  admin_password_confirm=""
  cleanup_bootstrap_file
  BOOTSTRAP_ADMIN_FILE=""
  echo "Created the initial Proxyma administrator with username: admin"
}

bootstrap_admin

echo "Starting Proxyma and waiting for its health check."
if ! compose up --detach --wait; then
  echo "Proxyma failed to become healthy. Current status:" >&2
  compose ps >&2 || true
  echo "Recent Proxyma logs:" >&2
  compose logs --no-color --tail 100 Proxyma >&2 || true
  exit 1
fi

echo "Proxyma is installed and healthy."
echo "Image:  $PROXYMA_IMAGE"
echo "Files:  $INSTALL_ROOT"
echo "Status: docker compose --env-file $COMPOSE_ENV_FILE --project-directory $INSTALL_ROOT -f $COMPOSE_FILE ps"
echo "Logs:   docker compose --env-file $COMPOSE_ENV_FILE --project-directory $INSTALL_ROOT -f $COMPOSE_FILE logs -f Proxyma"
if [[ "$BIND_ADDRESS" == "0.0.0.0" || "$BIND_ADDRESS" == "[::]" ]]; then
  DISPLAY_ADDRESS="<server-ip>"
else
  DISPLAY_ADDRESS="$BIND_ADDRESS"
fi
if [[ "$CONFIG_CREATED" != true && -z "$TRANSPORT_ACTION" ]]; then
  echo "Endpoint: see server.scheme in $CONFIG_FILE (host port $HOST_PORT)"
elif [[ "$TRANSPORT_ACTION" == "http" ]]; then
  echo "GUI:    http://$DISPLAY_ADDRESS:$HOST_PORT/"
else
  echo "GUI:    https://$DISPLAY_ADDRESS:$HOST_PORT/"
fi
