#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS_VALUE="${GOOS:-linux}"
GOARCH_VALUE="${GOARCH:-amd64}"
CGO_ENABLED_VALUE="${CGO_ENABLED:-0}"
OUT_DIR="${OUT_DIR:-$ROOT/dist}"
EMBED_DIR="$ROOT/internal/web/dist"
VERSION_VALUE="${PROXYMA_VERSION:-}"
COMMIT_VALUE="${PROXYMA_COMMIT:-}"
SKIP_FRONTEND_BUILD="${PROXYMA_SKIP_FRONTEND_BUILD:-0}"

if [[ -z "$VERSION_VALUE" ]]; then
  VERSION_VALUE="$(git -C "$ROOT" describe --tags --long --always --dirty)"
fi
if [[ -z "$COMMIT_VALUE" ]]; then
  COMMIT_VALUE="$(git -C "$ROOT" rev-parse --short=7 HEAD)"
fi
LDFLAGS="-s -w -X proxyma/internal/buildinfo.rawVersion=$VERSION_VALUE -X proxyma/internal/buildinfo.rawCommit=$COMMIT_VALUE"

server_ext=""
if [[ "$GOOS_VALUE" == "windows" ]]; then
  server_ext=".exe"
fi

if [[ "$SKIP_FRONTEND_BUILD" == "1" ]]; then
  if [[ ! -d "$EMBED_DIR" ]] || [[ -z "$(find "$EMBED_DIR" -mindepth 1 -print -quit)" ]]; then
    echo "PROXYMA_SKIP_FRONTEND_BUILD=1 requires prebuilt assets in $EMBED_DIR" >&2
    exit 1
  fi
else
  rm -rf "$EMBED_DIR"
  mkdir -p "$EMBED_DIR"

  cd "$ROOT/frontend"
  npm ci
  npm run build

  cp -R "$ROOT/frontend/dist/." "$EMBED_DIR/"
fi
mkdir -p "$OUT_DIR"

cd "$ROOT"
GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" CGO_ENABLED="$CGO_ENABLED_VALUE" \
  go build -tags embedded_frontend -trimpath -ldflags="$LDFLAGS" \
  -o "$OUT_DIR/Proxyma-$GOOS_VALUE-$GOARCH_VALUE$server_ext" ./cmd/proxyma

GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" CGO_ENABLED="$CGO_ENABLED_VALUE" \
  go build -trimpath -ldflags="-s -w" \
  -o "$OUT_DIR/proxymactl-$GOOS_VALUE-$GOARCH_VALUE$server_ext" ./cmd/proxymactl

echo "Built:"
echo "  $OUT_DIR/Proxyma-$GOOS_VALUE-$GOARCH_VALUE$server_ext"
echo "  $OUT_DIR/proxymactl-$GOOS_VALUE-$GOARCH_VALUE$server_ext"
