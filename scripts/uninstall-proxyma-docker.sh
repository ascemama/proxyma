#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPECTED_ROOT="${PROXYMA_DOCKER_INSTALL_ROOT:-/opt/proxyma}"
if [[ "$ROOT" != "$EXPECTED_ROOT" ]]; then
  echo "Refusing to uninstall outside the managed Docker installation root: $EXPECTED_ROOT" >&2
  exit 1
fi

COMPOSE_FILE="$ROOT/docker-compose.example.yml"
COMPOSE_ENV_FILE="$ROOT/proxyma-compose.env"
CONFIG_DIR="$ROOT/proxyma-config"
DATA_DIR="$ROOT/proxyma-data"
IMAGE_REFERENCE="${PROXYMA_IMAGE:-}"
REMOVE_IMAGE=false
CONFIRMED=false
CAPTURED_IMAGE_ID=""

usage() {
  cat <<EOF
Usage: sudo bash /opt/proxyma/scripts/uninstall-proxyma-docker.sh --yes [options]

Options:
  --yes                Confirm destructive removal of the Docker deployment.
  --remove-image       Also remove the image used by the Proxyma container.
  --image IMAGE        Image to remove when no Compose container is available.
  -h, --help           Show this help text.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes)
      CONFIRMED=true
      ;;
    --remove-image)
      REMOVE_IMAGE=true
      ;;
    --image)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--image requires a non-empty image reference." >&2
        usage >&2
        exit 1
      fi
      IMAGE_REFERENCE="$2"
      shift
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

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This uninstaller supports Linux hosts only." >&2
  exit 1
fi

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root, for example: sudo bash /opt/proxyma/scripts/uninstall-proxyma-docker.sh --yes" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is not installed or is not available in PATH." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "Docker Compose v2 is not available." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "Cannot connect to the Docker daemon." >&2
  echo "Confirm that rootful Docker is running, then retry." >&2
  exit 1
fi

docker_security_options="$(docker info --format '{{json .SecurityOptions}}' 2>/dev/null)"
if grep -qi rootless <<<"$docker_security_options"; then
  echo "Rootless Docker is not supported by this uninstaller." >&2
  exit 1
fi

if grep -qi 'name=userns' <<<"$docker_security_options"; then
  echo "Docker userns-remap is not supported by this uninstaller." >&2
  exit 1
fi

if [[ ! -f "$COMPOSE_FILE" || -L "$COMPOSE_FILE" ]]; then
  echo "Missing or unsafe Docker Compose file: $COMPOSE_FILE" >&2
  exit 1
fi

COMPOSE_ENV_ARGS=()
if [[ -L "$COMPOSE_ENV_FILE" ]]; then
  echo "Refusing to use a symbolic link as the Compose environment file: $COMPOSE_ENV_FILE" >&2
  exit 1
elif [[ -e "$COMPOSE_ENV_FILE" && ! -f "$COMPOSE_ENV_FILE" ]]; then
  echo "Expected a regular Compose environment file but found: $COMPOSE_ENV_FILE" >&2
  exit 1
elif [[ -f "$COMPOSE_ENV_FILE" ]]; then
  COMPOSE_ENV_ARGS=(--env-file "$COMPOSE_ENV_FILE")
fi

require_safe_deployment_dir() {
  local label="$1"
  local path="$2"
  local parent

  parent="$(dirname "$path")"
  if [[ "$parent" != "$ROOT" || "$path" == "$ROOT" || "$path" == "/" ]]; then
    echo "$label is outside the expected Docker installation root: $path" >&2
    exit 1
  fi
  if [[ -L "$path" ]]; then
    echo "Refusing to remove a symbolic-link deployment directory: $path" >&2
    exit 1
  fi
  if [[ -e "$path" && ! -d "$path" ]]; then
    echo "Expected a deployment directory but found another file type: $path" >&2
    exit 1
  fi
}

require_safe_deployment_dir "configuration directory" "$CONFIG_DIR"
require_safe_deployment_dir "data directory" "$DATA_DIR"

echo "The following Docker deployment artifacts will be removed:"
echo "  Compose containers, networks, orphan containers, and declared volumes"
echo "  $CONFIG_DIR"
echo "  $DATA_DIR"
if [[ -f "$COMPOSE_ENV_FILE" ]]; then
  echo "  $COMPOSE_ENV_FILE"
fi
if [[ "$REMOVE_IMAGE" == true ]]; then
  echo "  The Proxyma container image (when it can be identified)"
else
  echo "  Container images will be retained"
fi

if [[ "$CONFIRMED" != true && "${PROXYMA_UNINSTALL_YES:-}" != "1" ]]; then
  echo "Re-run with --yes or set PROXYMA_UNINSTALL_YES=1 to continue."
  exit 2
fi

compose() {
  docker compose "${COMPOSE_ENV_ARGS[@]}" --project-directory "$ROOT" -f "$COMPOSE_FILE" "$@"
}

if [[ "$REMOVE_IMAGE" == true ]]; then
  container_id="$(compose ps --all --quiet Proxyma 2>/dev/null | head -n 1 || true)"
  if [[ -n "$container_id" ]]; then
    CAPTURED_IMAGE_ID="$(docker inspect --format '{{.Image}}' "$container_id")"
  elif [[ -n "$IMAGE_REFERENCE" ]]; then
    CAPTURED_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$IMAGE_REFERENCE" 2>/dev/null || true)"
  else
    echo "Cannot identify the image: no Proxyma container exists and --image was not supplied." >&2
    echo "Re-run with --image IMAGE, or omit --remove-image." >&2
    exit 1
  fi
fi

compose down --volumes --remove-orphans
rm -rf -- "$CONFIG_DIR" "$DATA_DIR"
rm -f -- "$COMPOSE_ENV_FILE"

if [[ "$REMOVE_IMAGE" == true ]]; then
  if [[ -n "$CAPTURED_IMAGE_ID" ]]; then
    docker image rm "$CAPTURED_IMAGE_ID"
  else
    echo "The requested image was not present; no image was removed."
  fi
fi

rm -f -- \
  "$ROOT/docker-compose.example.yml" \
  "$ROOT/RELEASE" \
  "$ROOT/scripts/install-proxyma-docker.sh" \
  "$ROOT/scripts/deployment-endpoint.sh" \
  "$ROOT/scripts/uninstall-proxyma-docker.sh"
rmdir "$ROOT/scripts" "$ROOT" 2>/dev/null || true
echo "Uninstalled the Proxyma Docker deployment."
echo "Docker Engine, Docker Compose, and container images not selected above were retained."
