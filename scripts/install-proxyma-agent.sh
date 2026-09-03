#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_URL="${PROXYMA_RELEASE_BASE_URL:-https://github.com/ascemama/proxyma}"
VERSION="latest"
PROJECT_DIR="$PWD"
BIN_DIR="${HOME:?HOME is required}/.local/bin"
TARGET_OS="${PROXYMA_AGENT_OS:-}"
TARGET_ARCH="${PROXYMA_AGENT_ARCH:-}"
ARCHIVE_NAME="proxyma-agent-harness.tar.gz"
LOCAL_BINARY=""
LOCAL_HARNESS=""

usage() {
  cat <<'EOF'
Usage: bash ./install-proxyma-agent.sh [options]

Downloads and verifies the native proxyma-agent executable and matching harness
from one GitHub release, or installs local build artifacts, then installs both
for the current user and project.

Options:
  --version VERSION    Install latest or an exact vMAJOR.MINOR.PATCH[-PRERELEASE].
  --project-dir DIR    Install/refresh the harness in DIR (default: current directory).
  --bin-dir DIR        Install proxyma-agent in DIR (default: $HOME/.local/bin).
  --binary PATH        Install this locally built executable instead of downloading one.
  --harness PATH       Install this locally built harness archive instead of downloading one.
  -h, --help           Show this help text.

The installer does not accept credentials and does not modify PATH.
EOF
}

validate_version() {
  local value="$1"
  local prerelease identifier

  if [[ "$value" == "latest" ]]; then
    return 0
  fi
  if [[ ! "$value" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
    echo "Invalid version: $value" >&2
    echo "Use latest or vMAJOR.MINOR.PATCH[-PRERELEASE]." >&2
    return 1
  fi
  prerelease="${BASH_REMATCH[4]}"
  if [[ -n "$prerelease" ]]; then
    IFS='.' read -r -a identifiers <<<"${prerelease#-}"
    for identifier in "${identifiers[@]}"; do
      if [[ "$identifier" =~ ^[0-9]+$ && ${#identifier} -gt 1 && "$identifier" == 0* ]]; then
        echo "Invalid numeric prerelease identifier with a leading zero: $identifier" >&2
        return 1
      fi
    done
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--version requires a value." >&2; exit 2; }
      VERSION="$2"
      shift 2
      ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --project-dir)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--project-dir requires a value." >&2; exit 2; }
      PROJECT_DIR="$2"
      shift 2
      ;;
    --project-dir=*) PROJECT_DIR="${1#*=}"; shift ;;
    --bin-dir)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--bin-dir requires a value." >&2; exit 2; }
      BIN_DIR="$2"
      shift 2
      ;;
    --bin-dir=*) BIN_DIR="${1#*=}"; shift ;;
    --binary)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--binary requires a path." >&2; exit 2; }
      LOCAL_BINARY="$2"
      shift 2
      ;;
    --binary=*) LOCAL_BINARY="${1#*=}"; shift ;;
    --harness)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--harness requires a path." >&2; exit 2; }
      LOCAL_HARNESS="$2"
      shift 2
      ;;
    --harness=*) LOCAL_HARNESS="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -n "$LOCAL_BINARY" || -n "$LOCAL_HARNESS" ]]; then
  [[ -n "$LOCAL_BINARY" && -n "$LOCAL_HARNESS" ]] || {
    echo "--binary and --harness must be supplied together." >&2
    exit 2
  }
else
  validate_version "$VERSION"
fi

for command_name in tar mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || { echo "Required command not found: $command_name" >&2; exit 1; }
done
if [[ -z "$LOCAL_BINARY" ]]; then
  command -v curl >/dev/null 2>&1 || { echo "Required command not found: curl" >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    hash_file() { sha256sum "$1" | awk '{print $1}'; }
  elif command -v shasum >/dev/null 2>&1; then
    hash_file() { shasum -a 256 "$1" | awk '{print $1}'; }
  else
    echo "Required command not found: sha256sum or shasum" >&2
    exit 1
  fi
fi

if [[ -z "$TARGET_OS" ]]; then
  case "$(uname -s)" in
    Linux) TARGET_OS="linux" ;;
    Darwin) TARGET_OS="darwin" ;;
    *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
  esac
fi
case "$TARGET_OS" in
  linux|darwin) ;;
  *) echo "Unsupported operating system: $TARGET_OS" >&2; exit 1 ;;
esac

if [[ -z "$TARGET_ARCH" ]]; then
  TARGET_ARCH="$(uname -m)"
fi
case "$TARGET_ARCH" in
  x86_64|amd64) TARGET_ARCH="amd64" ;;
  aarch64|arm64) TARGET_ARCH="arm64" ;;
  *) echo "Unsupported architecture: $TARGET_ARCH" >&2; exit 1 ;;
esac

if [[ ! -d "$PROJECT_DIR" || -L "$PROJECT_DIR" ]]; then
  echo "Project directory must be an existing directory and not a symbolic link: $PROJECT_DIR" >&2
  exit 1
fi
PROJECT_DIR="$(cd "$PROJECT_DIR" && pwd -P)"

if [[ -e "$BIN_DIR" && ( ! -d "$BIN_DIR" || -L "$BIN_DIR" ) ]]; then
  echo "Binary directory must be a directory and not a symbolic link: $BIN_DIR" >&2
  exit 1
fi
mkdir -p "$BIN_DIR"
BIN_DIR="$(cd "$BIN_DIR" && pwd -P)"

BINARY_NAME="proxyma-agent-$TARGET_OS-$TARGET_ARCH"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/proxyma-agent.XXXXXX")"
trap 'rm -rf "$TEMP_DIR"' EXIT
BINARY_PATH="$TEMP_DIR/$BINARY_NAME"
ARCHIVE_PATH="$TEMP_DIR/$ARCHIVE_NAME"
CHECKSUM_PATH="$TEMP_DIR/SHA256SUMS"

if [[ -n "$LOCAL_BINARY" ]]; then
  [[ -f "$LOCAL_BINARY" && ! -L "$LOCAL_BINARY" ]] || { echo "Local binary must be a regular file and not a symbolic link: $LOCAL_BINARY" >&2; exit 1; }
  [[ -f "$LOCAL_HARNESS" && ! -L "$LOCAL_HARNESS" ]] || { echo "Local harness archive must be a regular file and not a symbolic link: $LOCAL_HARNESS" >&2; exit 1; }
  cp "$LOCAL_BINARY" "$BINARY_PATH"
  cp "$LOCAL_HARNESS" "$ARCHIVE_PATH"
  echo "Installing local proxyma-agent artifacts for $TARGET_OS/$TARGET_ARCH..."
else
  if [[ "$VERSION" == "latest" ]]; then
    RELEASE_URL="$REPOSITORY_URL/releases/latest/download"
  else
    RELEASE_URL="$REPOSITORY_URL/releases/download/$VERSION"
  fi
  echo "Downloading proxyma-agent $VERSION for $TARGET_OS/$TARGET_ARCH..."
  for asset_name in "$BINARY_NAME" "$ARCHIVE_NAME" "SHA256SUMS"; do
    curl --fail --location --silent --show-error --retry 3 \
      --output "$TEMP_DIR/$asset_name" "$RELEASE_URL/$asset_name"
  done
fi

verify_checksum() {
  local asset_name="$1"
  local asset_path="$2"
  local checksum filename remainder
  local matched_hash=""
  local match_count=0

  while read -r checksum filename remainder; do
    filename="${filename#\*}"
    if [[ "$filename" == "$asset_name" && -z "${remainder:-}" ]]; then
      matched_hash="$checksum"
      match_count=$((match_count + 1))
    fi
  done <"$CHECKSUM_PATH"
  if [[ "$match_count" -ne 1 || ! "$matched_hash" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "SHA256SUMS does not contain exactly one valid entry for $asset_name." >&2
    exit 1
  fi
  matched_hash="$(printf '%s' "$matched_hash" | tr '[:upper:]' '[:lower:]')"
  if [[ "$(hash_file "$asset_path")" != "$matched_hash" ]]; then
    echo "SHA-256 checksum mismatch for $asset_name." >&2
    exit 1
  fi
  echo "Verified SHA-256 checksum for $asset_name."
}

if [[ -z "$LOCAL_BINARY" ]]; then
  verify_checksum "$BINARY_NAME" "$BINARY_PATH"
  verify_checksum "$ARCHIVE_NAME" "$ARCHIVE_PATH"
fi

while IFS= read -r entry; do
  case "$entry" in
    harness|harness/*) ;;
    *) echo "Harness archive contains an unsafe path: $entry" >&2; exit 1 ;;
  esac
  case "/$entry/" in
    *'/../'*|*'/./'*) echo "Harness archive contains path traversal: $entry" >&2; exit 1 ;;
  esac
done < <(tar -tzf "$ARCHIVE_PATH")

while IFS= read -r listing; do
  case "${listing:0:1}" in
    -|d) ;;
    *) echo "Harness archive contains a link or special file." >&2; exit 1 ;;
  esac
done < <(tar -tvzf "$ARCHIVE_PATH")

tar --extract --gzip --file "$ARCHIVE_PATH" --directory "$TEMP_DIR" \
  --no-same-owner --no-same-permissions 2>/dev/null || \
  tar --extract --gzip --file "$ARCHIVE_PATH" --directory "$TEMP_DIR"
PAYLOAD_DIR="$TEMP_DIR/harness"
for required in \
  "$PAYLOAD_DIR/RELEASE" \
  "$PAYLOAD_DIR/proxyma-agent.json" \
  "$PAYLOAD_DIR/instruction-pack/proxyma-harness.md" \
  "$PAYLOAD_DIR/.agents/skills/proxyma-harness/SKILL.md" \
  "$PAYLOAD_DIR/.claude/skills/proxyma-harness/SKILL.md"; do
  if [[ ! -f "$required" || -L "$required" ]]; then
    echo "Harness archive is missing a required regular file: $required" >&2
    exit 1
  fi
done

replace_path() {
  local source_path="$1"
  local destination_path="$2"
  local new_path="${destination_path}.proxyma-new.$$"
  local backup_path="${destination_path}.proxyma-backup.$$"

  if [[ -L "$destination_path" || -e "$new_path" || -e "$backup_path" ]]; then
    echo "Refusing unsafe or conflicting install destination: $destination_path" >&2
    exit 1
  fi
  cp -R "$source_path" "$new_path"
  if [[ -e "$destination_path" ]]; then
    mv "$destination_path" "$backup_path"
  fi
  if ! mv "$new_path" "$destination_path"; then
    [[ ! -e "$backup_path" ]] || mv "$backup_path" "$destination_path"
    exit 1
  fi
  [[ ! -e "$backup_path" ]] || rm -rf "$backup_path"
}

INSTALLED_BINARY="$BIN_DIR/proxyma-agent"
HARNESS_DIR="$PROJECT_DIR/harness"
SKILLS_DIR="$PROJECT_DIR/.agents/skills"
if [[ -e "$INSTALLED_BINARY" && ( ! -f "$INSTALLED_BINARY" || -L "$INSTALLED_BINARY" ) ]]; then
  echo "Installed binary target must be a regular file and not a symbolic link: $INSTALLED_BINARY" >&2
  exit 1
fi
if [[ -e "$HARNESS_DIR" && ( ! -d "$HARNESS_DIR" || -L "$HARNESS_DIR" ) ]]; then
  echo "Harness target must be a directory and not a symbolic link: $HARNESS_DIR" >&2
  exit 1
fi
for preserved_path in \
  "$HARNESS_DIR/proxyma-agent.json" \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/tasks" \
  "$HARNESS_DIR/task-state/summaries"; do
  if [[ -L "$preserved_path" ]]; then
    echo "Refusing symbolic link in preserved harness data: $preserved_path" >&2
    exit 1
  fi
done
if [[ -e "$HARNESS_DIR/proxyma-agent.json" && ! -f "$HARNESS_DIR/proxyma-agent.json" ]]; then
  echo "Harness configuration must be a regular file: $HARNESS_DIR/proxyma-agent.json" >&2
  exit 1
fi
for preserved_dir in \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/tasks" \
  "$HARNESS_DIR/task-state/summaries"; do
  if [[ -e "$preserved_dir" && ! -d "$preserved_dir" ]]; then
    echo "Preserved harness path must be a directory: $preserved_dir" >&2
    exit 1
  fi
done
for path_component in "$PROJECT_DIR/.agents" "$SKILLS_DIR"; do
  if [[ -e "$path_component" && ( ! -d "$path_component" || -L "$path_component" ) ]]; then
    echo "Skill directory must be a directory and not a symbolic link: $path_component" >&2
    exit 1
  fi
done
for owned_target in \
  "$HARNESS_DIR/instruction-pack" \
  "$HARNESS_DIR/examples" \
  "$HARNESS_DIR/tests" \
  "$HARNESS_DIR/.agents" \
  "$HARNESS_DIR/.claude" \
  "$HARNESS_DIR/RELEASE" \
  "$SKILLS_DIR/proxyma-harness"; do
  if [[ -L "$owned_target" || -e "${owned_target}.proxyma-new.$$" || -e "${owned_target}.proxyma-backup.$$" ]]; then
    echo "Refusing unsafe or conflicting install destination: $owned_target" >&2
    exit 1
  fi
done
NEW_BINARY="${INSTALLED_BINARY}.proxyma-new.$$"
BACKUP_BINARY="${INSTALLED_BINARY}.proxyma-backup.$$"
if [[ -e "$NEW_BINARY" || -e "$BACKUP_BINARY" ]]; then
  echo "Refusing conflicting binary transaction path for $INSTALLED_BINARY" >&2
  exit 1
fi
install -m 0755 "$BINARY_PATH" "$NEW_BINARY"
if [[ -e "$INSTALLED_BINARY" ]]; then mv "$INSTALLED_BINARY" "$BACKUP_BINARY"; fi
if ! mv "$NEW_BINARY" "$INSTALLED_BINARY"; then
  [[ ! -e "$BACKUP_BINARY" ]] || mv "$BACKUP_BINARY" "$INSTALLED_BINARY"
  exit 1
fi
[[ ! -e "$BACKUP_BINARY" ]] || rm -f "$BACKUP_BINARY"

mkdir -p "$HARNESS_DIR"
if [[ ! -e "$HARNESS_DIR/proxyma-agent.json" ]]; then
  install -m 0600 "$PAYLOAD_DIR/proxyma-agent.json" "$HARNESS_DIR/proxyma-agent.json"
fi
install -d -m 0700 \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/tasks" \
  "$HARNESS_DIR/task-state/summaries"

for owned in instruction-pack examples tests .agents .claude RELEASE; do
  replace_path "$PAYLOAD_DIR/$owned" "$HARNESS_DIR/$owned"
done

mkdir -p "$SKILLS_DIR"
replace_path "$PAYLOAD_DIR/.agents/skills/proxyma-harness" "$SKILLS_DIR/proxyma-harness"

echo "Installed proxyma-agent at $INSTALLED_BINARY"
echo "Installed the matching harness at $HARNESS_DIR"
echo "Installed the Codex adapter at $SKILLS_DIR/proxyma-harness"
case ":${PATH:-}:" in
  *":$BIN_DIR:"*) echo "$BIN_DIR is already on PATH." ;;
  *)
    echo "$BIN_DIR is not on PATH. Add it to your shell profile, then restart Codex."
    echo "For example: export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac
