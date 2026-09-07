#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-agent-harness.sh" \
  v1.2.3 deadbeef "$TEST_ROOT/first"
SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-agent-harness.sh" \
  v1.2.3 deadbeef "$TEST_ROOT/second"

FIRST="$TEST_ROOT/first/proxyma-agent-harness.tar.gz"
SECOND="$TEST_ROOT/second/proxyma-agent-harness.tar.gz"
cmp "$FIRST" "$SECOND" >/dev/null || fail "harness package is not reproducible"

LISTING="$TEST_ROOT/listing"
tar -tzf "$FIRST" >"$LISTING"
for required in \
  harness/RELEASE \
  harness/proxyma-agent.json \
  harness/agent-configuration/.gitkeep \
  harness/task-state/active/.gitkeep \
  harness/task-state/blocked/.gitkeep \
  harness/task-state/closed/.gitkeep \
  harness/task-state/summaries/.gitkeep \
  harness/instruction-pack/proxyma-harness.md \
  harness/.agents/skills/proxyma-harness/SKILL.md \
  harness/.claude/skills/proxyma-harness/SKILL.md; do
  grep -Fx "$required" "$LISTING" >/dev/null || fail "package is missing $required"
done

if grep -F 'df9ec8bb-1a22-4181-a7c8-126ad553221d.json' "$LISTING" >/dev/null; then
  fail "package contains the development credential"
fi
if grep -E '^harness/task-state/(active|blocked|closed)/[^.][^/]*\.json$' "$LISTING" >/dev/null; then
  fail "package contains runtime task state"
fi
if tar -tvzf "$FIRST" | grep -Ev '^[d-]' >/dev/null; then
  fail "package contains a link or special file"
fi

mkdir -p "$TEST_ROOT/extracted"
tar -xzf "$FIRST" -C "$TEST_ROOT/extracted"
grep -Fx 'version=v1.2.3' "$TEST_ROOT/extracted/harness/RELEASE" >/dev/null || fail "release version metadata is missing"
grep -Fx 'commit=deadbeef' "$TEST_ROOT/extracted/harness/RELEASE" >/dev/null || fail "release commit metadata is missing"
grep -F 'https://proxyma.example' "$TEST_ROOT/extracted/harness/proxyma-agent.json" >/dev/null || fail "configuration is not production-neutral"
if grep -R -F 'dev-only-codex-harness-signing-key' "$TEST_ROOT/extracted/harness" >/dev/null; then
  fail "package exposes the development signing key"
fi

if bash "$ROOT/scripts/package-agent-harness.sh" v1.2.3-01 deadbeef "$TEST_ROOT/bad" >/dev/null 2>&1; then
  fail "package script accepted an invalid prerelease"
fi

echo "Agent harness package tests passed."
