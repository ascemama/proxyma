#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARCH="${1:-}"
VERSION="${2:-}"
COMMIT="${3:-}"
OUT_DIR="${4:-$ROOT/dist}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}"

usage() {
  echo "Usage: $0 <amd64|arm64> <vMAJOR.MINOR.PATCH[-PRERELEASE]> <commit> [output-directory]" >&2
}

if [[ "$ARCH" != "amd64" && "$ARCH" != "arm64" ]]; then
  usage
  exit 2
fi
if [[ ! "$VERSION" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
  echo "Invalid release version: $VERSION" >&2
  usage
  exit 2
fi
prerelease="${BASH_REMATCH[4]}"
if [[ -n "$prerelease" ]]; then
  IFS='.' read -r -a identifiers <<<"${prerelease#-}"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^[0-9]+$ && ${#identifier} -gt 1 && "$identifier" == 0* ]]; then
      echo "Invalid numeric prerelease identifier with a leading zero: $identifier" >&2
      exit 2
    fi
  done
fi
if [[ -z "$COMMIT" ]]; then
  usage
  exit 2
fi

SERVER_BIN="$ROOT/dist/Proxyma-linux-$ARCH"
CTL_BIN="$ROOT/dist/proxymactl-linux-$ARCH"
for required in \
  "$SERVER_BIN" \
  "$CTL_BIN" \
  "$ROOT/scripts/install-proxyma-server-linux-release.sh" \
  "$ROOT/scripts/uninstall-proxyma-server-linux-release.sh" \
  "$ROOT/scripts/deployment-endpoint.sh" \
  "$ROOT/LICENSE"; do
  if [[ ! -f "$required" ]]; then
    echo "Missing release input: $required" >&2
    exit 1
  fi
done

mkdir -p "$OUT_DIR"
STAGING_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGING_DIR"' EXIT
PAYLOAD_DIR="$STAGING_DIR/proxyma"
mkdir -p "$PAYLOAD_DIR/dist" "$PAYLOAD_DIR/scripts"

install -m 0755 "$SERVER_BIN" "$PAYLOAD_DIR/dist/Proxyma-linux-$ARCH"
install -m 0755 "$CTL_BIN" "$PAYLOAD_DIR/dist/proxymactl-linux-$ARCH"
install -m 0755 "$ROOT/scripts/install-proxyma-server-linux-release.sh" "$PAYLOAD_DIR/scripts/install-proxyma-server-linux-release.sh"
install -m 0755 "$ROOT/scripts/uninstall-proxyma-server-linux-release.sh" "$PAYLOAD_DIR/scripts/uninstall-proxyma-server-linux-release.sh"
install -m 0644 "$ROOT/scripts/deployment-endpoint.sh" "$PAYLOAD_DIR/scripts/deployment-endpoint.sh"
install -m 0644 "$ROOT/LICENSE" "$PAYLOAD_DIR/LICENSE"

cat >"$PAYLOAD_DIR/RELEASE" <<EOF
name=Proxyma
version=$VERSION
commit=$COMMIT
os=linux
arch=$ARCH
install=sudo GOARCH=$ARCH bash ./scripts/install-proxyma-server-linux-release.sh
EOF

ARCHIVE="$OUT_DIR/proxyma-server-linux-$ARCH.tar.gz"
tar \
  --sort=name \
  --mtime="@$SOURCE_DATE_EPOCH" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  -C "$STAGING_DIR" \
  -czf "$ARCHIVE" \
  proxyma

echo "Packaged $ARCHIVE"
