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
  "$ROOT/harness/.agents" \
  "$ROOT/harness/.claude" \
  "$ROOT/harness/examples" \
  "$ROOT/harness/instruction-pack" \
  "$ROOT/harness/tests" \
  "$ROOT/harness/examples/proxyma-agent.json"; do
  if [[ ! -e "$required" || -L "$required" ]]; then
    echo "Missing or unsafe harness package input: $required" >&2
    exit 1
  fi
done

mkdir -p "$OUT_DIR"
STAGING_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGING_DIR"' EXIT
PAYLOAD_DIR="$STAGING_DIR/harness"
mkdir -p \
  "$PAYLOAD_DIR/agent-configuration" \
  "$PAYLOAD_DIR/task-state/active" \
  "$PAYLOAD_DIR/task-state/blocked" \
  "$PAYLOAD_DIR/task-state/closed" \
  "$PAYLOAD_DIR/task-state/summaries"

# Keep this list explicit and copy tracked files only. In particular, never copy
# the repository's development credential, local configuration, untracked files,
# or task state into a release.
copy_tracked_tree() {
  local relative_root="$1"
  local relative_file source_file destination_file

  while IFS= read -r -d '' relative_file; do
    source_file="$ROOT/$relative_file"
    destination_file="$STAGING_DIR/$relative_file"
    if [[ ! -f "$source_file" || -L "$source_file" ]]; then
      echo "Unsafe tracked harness input: $source_file" >&2
      exit 1
    fi
    mkdir -p "$(dirname "$destination_file")"
    cp "$source_file" "$destination_file"
  done < <(git -C "$ROOT" ls-files -z -- "$relative_root")
}

copy_tracked_tree harness/.agents
copy_tracked_tree harness/.claude
copy_tracked_tree harness/examples
copy_tracked_tree harness/instruction-pack
copy_tracked_tree harness/tests
install -m 0644 "$ROOT/harness/examples/proxyma-agent.json" "$PAYLOAD_DIR/proxyma-agent.json"
: >"$PAYLOAD_DIR/agent-configuration/.gitkeep"
: >"$PAYLOAD_DIR/task-state/active/.gitkeep"
: >"$PAYLOAD_DIR/task-state/blocked/.gitkeep"
: >"$PAYLOAD_DIR/task-state/closed/.gitkeep"
: >"$PAYLOAD_DIR/task-state/summaries/.gitkeep"

cat >"$PAYLOAD_DIR/RELEASE" <<EOF
name=proxyma-agent-harness
version=$VERSION
commit=$COMMIT
EOF

ARCHIVE="$OUT_DIR/proxyma-agent-harness.tar.gz"
tar \
  --sort=name \
  --mtime="@$SOURCE_DATE_EPOCH" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  -C "$STAGING_DIR" \
  -czf "$ARCHIVE" \
  harness

echo "Packaged $ARCHIVE"
