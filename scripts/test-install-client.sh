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
    "$payload/harness/generic/skills/proxyma-harness" \
    "$payload/harness/workflows/vulnerability-management" \
    "$payload/harness/agent-configuration" \
    "$payload/harness/task-state/active" \
    "$payload/harness/task-state/blocked" \
    "$payload/harness/task-state/closed" \
    "$payload/harness/task-state/summaries"
  printf 'name=proxyma-client-harness\nversion=v1.2.3\ncommit=deadbeef\n' >"$payload/harness/RELEASE"
  printf '{"schema_version":"2","proxyma_base_url":"https://proxyma.example","agent_config":"agent-configuration/agent.json","state_dir":"task-state"}\n' >"$payload/harness/proxyma-client.json"
  printf '{"agent_id":"TOBECHANGED","agent_jwt_signing_key":"TOBECHANGED"}\n' >"$payload/harness/agent-configuration/agent.example.json"
  printf '%s\n' "$marker" >"$payload/harness/instruction-pack/proxyma-harness.md"
  printf 'example\n' >"$payload/harness/examples/example.txt"
  printf 'test\n' >"$payload/harness/tests/test.txt"
  printf '%s\n' "$marker" >"$payload/harness/.agents/skills/proxyma-harness/SKILL.md"
  printf '%s\n' "$marker" >"$payload/harness/.claude/skills/proxyma-harness/SKILL.md"
  printf '%s\n' "$marker" >"$payload/harness/generic/skills/proxyma-harness/SKILL.md"
  printf '%s\n' "$marker" >"$payload/harness/workflows/vulnerability-management/prompt.md"
  tar -C "$payload" -czf "$output" harness
}

create_release() {
  local release_dir="$1"
  local os="$2"
  local arch="$3"
  local binary_marker="$4"
  mkdir -p "$release_dir"
  printf '%s\n' "$binary_marker" >"$release_dir/proxyma-client-$os-$arch"
  printf '#!/usr/bin/env bash\nif [[ "${1:-}" == version ]]; then printf "%s\\n" vm-workflow-test; exit 0; fi\nexit 1\n' >"$release_dir/vm-workflow-$os-$arch"
  chmod 0755 "$release_dir/vm-workflow-$os-$arch"
  create_harness_archive "$release_dir/proxyma-client-harness.tar.gz" "$binary_marker"
  {
    printf '%s  %s\n' "$(hash_file "$release_dir/proxyma-client-$os-$arch")" "proxyma-client-$os-$arch"
    printf '%s  %s\n' "$(hash_file "$release_dir/proxyma-client-harness.tar.gz")" "proxyma-client-harness.tar.gz"
    printf '%s  %s\n' "$(hash_file "$release_dir/vm-workflow-$os-$arch")" "vm-workflow-$os-$arch"
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
  PROXYMA_CLIENT_OS=linux \
  PROXYMA_CLIENT_ARCH=x86_64 \
    bash "$ROOT/scripts/install-proxyma-client.sh" \
      --version v1.2.3 --harness-type codex --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR"
)"

[[ "$PATH" == "$ORIGINAL_PATH" ]] || fail "installer changed PATH"
grep -Fx 'first' "$BIN_DIR/proxyma-client" >/dev/null || fail "wrong binary was installed"
[[ ! -e "$BIN_DIR/vm-workflow" ]] || fail "vm-workflow was installed without opt-in"
grep -Fx 'first' "$PROJECT_DIR/harness/instruction-pack/proxyma-harness.md" >/dev/null || fail "harness was not installed"
grep -Fx 'first' "$PROJECT_DIR/.agents/skills/proxyma-harness/SKILL.md" >/dev/null || fail "Codex adapter was not installed"
[[ ! -e "$PROJECT_DIR/.claude/skills/proxyma-harness" ]] || fail "Claude adapter was installed during Codex install"
grep -F 'is not on PATH' <<<"$OUTPUT" >/dev/null || fail "missing PATH guidance"
find "$PROJECT_DIR/harness/agent-configuration" -type f ! -name .gitkeep ! -name agent.example.json -print -quit | grep -q . && fail "installer created credentials"
test -f "$PROJECT_DIR/harness/agent-configuration/agent.example.json" || fail "installer omitted credential example"
[[ ! -e "$PROJECT_DIR/harness/workflows" ]] || fail "workflow assets installed without opt-in flag"
grep -F 'TOBECHANGED' "$PROJECT_DIR/harness/agent-configuration/agent.example.json" >/dev/null || fail "credential example is incorrect"

printf 'preserved-config\n' >"$PROJECT_DIR/harness/proxyma-client.json"
printf 'preserved-secret\n' >"$PROJECT_DIR/harness/agent-configuration/agent.json"
printf 'custom-example\n' >"$PROJECT_DIR/harness/agent-configuration/agent.example.json"
printf 'preserved-state\n' >"$PROJECT_DIR/harness/task-state/active/task.json"
create_release "$EXACT_DIR" linux amd64 second
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_CLIENT_OS=linux \
PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
    --version v1.2.3 --harness-type codex --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null
grep -Fx 'preserved-config' "$PROJECT_DIR/harness/proxyma-client.json" >/dev/null || fail "configuration was overwritten"
grep -Fx 'preserved-secret' "$PROJECT_DIR/harness/agent-configuration/agent.json" >/dev/null || fail "credential was overwritten"
grep -Fx 'custom-example' "$PROJECT_DIR/harness/agent-configuration/agent.example.json" >/dev/null || fail "credential example was overwritten"
grep -Fx 'preserved-state' "$PROJECT_DIR/harness/task-state/active/task.json" >/dev/null || fail "task state was overwritten"
grep -Fx 'second' "$PROJECT_DIR/harness/instruction-pack/proxyma-harness.md" >/dev/null || fail "release-owned harness content was not refreshed"

VM_PROJECT="$TEST_ROOT/vm-project"
mkdir -p "$VM_PROJECT"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.3 --with-vm-workflow \
    --project-dir "$VM_PROJECT" --bin-dir "$TEST_ROOT/vm-bin" >/dev/null
grep -Fx 'second' "$VM_PROJECT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "opt-in workflow assets were not installed"
"$TEST_ROOT/vm-bin/vm-workflow" version >/dev/null || fail "installed vm-workflow failed version check"
printf 'local-workflow\n' >"$VM_PROJECT/harness/workflows/vulnerability-management/prompt.md"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.3 \
    --project-dir "$VM_PROJECT" --bin-dir "$TEST_ROOT/vm-bin" >/dev/null
grep -Fx 'local-workflow' "$VM_PROJECT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "unflagged reinstall modified workflow assets"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.3 --with-vm-workflow \
    --project-dir "$VM_PROJECT" --bin-dir "$TEST_ROOT/vm-bin" >/dev/null
grep -Fx 'second' "$VM_PROJECT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "flagged reinstall did not refresh workflow assets"

mkdir -p "$TEST_ROOT/vm-link-project" "$TEST_ROOT/vm-link-target"
ln -s "$TEST_ROOT/vm-link-target" "$TEST_ROOT/vm-link-project/harness"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.3 --with-vm-workflow \
    --project-dir "$TEST_ROOT/vm-link-project" --bin-dir "$TEST_ROOT/vm-link-bin" >/dev/null 2>&1; then
  fail "workflow install accepted symbolic-link harness target"
fi

LATEST_DIR="$TEST_ROOT/releases/latest/download"
create_release "$LATEST_DIR" darwin arm64 latest
mkdir -p "$TEST_ROOT/latest-project"
PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
PROXYMA_CLIENT_OS=darwin \
PROXYMA_CLIENT_ARCH=aarch64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
    --project-dir "$TEST_ROOT/latest-project" --bin-dir "$TEST_ROOT/latest-bin" >/dev/null
grep -Fx latest "$TEST_ROOT/latest-bin/proxyma-client" >/dev/null || fail "latest Darwin ARM64 asset was not selected"

CLAUDE_PROJECT="$TEST_ROOT/claude-project"
mkdir -p "$CLAUDE_PROJECT"
CLAUDE_OUTPUT="$(
  PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
  PROXYMA_CLIENT_OS=darwin \
  PROXYMA_CLIENT_ARCH=arm64 \
    bash "$ROOT/scripts/install-proxyma-client.sh" \
      --harness-type claude --project-dir "$CLAUDE_PROJECT" --bin-dir "$TEST_ROOT/claude-bin"
)"
grep -F "Installed the Claude adapter" <<<"$CLAUDE_OUTPUT" >/dev/null || fail "Claude adapter was not reported"
grep -Fx latest "$TEST_ROOT/claude-bin/proxyma-client" >/dev/null || fail "Claude install selected the wrong binary"
grep -Fx latest "$CLAUDE_PROJECT/.claude/skills/proxyma-harness/SKILL.md" >/dev/null || fail "Claude adapter was not installed"
[[ ! -e "$CLAUDE_PROJECT/.agents/skills/proxyma-harness" ]] || fail "Codex adapter was installed during Claude install"

GENERIC_PROJECT="$TEST_ROOT/generic-project"
GENERIC_SKILL_DIR="$TEST_ROOT/hermes/skills"
mkdir -p "$GENERIC_PROJECT"
GENERIC_OUTPUT="$(
  PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
  PROXYMA_CLIENT_OS=darwin \
  PROXYMA_CLIENT_ARCH=arm64 \
    bash "$ROOT/scripts/install-proxyma-client.sh" \
      --harness-type generic --skill-dir "$GENERIC_SKILL_DIR" \
      --project-dir "$GENERIC_PROJECT" --bin-dir "$TEST_ROOT/generic-bin"
)"
grep -F "Installed the generic skill" <<<"$GENERIC_OUTPUT" >/dev/null || fail "generic skill was not reported"
grep -Fx latest "$TEST_ROOT/generic-bin/proxyma-client" >/dev/null || fail "generic install selected the wrong binary"
grep -Fx latest "$GENERIC_SKILL_DIR/proxyma-harness/SKILL.md" >/dev/null || fail "generic skill was not installed"
test -f "$GENERIC_PROJECT/harness/generic/skills/proxyma-harness/SKILL.md" || fail "generic source skill is missing"
[[ ! -e "$GENERIC_PROJECT/.agents/skills/proxyma-harness" ]] || fail "generic install created the Codex adapter"
[[ ! -e "$GENERIC_PROJECT/.claude/skills/proxyma-harness" ]] || fail "generic install created the Claude adapter"

if PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --harness-type generic \
    --project-dir "$TEST_ROOT/missing-skill-project" --bin-dir "$TEST_ROOT/missing-skill-bin" >/dev/null 2>&1; then
  fail "generic install accepted a missing skill directory"
fi

DEFAULT_PROJECT="$TEST_ROOT/default-project"
mkdir -p "$DEFAULT_PROJECT"
DEFAULT_OUTPUT="$(
  PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" \
  PROXYMA_CLIENT_OS=darwin \
  PROXYMA_CLIENT_ARCH=arm64 \
    bash "$ROOT/scripts/install-proxyma-client.sh" \
      --project-dir "$DEFAULT_PROJECT" --bin-dir "$TEST_ROOT/default-bin"
)"
grep -F "No project adapter selected" <<<"$DEFAULT_OUTPUT" >/dev/null || fail "missing adapter guidance"
[[ ! -e "$DEFAULT_PROJECT/.agents/skills/proxyma-harness" ]] || fail "Codex adapter was installed without selection"
[[ ! -e "$DEFAULT_PROJECT/.claude/skills/proxyma-harness" ]] || fail "Claude adapter was installed without selection"
test -f "$DEFAULT_PROJECT/harness/.agents/skills/proxyma-harness/SKILL.md" || fail "manual Codex adapter source is missing"
test -f "$DEFAULT_PROJECT/harness/.claude/skills/proxyma-harness/SKILL.md" || fail "manual Claude adapter source is missing"
test -f "$DEFAULT_PROJECT/harness/generic/skills/proxyma-harness/SKILL.md" || fail "manual generic skill source is missing"

INVALID_PROJECT="$TEST_ROOT/invalid-project"
if PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --harness-type invalid \
    --project-dir "$INVALID_PROJECT" --bin-dir "$TEST_ROOT/invalid-bin" >/dev/null 2>&1; then
  fail "invalid harness type was accepted"
fi
[[ ! -e "$INVALID_PROJECT" ]] || fail "invalid harness type created an install directory"

LOCAL_PROJECT="$TEST_ROOT/local-project"
LOCAL_BIN="$TEST_ROOT/local-bin"
mkdir -p "$LOCAL_PROJECT"
PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
  --binary "$EXACT_DIR/proxyma-client-linux-amd64" \
  --harness "$EXACT_DIR/proxyma-client-harness.tar.gz" \
  --harness-type claude \
  --project-dir "$LOCAL_PROJECT" \
  --bin-dir "$LOCAL_BIN" >/dev/null
grep -Fx second "$LOCAL_BIN/proxyma-client" >/dev/null || fail "local binary was not installed"
grep -Fx second "$LOCAL_PROJECT/.claude/skills/proxyma-harness/SKILL.md" >/dev/null || fail "local Claude adapter was not installed"
[[ ! -e "$LOCAL_PROJECT/.agents/skills/proxyma-harness" ]] || fail "local install created the wrong adapter"

if PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
  --binary "$EXACT_DIR/proxyma-client-linux-amd64" \
  --project-dir "$LOCAL_PROJECT" --bin-dir "$LOCAL_BIN" >/dev/null 2>&1; then
  fail "local mode accepted only one artifact"
fi

if PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
    --binary "$EXACT_DIR/proxyma-client-linux-amd64" \
    --harness "$EXACT_DIR/proxyma-client-harness.tar.gz" \
    --with-vm-workflow --project-dir "$LOCAL_PROJECT" --bin-dir "$LOCAL_BIN" >/dev/null 2>&1; then
  fail "local workflow install accepted a missing vm-workflow path"
fi
PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" \
    --binary "$EXACT_DIR/proxyma-client-linux-amd64" \
    --harness "$EXACT_DIR/proxyma-client-harness.tar.gz" \
    --vm-workflow "$EXACT_DIR/vm-workflow-linux-amd64" \
    --with-vm-workflow --project-dir "$LOCAL_PROJECT" --bin-dir "$LOCAL_BIN" >/dev/null
test -x "$LOCAL_BIN/vm-workflow" || fail "local vm-workflow was not installed"

BAD_DIR="$TEST_ROOT/releases/download/v1.2.4"
create_release "$BAD_DIR" linux amd64 bad
printf '%064d  proxyma-client-linux-amd64\n' 0 >"$BAD_DIR/SHA256SUMS"
printf '%s  proxyma-client-harness.tar.gz\n' "$(hash_file "$BAD_DIR/proxyma-client-harness.tar.gz")" >>"$BAD_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/bad-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.4 \
    --project-dir "$TEST_ROOT/bad-project" --bin-dir "$TEST_ROOT/bad-bin" >/dev/null 2>&1; then
  fail "checksum mismatch was accepted"
fi

DUP_DIR="$TEST_ROOT/releases/download/v1.2.5"
create_release "$DUP_DIR" linux amd64 duplicate
duplicate_line="$(grep 'proxyma-client-linux-amd64$' "$DUP_DIR/SHA256SUMS")"
printf '%s\n' "$duplicate_line" >>"$DUP_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/duplicate-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.5 \
    --project-dir "$TEST_ROOT/duplicate-project" --bin-dir "$TEST_ROOT/duplicate-bin" >/dev/null 2>&1; then
  fail "duplicate checksum entry was accepted"
fi

MISSING_DIR="$TEST_ROOT/releases/download/v1.2.7"
create_release "$MISSING_DIR" linux amd64 missing
grep 'proxyma-client-linux-amd64$' "$MISSING_DIR/SHA256SUMS" >"$MISSING_DIR/SHA256SUMS.filtered"
mv "$MISSING_DIR/SHA256SUMS.filtered" "$MISSING_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/missing-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.7 \
    --project-dir "$TEST_ROOT/missing-project" --bin-dir "$TEST_ROOT/missing-bin" >/dev/null 2>&1; then
  fail "missing checksum entry was accepted"
fi

UNSAFE_DIR="$TEST_ROOT/releases/download/v1.2.6"
create_release "$UNSAFE_DIR" linux amd64 unsafe
rm -f "$UNSAFE_DIR/proxyma-client-harness.tar.gz"
mkdir -p "$TEST_ROOT/unsafe-payload/harness"
printf 'unsafe\n' >"$TEST_ROOT/unsafe-payload/harness/file"
ln -s file "$TEST_ROOT/unsafe-payload/harness/link"
tar -C "$TEST_ROOT/unsafe-payload" -czf "$UNSAFE_DIR/proxyma-client-harness.tar.gz" harness
{
  printf '%s  proxyma-client-linux-amd64\n' "$(hash_file "$UNSAFE_DIR/proxyma-client-linux-amd64")"
  printf '%s  proxyma-client-harness.tar.gz\n' "$(hash_file "$UNSAFE_DIR/proxyma-client-harness.tar.gz")"
} >"$UNSAFE_DIR/SHA256SUMS"
mkdir -p "$TEST_ROOT/unsafe-project"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.6 \
    --project-dir "$TEST_ROOT/unsafe-project" --bin-dir "$TEST_ROOT/unsafe-bin" >/dev/null 2>&1; then
  fail "archive link was accepted"
fi

mkdir -p "$TEST_ROOT/link-project" "$TEST_ROOT/link-target"
ln -s "$TEST_ROOT/link-target" "$TEST_ROOT/link-project/harness"
if PROXYMA_RELEASE_BASE_URL="$TEST_BASE_URL" PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2.3 \
    --project-dir "$TEST_ROOT/link-project" --bin-dir "$TEST_ROOT/link-bin" >/dev/null 2>&1; then
  fail "symbolic-link harness target was accepted"
fi
[[ ! -e "$TEST_ROOT/link-bin/proxyma-client" ]] || fail "binary was installed before symbolic-link preflight failed"

if PROXYMA_CLIENT_OS=freebsd PROXYMA_CLIENT_ARCH=amd64 \
  bash "$ROOT/scripts/install-proxyma-client.sh" --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "unsupported operating system was accepted"
fi
if PROXYMA_CLIENT_OS=linux PROXYMA_CLIENT_ARCH=ppc64le \
  bash "$ROOT/scripts/install-proxyma-client.sh" --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "unsupported architecture was accepted"
fi
if bash "$ROOT/scripts/install-proxyma-client.sh" --version v1.2 \
  --project-dir "$PROJECT_DIR" --bin-dir "$BIN_DIR" >/dev/null 2>&1; then
  fail "invalid version was accepted"
fi

echo "POSIX client installer tests passed."
