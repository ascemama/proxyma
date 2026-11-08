#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_URL="${PROXYMA_RELEASE_BASE_URL:-https://github.com/ascemama/proxyma}"
VERSION="latest"
MACHINE="${PROXYMA_MACHINE:-$(uname -m)}"
INSTALLER_ARGS=()

usage() {
  cat <<'EOF'
Usage: sudo bash ./install-proxyma-server-linux.sh [--version VERSION] [installer options]

Downloads and verifies a precompiled Proxyma Linux release, then runs its
system installer. VERSION defaults to the latest stable GitHub release.

Downloader options:
  --version VERSION      Install an exact vMAJOR.MINOR.PATCH[-PRERELEASE].
  -h, --help             Show this help text.

All other options are passed to install-proxyma-server-linux-release.sh. Run with --help after
-- to display the system installer's options:
  sudo bash ./install-proxyma-server-linux.sh -- --help
EOF
}

validate_version() {
  local value="$1"
  local prerelease identifier

  if [[ "$value" == "latest" ]]; then
    return 0
  fi
  if [[ ! "$value" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
    echo "Invalid version: $value" >&2
    echo "Use latest or vMAJOR.MINOR.PATCH[-PRERELEASE]." >&2
    return 1
  fi
  prerelease="${BASH_REMATCH[4]}"
  if [[ -n "$prerelease" ]]; then
    IFS='.' read -r -a identifiers <<<"${prerelease#-}"
    for identifier in "${identifiers[@]}"; do
      if [[ "$identifier" =~ ^[0-9]+$ && ${#identifier} -gt 1 && "$identifier" == 0* ]]; then
        echo "Invalid numeric prerelease identifier with a leading zero: $identifier" >&2
        return 1
      fi
    done
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      if [[ $# -lt 2 || -z "$2" ]]; then
        echo "--version requires a value." >&2
        exit 2
      fi
      VERSION="$2"
      shift 2
      ;;
    --version=*)
      VERSION="${1#*=}"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      INSTALLER_ARGS+=("$@")
      break
      ;;
    *)
      INSTALLER_ARGS+=("$1")
      shift
      ;;
  esac
done

validate_version "$VERSION"

case "$MACHINE" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported Linux architecture: $MACHINE" >&2
    echo "Proxyma precompiled releases support x86_64/amd64 and aarch64/arm64." >&2
    exit 1
    ;;
esac

if [[ "$(uname -s)" != "Linux" && -z "${PROXYMA_ALLOW_NON_LINUX_TEST:-}" ]]; then
  echo "The precompiled binary installer supports Linux only." >&2
  exit 1
fi

for command_name in curl tar sha256sum mktemp; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command not found: $command_name" >&2
    exit 1
  fi
done

if [[ "$VERSION" == "latest" ]]; then
  RELEASE_URL="$REPOSITORY_URL/releases/latest/download"
else
  RELEASE_URL="$REPOSITORY_URL/releases/download/$VERSION"
fi

ARCHIVE_NAME="proxyma-server-linux-$ARCH.tar.gz"
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
ARCHIVE_PATH="$TEMP_DIR/$ARCHIVE_NAME"
CHECKSUM_PATH="$TEMP_DIR/SHA256SUMS"

echo "Downloading Proxyma ${VERSION} for linux/$ARCH..."
curl --fail --location --silent --show-error --retry 3 \
  --output "$ARCHIVE_PATH" "$RELEASE_URL/$ARCHIVE_NAME"
curl --fail --location --silent --show-error --retry 3 \
  --output "$CHECKSUM_PATH" "$RELEASE_URL/SHA256SUMS"

MATCHED_HASH=""
MATCH_COUNT=0
while read -r checksum filename remainder; do
  filename="${filename#\*}"
  if [[ "$filename" == "$ARCHIVE_NAME" && -z "${remainder:-}" ]]; then
    MATCHED_HASH="$checksum"
    MATCH_COUNT=$((MATCH_COUNT + 1))
  fi
done <"$CHECKSUM_PATH"

if [[ "$MATCH_COUNT" -ne 1 || ! "$MATCHED_HASH" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "SHA256SUMS does not contain exactly one valid entry for $ARCHIVE_NAME." >&2
  exit 1
fi
printf '%s  %s\n' "$MATCHED_HASH" "$ARCHIVE_PATH" | sha256sum --check --status
echo "Verified SHA-256 checksum for $ARCHIVE_NAME."

while IFS= read -r entry; do
  case "$entry" in
    proxyma|proxyma/*) ;;
    *)
      echo "Release archive contains an unsafe path: $entry" >&2
      exit 1
      ;;
  esac
  if [[ "$entry" == *"/../"* || "$entry" == "../"* || "$entry" == *"/.." ]]; then
    echo "Release archive contains path traversal: $entry" >&2
    exit 1
  fi
done < <(tar -tzf "$ARCHIVE_PATH")

while IFS= read -r listing; do
  case "${listing:0:1}" in
    -|d) ;;
    *)
      echo "Release archive contains a link or special file." >&2
      exit 1
      ;;
  esac
done < <(tar -tvzf "$ARCHIVE_PATH")

tar --extract --gzip --file "$ARCHIVE_PATH" --directory "$TEMP_DIR" \
  --no-same-owner --no-same-permissions

PAYLOAD_DIR="$TEMP_DIR/proxyma"
INSTALLER="$PAYLOAD_DIR/scripts/install-proxyma-server-linux-release.sh"
for required in \
  "$INSTALLER" \
  "$PAYLOAD_DIR/scripts/deployment-endpoint.sh" \
  "$PAYLOAD_DIR/scripts/uninstall-proxyma-server-linux-release.sh" \
  "$PAYLOAD_DIR/dist/Proxyma-linux-$ARCH" \
  "$PAYLOAD_DIR/dist/proxymactl-linux-$ARCH"; do
  if [[ ! -f "$required" || -L "$required" ]]; then
    echo "Release archive is missing a required regular file: $required" >&2
    exit 1
  fi
done

echo "Starting Proxyma system installation..."
GOARCH="$ARCH" bash "$INSTALLER" "${INSTALLER_ARGS[@]}"
