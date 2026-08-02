#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-}"
COMMIT="${2:-}"
OUT_DIR="${3:-$ROOT/dist}"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}"

usage() {
  echo "Usage: $0 <vMAJOR.MINOR.PATCH[-PRERELEASE]> <commit> [output-directory]" >&2
}

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

for required in \
  "$ROOT/docker-compose.example.yml" \
  "$ROOT/scripts/install-proxyma-docker.sh" \
  "$ROOT/scripts/uninstall-proxyma-docker.sh" \
  "$ROOT/scripts/deployment-endpoint.sh" \
  "$ROOT/LICENSE"; do
  if [[ ! -f "$required" ]]; then
    echo "Missing Docker release input: $required" >&2
    exit 1
  fi
done

mkdir -p "$OUT_DIR"
STAGING_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGING_DIR"' EXIT
PAYLOAD_DIR="$STAGING_DIR/proxyma"
mkdir -p "$PAYLOAD_DIR/scripts"

install -m 0644 "$ROOT/docker-compose.example.yml" "$PAYLOAD_DIR/docker-compose.example.yml"
install -m 0755 "$ROOT/scripts/install-proxyma-docker.sh" "$PAYLOAD_DIR/scripts/install-proxyma-docker.sh"
install -m 0755 "$ROOT/scripts/uninstall-proxyma-docker.sh" "$PAYLOAD_DIR/scripts/uninstall-proxyma-docker.sh"
install -m 0644 "$ROOT/scripts/deployment-endpoint.sh" "$PAYLOAD_DIR/scripts/deployment-endpoint.sh"
install -m 0644 "$ROOT/LICENSE" "$PAYLOAD_DIR/LICENSE"

cat >"$PAYLOAD_DIR/RELEASE" <<EOF
name=Proxyma
version=$VERSION
commit=$COMMIT
deployment=docker
image=ghcr.io/ascemama/proxyma:${VERSION#v}
install=sudo bash ./scripts/install-proxyma-docker.sh
EOF

ARCHIVE="$OUT_DIR/proxyma_docker.tar.gz"
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
