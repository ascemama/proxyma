#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-client-harness.sh" \
  v1.2.3 deadbeef "$TEST_ROOT/first"
SOURCE_DATE_EPOCH=1700000000 bash "$ROOT/scripts/package-client-harness.sh" \
  v1.2.3 deadbeef "$TEST_ROOT/second"

FIRST="$TEST_ROOT/first/proxyma-client-harness.tar.gz"
SECOND="$TEST_ROOT/second/proxyma-client-harness.tar.gz"
cmp "$FIRST" "$SECOND" >/dev/null || fail "harness package is not reproducible"

LISTING="$TEST_ROOT/listing"
tar -tzf "$FIRST" >"$LISTING"
for required in \
  harness/RELEASE \
  harness/proxyma-client.json \
  harness/agent-configuration/agent.example.json \
  harness/agent-configuration/.gitkeep \
  harness/task-state/active/.gitkeep \
  harness/task-state/blocked/.gitkeep \
  harness/task-state/closed/.gitkeep \
  harness/task-state/summaries/.gitkeep \
  harness/instruction-pack/proxyma-harness.md \
  harness/.agents/skills/proxyma-harness/SKILL.md \
  harness/.claude/skills/proxyma-harness/SKILL.md \
  harness/generic/skills/proxyma-harness/SKILL.md; do
  grep -Fx "$required" "$LISTING" >/dev/null || fail "package is missing $required"
done
for required in \
  harness/workflows/vulnerability-management/README.md \
  harness/workflows/vulnerability-management/prompt.md \
  harness/workflows/vulnerability-management/cron.example \
  harness/workflows/vulnerability-management/run-hermes-worker.sh \
  harness/workflows/vulnerability-management/configure-hermes-worker-prompt.sh \
  harness/workflows/vulnerability-management/configure-hermes-worker-prompt.ps1 \
  harness/workflows/vulnerability-management/schema.sql \
  harness/workflows/vulnerability-management/profile.schema.json \
  harness/workflows/vulnerability-management/profiles/nessus-detailed.json \
  harness/workflows/vulnerability-management/tests/fixtures/nessus-detailed-sample.csv; do
  grep -Fx "$required" "$LISTING" >/dev/null || fail "package is missing $required"
done

if grep -F 'df9ec8bb-1a22-4181-a7c8-126ad553221d.json' "$LISTING" >/dev/null; then
  fail "package contains the development credential"
fi
if grep -Fx 'harness/agent-configuration/agent.json' "$LISTING" >/dev/null; then
  fail "package contains a real credential file"
fi
if grep -E '^harness/task-state/(active|blocked|closed)/[^.][^/]*\.json$' "$LISTING" >/dev/null; then
  fail "package contains runtime task state"
fi
if grep -Fx 'harness/instruction-pack/proxyma-api-reference.md' "$LISTING" >/dev/null; then
  fail "package contains the retired raw API reference"
fi
if tar -tvzf "$FIRST" | grep -Ev '^[d-]' >/dev/null; then
  fail "package contains a link or special file"
fi

mkdir -p "$TEST_ROOT/extracted"
tar -xzf "$FIRST" -C "$TEST_ROOT/extracted"
grep -Fx 'version=v1.2.3' "$TEST_ROOT/extracted/harness/RELEASE" >/dev/null || fail "release version metadata is missing"
grep -Fx 'commit=deadbeef' "$TEST_ROOT/extracted/harness/RELEASE" >/dev/null || fail "release commit metadata is missing"
grep -F 'https://proxyma.example' "$TEST_ROOT/extracted/harness/proxyma-client.json" >/dev/null || fail "configuration is not production-neutral"
grep -F '"agent_id": "TOBECHANGED"' "$TEST_ROOT/extracted/harness/agent-configuration/agent.example.json" >/dev/null || fail "credential example is missing the agent id placeholder"
grep -F '"agent_jwt_signing_key": "TOBECHANGED"' "$TEST_ROOT/extracted/harness/agent-configuration/agent.example.json" >/dev/null || fail "credential example is missing the signing key placeholder"
if grep -R -F 'dev-only-codex-harness-signing-key' "$TEST_ROOT/extracted/harness" >/dev/null; then
  fail "package exposes the development signing key"
fi

if bash "$ROOT/scripts/package-client-harness.sh" v1.2.3-01 deadbeef "$TEST_ROOT/bad" >/dev/null 2>&1; then
  fail "package script accepted an invalid prerelease"
fi

if bash "$ROOT/scripts/package-client-harness.sh" v1.2.3 >/dev/null 2>&1; then
  fail "package script accepted one positional argument"
fi
if bash "$ROOT/scripts/package-client-harness.sh" v1.2.3 "" "$TEST_ROOT/missing-commit" >/dev/null 2>&1; then
  fail "package script accepted a missing release commit"
fi

EXPECTED_SOURCE_VERSION="$(git -C "$ROOT" describe --tags --long --always --dirty)"
EXPECTED_SOURCE_COMMIT="$(git -C "$ROOT" rev-parse --short=7 HEAD)"
env -u PROXYMA_VERSION -u PROXYMA_COMMIT \
  SOURCE_DATE_EPOCH=1700000000 OUT_DIR="$TEST_ROOT/source-default" \
  bash "$ROOT/scripts/package-client-harness.sh"
SOURCE_DEFAULT="$TEST_ROOT/source-default/proxyma-client-harness.tar.gz"
mkdir -p "$TEST_ROOT/source-default-extracted"
tar -xzf "$SOURCE_DEFAULT" -C "$TEST_ROOT/source-default-extracted"
grep -Fx "version=$EXPECTED_SOURCE_VERSION" "$TEST_ROOT/source-default-extracted/harness/RELEASE" >/dev/null \
  || fail "source package did not derive the Git version"
grep -Fx "commit=$EXPECTED_SOURCE_COMMIT" "$TEST_ROOT/source-default-extracted/harness/RELEASE" >/dev/null \
  || fail "source package did not derive the Git commit"

SOURCE_VERSION_OVERRIDE='dev+gdeadbee.dirty'
SOURCE_COMMIT_OVERRIDE='deadbeef'
SOURCE_DATE_EPOCH=1700000000 \
  PROXYMA_VERSION="$SOURCE_VERSION_OVERRIDE" \
  PROXYMA_COMMIT="$SOURCE_COMMIT_OVERRIDE" \
  OUT_DIR="$TEST_ROOT/source-override" \
  bash "$ROOT/scripts/package-client-harness.sh"
SOURCE_OVERRIDE="$TEST_ROOT/source-override/proxyma-client-harness.tar.gz"
mkdir -p "$TEST_ROOT/source-override-extracted"
tar -xzf "$SOURCE_OVERRIDE" -C "$TEST_ROOT/source-override-extracted"
grep -Fx "version=$SOURCE_VERSION_OVERRIDE" "$TEST_ROOT/source-override-extracted/harness/RELEASE" >/dev/null \
  || fail "source package did not honor the version override"
grep -Fx "commit=$SOURCE_COMMIT_OVERRIDE" "$TEST_ROOT/source-override-extracted/harness/RELEASE" >/dev/null \
  || fail "source package did not honor the commit override"

SOURCE_ROOT="$TEST_ROOT/source-checkout"
mkdir -p "$SOURCE_ROOT"
git archive HEAD | tar -x -C "$SOURCE_ROOT"
cp "$ROOT/scripts/package-client-harness.sh" "$SOURCE_ROOT/scripts/package-client-harness.sh"
mkdir -p "$SOURCE_ROOT/harness/agent-configuration"
cp "$ROOT/harness/agent-configuration/agent.example.json" "$SOURCE_ROOT/harness/agent-configuration/agent.example.json"
git -C "$SOURCE_ROOT" init -q
git -C "$SOURCE_ROOT" add -A -- .
git -C "$SOURCE_ROOT" config user.email test@example.invalid
git -C "$SOURCE_ROOT" config user.name test
git -C "$SOURCE_ROOT" commit -q -m fixture

MODIFIED_FILE="$SOURCE_ROOT/harness/instruction-pack/TASK-STATE.md"
DELETED_FILE="$SOURCE_ROOT/harness/instruction-pack/TASK-SUMMARY-TEMPLATE.md"
UNTRACKED_FILE="$SOURCE_ROOT/harness/instruction-pack/source-only.md"
printf '\nsource working-tree modification\n' >>"$MODIFIED_FILE"
rm -f "$DELETED_FILE"
printf 'source-only file\n' >"$UNTRACKED_FILE"
mkdir -p \
  "$SOURCE_ROOT/harness/agent-configuration" \
  "$SOURCE_ROOT/harness/task-state/active"
printf 'fixture-secret\n' >"$SOURCE_ROOT/harness/agent-configuration/fixture.json"
printf 'fixture-state\n' >"$SOURCE_ROOT/harness/task-state/active/fixture.json"

SOURCE_DATE_EPOCH=1700000000 \
  PROXYMA_VERSION='source-fixture' \
  PROXYMA_COMMIT='fixturecommit' \
  OUT_DIR="$TEST_ROOT/source-working-tree-first" \
  bash "$SOURCE_ROOT/scripts/package-client-harness.sh"
SOURCE_DATE_EPOCH=1700000000 \
  PROXYMA_VERSION='source-fixture' \
  PROXYMA_COMMIT='fixturecommit' \
  OUT_DIR="$TEST_ROOT/source-working-tree-second" \
  bash "$SOURCE_ROOT/scripts/package-client-harness.sh"
cmp \
  "$TEST_ROOT/source-working-tree-first/proxyma-client-harness.tar.gz" \
  "$TEST_ROOT/source-working-tree-second/proxyma-client-harness.tar.gz" >/dev/null \
  || fail "source harness package is not reproducible"

SOURCE_WORKING_LISTING="$TEST_ROOT/source-working-tree-listing"
tar -tzf "$TEST_ROOT/source-working-tree-first/proxyma-client-harness.tar.gz" >"$SOURCE_WORKING_LISTING"
grep -Fx 'harness/instruction-pack/source-only.md' "$SOURCE_WORKING_LISTING" >/dev/null \
  || fail "source package omitted an untracked file"
if grep -Fx 'harness/instruction-pack/TASK-SUMMARY-TEMPLATE.md' "$SOURCE_WORKING_LISTING" >/dev/null; then
  fail "source package included a deleted file"
fi
if grep -E '^harness/(agent-configuration/fixture\.json|task-state/active/fixture\.json)$' "$SOURCE_WORKING_LISTING" >/dev/null; then
  fail "source package included preserved runtime data"
fi
SOURCE_WORKING_EXTRACTED="$TEST_ROOT/source-working-tree-extracted"
mkdir -p "$SOURCE_WORKING_EXTRACTED"
tar -xzf "$TEST_ROOT/source-working-tree-first/proxyma-client-harness.tar.gz" -C "$SOURCE_WORKING_EXTRACTED"
grep -F 'source working-tree modification' "$SOURCE_WORKING_EXTRACTED/harness/instruction-pack/TASK-STATE.md" >/dev/null \
  || fail "source package omitted a modified tracked file"

ln -s TASK-STATE.md "$SOURCE_ROOT/harness/instruction-pack/unsafe-link"
if OUT_DIR="$TEST_ROOT/source-symlink" bash "$SOURCE_ROOT/scripts/package-client-harness.sh" >/dev/null 2>&1; then
  fail "source package accepted a symlink"
fi
rm -f "$SOURCE_ROOT/harness/instruction-pack/unsafe-link"

mkfifo "$SOURCE_ROOT/harness/instruction-pack/unsafe-fifo"
if OUT_DIR="$TEST_ROOT/source-special-file" bash "$SOURCE_ROOT/scripts/package-client-harness.sh" >/dev/null 2>&1; then
  fail "source package accepted a special file"
fi

echo "Client harness package tests passed."
