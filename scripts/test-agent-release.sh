#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
RELEASE_BASE_URL="${2:-https://github.com/ascemama/proxyma}"

if [[ -z "$VERSION" ]]; then
  echo "Usage: $0 <version> [release-base-url]" >&2
  exit 2
fi

RELEASE_BASE_URL="${RELEASE_BASE_URL%/}"
RELEASE_URL="$RELEASE_BASE_URL/releases/download/$VERSION"
CLI_VERSION="${VERSION#v}"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

PROJECT_DIR="$TEST_ROOT/project"
BIN_DIR="$TEST_ROOT/bin"
INSTALLER="$TEST_ROOT/install-proxyma-agent.sh"
mkdir -p "$PROJECT_DIR"

download_with_retry() {
  local url="$1"
  local destination="$2"
  local attempt
  for attempt in 1 2 3 4 5; do
    if curl --fail --location --silent --show-error \
      --output "$destination" "$url"; then
      return 0
    fi
    [[ "$attempt" -eq 5 ]] || sleep 2
  done
  return 1
}

download_with_retry "$RELEASE_URL/install-proxyma-agent.sh" "$INSTALLER"
chmod 0755 "$INSTALLER"

PROXYMA_RELEASE_BASE_URL="$RELEASE_BASE_URL" \
  bash "$INSTALLER" \
    --version "$VERSION" \
    --harness-type codex \
    --project-dir "$PROJECT_DIR" \
    --bin-dir "$BIN_DIR"

INSTALLED_BINARY="$BIN_DIR/proxyma-agent"
test -x "$INSTALLED_BINARY"
test -f "$PROJECT_DIR/harness/RELEASE"
test -f "$PROJECT_DIR/harness/proxyma-agent.json"
test -f "$PROJECT_DIR/.agents/skills/proxyma-harness/SKILL.md"
grep -F '"version":"'"$CLI_VERSION"'"' <("$INSTALLED_BINARY" version) >/dev/null
grep -Fx "version=$VERSION" "$PROJECT_DIR/harness/RELEASE" >/dev/null

printf 'preserved-config\n' >"$PROJECT_DIR/harness/proxyma-agent.json"
printf 'preserved-state\n' >"$PROJECT_DIR/harness/task-state/active/task.json"

PROXYMA_RELEASE_BASE_URL="$RELEASE_BASE_URL" \
  bash "$INSTALLER" \
    --version "$VERSION" \
    --harness-type codex \
    --project-dir "$PROJECT_DIR" \
    --bin-dir "$BIN_DIR" >/dev/null

grep -Fx 'preserved-config' "$PROJECT_DIR/harness/proxyma-agent.json" >/dev/null
grep -Fx 'preserved-state' "$PROJECT_DIR/harness/task-state/active/task.json" >/dev/null

echo "Agent release smoke test passed for $VERSION on $(uname -s)/$(uname -m)."
