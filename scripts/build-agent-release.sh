#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS_VALUE="${GOOS:-$(go env GOOS)}"
GOARCH_VALUE="${GOARCH:-$(go env GOARCH)}"
CGO_ENABLED_VALUE="${CGO_ENABLED:-0}"
OUT_DIR="${OUT_DIR:-$ROOT/dist}"
VERSION_VALUE="${PROXYMA_VERSION:-$(git -C "$ROOT" describe --tags --long --always --dirty)}"
COMMIT_VALUE="${PROXYMA_COMMIT:-$(git -C "$ROOT" rev-parse --short=7 HEAD)}"
extension=""
if [[ "$GOOS_VALUE" == "windows" ]]; then extension=".exe"; fi

mkdir -p "$OUT_DIR"
output="$OUT_DIR/proxyma-agent-$GOOS_VALUE-$GOARCH_VALUE$extension"
cd "$ROOT"
GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" CGO_ENABLED="$CGO_ENABLED_VALUE" \
  go build -trimpath \
  -ldflags="-s -w -X proxyma/internal/buildinfo.rawVersion=$VERSION_VALUE -X proxyma/internal/buildinfo.rawCommit=$COMMIT_VALUE" \
  -o "$output" ./cmd/proxyma-agent
echo "Built $output"
