#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT
TEST_BASE_URL="file://$TEST_ROOT"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

create_harness_archive() {
  local output="$1"
  local marker="${2:-current}"
  local payload="$TEST_ROOT/payload"
  rm -rf "$payload"
  mkdir -p \
    "$payload/harness/instruction-pack" \
    "$payload/harness/examples" \
    "$payload/harness/tests" \
    "$payload/harness/.agents/skills/proxyma-harness" \
    "$payload/harness/.claude/skills/proxyma-harness" \
    "$payload/harness/agent-configuration" \
    "$payload/harness/task-state/tasks" \
    "$payload/harness/task-state/summaries"
  printf 'name=proxyma-agent-harness\nversion=v1.2.3\ncommit=deadbeef\n' >"$payload/harness/RELEASE"
  printf '{"schema_version":"2","proxyma_base_url":"https://proxyma.example","agent_config":"agent-configuration/agent.json","state_dir":"task-state"}\n' >"$payload/harness/proxyma-agent.json"
  printf '%s\n' "$marker" >"$payload/harness/instruction-pack/proxyma-harness.md"
  printf 'example\n' >"$payload/harness/examples/example.txt"
  printf 'test\n' >"$payload/harness/tests/test.txt"
  printf '%s\n' "$marker" >"$payload/harness/.agents/skills/proxyma-harness/SKILL.md"
  printf '%s\n' "$marker" >"$payload/harness/.claude/skills/proxyma-harness/SKILL.md"
  tar -C "$payload" -czf "$output" harness
}

create_release() {
  local release_dir="$1"
  local os="$2"
  local arch="$3"
  local binary_marker="$4"
  mkdir -p "$release_dir"
  printf '%s\n' "$binary_marker" >"$release_dir/proxyma-agent-$os-$arch"
  create_harness_archive "$release_dir/proxyma-agent-harness.tar.gz" "$binary_marker"
  {
    printf '%s  %s\n' "$(hash_file "$release_dir/proxyma-agent-$os-$arch")" "proxyma-agent-$os-$arch"
    printf '%s  %s\n' "$(hash_file "$release_dir/proxyma-agent-harness.tar.gz")" "proxyma-agent-harness.tar.gz"
  } >"$release_dir/SHA256SUMS"
}

EXACT_DIR="$TEST_ROOT/releases/download/v1.2.3"
create_release "$EXACT_DIR" linux amd64 first
PROJECT_DIR="$TEST_ROOT/project"
BIN_DIR="$TEST_ROOT/bin"
mkdir -p "$PROJECT_DIR"
ORIGINAL_PATH="$PATH"
OUTPUT="$(
  PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
  PROXYMA_AGENT_OS=linux \
  PROXYMA_AGENT_ARCH=x86_64 \
    bash "$ROOT/scripts/install-proxyma-agent.sh" \
      --version v1.2.3 --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR"
)"

[[ "$PATH" == "$ORIGINAL_PATH" ]] || fail "installer changed PATH"
grep -Fx 'first' "$BIN_DIR/proxyma-agent" >/dev/null || fail "wrong binary was installed"
grep -Fx 'first' "$PROJECT_DIR/harness/instruction-pack/proxyma-harness.md" >/dev/null || fail "harness was not installed"
grep -Fx 'first' "$PROJECT_DIR/.agents/skills/proxyma-harness/SKILL.md" >/dev/null || fail "Codex adapter was not installed"
grep -F 'is not on PATH' <<<"$OUTPUT" >/dev/null || fail "missing PATH guidance"
find "$PROJECT_DIR/harness/agent-configuration" -type f ! -name .gitkeep -print -quit | grep -q . && fail "installer created credentials"

printf 'preserved-config\n' >"$PROJECT_DIR/harness/proxyma-agent.json"
printf 'preserved-secret\n' >"$PROJECT_DIR/harness/agent-configuration/agent.json"
printf 'preserved-state\n' >"$PROJECT_DIR/harness/task-state/tasks/task.json"
create_release "$EXACT_DIR" linux amd64 second
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_AGENT_OS=linux \
PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" \
    --version v1.2.3 --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null
grep -Fx 'preserved-config' "$PROJECT_DIR/harness/proxyma-agent.json" >/dev/null || fail "configuration was overwritten"
grep -Fx 'preserved-secret' "$PROJECT_DIR/harness/agent-configuration/agent.json" >/dev/null || fail "credential was overwritten"
grep -Fx 'preserved-state' "$PROJECT_DIR/harness/task-state/tasks/task.json" >/dev/null || fail "task state was overwritten"
grep -Fx 'second' "$PROJECT_DIR/harness/instruction-pack/proxyma-harness.md" >/dev/null || fail "release-owned harness content was not refreshed"

LATEST_DIR="$TEST_ROOT/releases/latest/download"
create_release "$LATEST_DIR" darwin arm64 latest
mkdir -p "$TEST_ROOT/latest-project"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_AGENT_OS=darwin \
PROXYMA_AGENT_ARCH=aarch64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" \
    --project-dir "$TEST_ROOT/latest-project" --bin-dir "$TEST_ROOT/latest-bin" >/dev/null
grep -Fx latest "$TEST_ROOT/latest-bin/proxyma-agent" >/dev/null || fail "latest Darwin ARM64 asset was not selected"

BAD_DIR="$TEST_ROOT/releases/download/v1.2.4"
create_release "$BAD_DIR" linux amd64 bad
printf '%064d  proxyma-agent-linux-amd64\n' 0 >"$BAD_DIR/SHA256SUMS"
printf '%s  proxyma-agent-harness.tar.gz\n' "$(hash_file "$BAD_DIR/proxyma-agent-harness.tar.gz")" >>"$BAD_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/bad-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2.4 \
    --project-dir "$TEST_ROOT/bad-project" --bin-dir "$TEST_ROOT/bad-bin" >/dev/null 2>&1; then
  fail "checksum mismatch was accepted"
fi

DUP_DIR="$TEST_ROOT/releases/download/v1.2.5"
create_release "$DUP_DIR" linux amd64 duplicate
duplicate_line="$(grep 'proxyma-agent-linux-amd64$' "$DUP_DIR/SHA256SUMS")"
printf '%s\n' "$duplicate_line" >>"$DUP_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/duplicate-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2.5 \
    --project-dir "$TEST_ROOT/duplicate-project" --bin-dir "$TEST_ROOT/duplicate-bin" >/dev/null 2>&1; then
  fail "duplicate checksum entry was accepted"
fi

MISSING_DIR="$TEST_ROOT/releases/download/v1.2.7"
create_release "$MISSING_DIR" linux amd64 missing
grep 'proxyma-agent-linux-amd64$' "$MISSING_DIR/SHA256SUMS" >"$MISSING_DIR/SHA256SUMS.filtered"
mv "$MISSING_DIR/SHA256SUMS.filtered" "$MISSING_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/missing-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2.7 \
    --project-dir "$TEST_ROOT/missing-project" --bin-dir "$TEST_ROOT/missing-bin" >/dev/null 2>&1; then
  fail "missing checksum entry was accepted"
fi

UNSAFE_DIR="$TEST_ROOT/releases/download/v1.2.6"
create_release "$UNSAFE_DIR" linux amd64 unsafe
rm -f "$UNSAFE_DIR/proxyma-agent-harness.tar.gz"
mkdir -p "$TEST_ROOT/unsafe-payload/harness"
printf 'unsafe\n' >"$TEST_ROOT/unsafe-payload/harness/file"
ln -s file "$TEST_ROOT/unsafe-payload/harness/link"
tar -C "$TEST_ROOT/unsafe-payload" -czf "$UNSAFE_DIR/proxyma-agent-harness.tar.gz" harness
{
  printf '%s  proxyma-agent-linux-amd64\n' "$(hash_file "$UNSAFE_DIR/proxyma-agent-linux-amd64")"
  printf '%s  proxyma-agent-harness.tar.gz\n' "$(hash_file "$UNSAFE_DIR/proxyma-agent-harness.tar.gz")"
} >"$UNSAFE_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/unsafe-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2.6 \
    --project-dir "$TEST_ROOT/unsafe-project" --bin-dir "$TEST_ROOT/unsafe-bin" >/dev/null 2>&1; then
  fail "archive link was accepted"
fi

mkdir -p "$TEST_ROOT/link-project" "$TEST_ROOT/link-target"
ln -s "$TEST_ROOT/link-target" "$TEST_ROOT/link-project/harness"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2.3 \
    --project-dir "$TEST_ROOT/link-project" --bin-dir "$TEST_ROOT/link-bin" >/dev/null 2>&1; then
  fail "symbolic-link harness target was accepted"
fi
[[ ! -e "$TEST_ROOT/link-bin/proxyma-agent" ]] || fail "binary was installed before symbolic-link preflight failed"

if PROXYMA_AGENT_OS=freebsd PROXYMA_AGENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "unsupported operating system was accepted"
fi
if PROXYMA_AGENT_OS=linux PROXYMA_AGENT_ARCH=ppc64le \
  bash "$ROOT/scripts/install-proxyma-agent.sh" --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "unsupported architecture was accepted"
fi
if bash "$ROOT/scripts/install-proxyma-agent.sh" --version v1.2 \
  --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "invalid version was accepted"
fi

echo "POSIX agent installer tests passed."
