#!/usr/bin/env bash
set -euo pipefail

REPOSITORY_URL="${PROXYMA_RELEASE_BASE_URL:-https://github.com/ascemama/proxyma}"
VERSION="latest"
PROJECT_DIR="$PWD"
BIN_DIR="${HOME:?HOME is required}/.local/bin"
TARGET_OS="${PROXYMA_CLIENT_OS:-}"
TARGET_ARCH="${PROXYMA_CLIENT_ARCH:-}"
ARCHIVE_NAME="proxyma-client-harness.tar.gz"
LOCAL_BINARY=""
LOCAL_HARNESS=""
LOCAL_VM_WORKFLOW=""
HARNESS_TYPE=""
SKILL_DIR=""
WITH_VM_WORKFLOW=false

usage() {
  cat <<'EOF'
Usage: bash ./install-proxyma-client.sh [options]

Downloads and verifies the native proxyma-client executable and matching harness
from one GitHub release, or installs local build artifacts, then installs both
for the current user and project.

Options:
  --version VERSION    Install latest or an exact vMAJOR.MINOR.PATCH[-PRERELEASE].
  --project-dir DIR    Install/refresh the harness in DIR (default: current directory).
  --bin-dir DIR        Install proxyma-client in DIR (default: $HOME/.local/bin).
  --binary PATH        Install this locally built executable instead of downloading one.
  --harness PATH       Install this locally built harness archive instead of downloading one.
  --vm-workflow PATH   Install this locally built vm-workflow executable.
  --harness-type TYPE  Install the adapter for codex, claude, or generic (optional).
  --skill-dir DIR      Install the generic skill below DIR (required for generic).
  --with-vm-workflow   Install vm-workflow and copy optional workflow assets.
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
    --vm-workflow)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--vm-workflow requires a path." >&2; exit 2; }
      LOCAL_VM_WORKFLOW="$2"
      shift 2
      ;;
    --vm-workflow=*) LOCAL_VM_WORKFLOW="${1#*=}"; shift ;;
    --harness-type)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--harness-type requires codex, claude, or generic." >&2; exit 2; }
      HARNESS_TYPE="$2"
      shift 2
      ;;
    --harness-type=*) HARNESS_TYPE="${1#*=}"; shift ;;
    --skill-dir)
      [[ $# -ge 2 && -n "$2" ]] || { echo "--skill-dir requires a path." >&2; exit 2; }
      SKILL_DIR="$2"
      shift 2
      ;;
    --skill-dir=*) SKILL_DIR="${1#*=}"; shift ;;
    --with-vm-workflow) WITH_VM_WORKFLOW=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -n "$LOCAL_VM_WORKFLOW" && "$WITH_VM_WORKFLOW" != true ]]; then
  echo "--vm-workflow requires --with-vm-workflow." >&2
  exit 2
fi

case "$HARNESS_TYPE" in
  ""|codex|claude|generic) ;;
  *) echo "Invalid harness type: $HARNESS_TYPE. Use codex, claude, or generic." >&2; exit 2 ;;
esac
if [[ "$HARNESS_TYPE" == "generic" && -z "$SKILL_DIR" ]]; then
  echo "--skill-dir is required when --harness-type generic is selected." >&2
  exit 2
fi
if [[ "$HARNESS_TYPE" != "generic" && -n "$SKILL_DIR" ]]; then
  echo "--skill-dir is only valid with --harness-type generic." >&2
  exit 2
fi

if [[ -n "$LOCAL_BINARY" || -n "$LOCAL_HARNESS" ]]; then
  [[ -n "$LOCAL_BINARY" && -n "$LOCAL_HARNESS" ]] || {
    echo "--binary and --harness must be supplied together." >&2
    exit 2
  }
else
  validate_version "$VERSION"
fi
if [[ "$WITH_VM_WORKFLOW" == true && -n "$LOCAL_BINARY" && -z "$LOCAL_VM_WORKFLOW" ]]; then
  echo "local --with-vm-workflow installation requires --vm-workflow PATH." >&2
  exit 2
fi
if [[ "$WITH_VM_WORKFLOW" == true && -z "$LOCAL_BINARY" && -n "$LOCAL_VM_WORKFLOW" ]]; then
  echo "--vm-workflow is only valid with local --binary and --harness artifacts." >&2
  exit 2
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

if [[ "$HARNESS_TYPE" == "generic" ]]; then
  if [[ -e "$SKILL_DIR" && ( ! -d "$SKILL_DIR" || -L "$SKILL_DIR" ) ]]; then
    echo "Skill directory must be a directory and not a symbolic link: $SKILL_DIR" >&2
    exit 1
  fi
  mkdir -p "$SKILL_DIR"
  SKILL_DIR="$(cd "$SKILL_DIR" && pwd -P)"
fi

if [[ -e "$BIN_DIR" && ( ! -d "$BIN_DIR" || -L "$BIN_DIR" ) ]]; then
  echo "Binary directory must be a directory and not a symbolic link: $BIN_DIR" >&2
  exit 1
fi
mkdir -p "$BIN_DIR"
BIN_DIR="$(cd "$BIN_DIR" && pwd -P)"

BINARY_NAME="proxyma-client-$TARGET_OS-$TARGET_ARCH"
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/proxyma-client.XXXXXX")"
trap 'rm -rf "$TEMP_DIR"' EXIT
BINARY_PATH="$TEMP_DIR/$BINARY_NAME"
ARCHIVE_PATH="$TEMP_DIR/$ARCHIVE_NAME"
CHECKSUM_PATH="$TEMP_DIR/SHA256SUMS"
VM_WORKFLOW_BINARY_NAME=""
VM_WORKFLOW_BINARY_PATH=""
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  VM_WORKFLOW_BINARY_NAME="vm-workflow-$TARGET_OS-$TARGET_ARCH"
  [[ "$TARGET_OS" == "windows" ]] && VM_WORKFLOW_BINARY_NAME+=".exe"
  VM_WORKFLOW_BINARY_PATH="$TEMP_DIR/$VM_WORKFLOW_BINARY_NAME"
fi

if [[ -n "$LOCAL_BINARY" ]]; then
  [[ -f "$LOCAL_BINARY" && ! -L "$LOCAL_BINARY" ]] || { echo "Local binary must be a regular file and not a symbolic link: $LOCAL_BINARY" >&2; exit 1; }
  [[ -f "$LOCAL_HARNESS" && ! -L "$LOCAL_HARNESS" ]] || { echo "Local harness archive must be a regular file and not a symbolic link: $LOCAL_HARNESS" >&2; exit 1; }
  if [[ "$WITH_VM_WORKFLOW" == true ]]; then
    [[ -f "$LOCAL_VM_WORKFLOW" && ! -L "$LOCAL_VM_WORKFLOW" && -x "$LOCAL_VM_WORKFLOW" ]] || { echo "Local vm-workflow must be an executable regular file and not a symbolic link: $LOCAL_VM_WORKFLOW" >&2; exit 1; }
  fi
  cp "$LOCAL_BINARY" "$BINARY_PATH"
  cp "$LOCAL_HARNESS" "$ARCHIVE_PATH"
  if [[ "$WITH_VM_WORKFLOW" == true ]]; then
    cp "$LOCAL_VM_WORKFLOW" "$VM_WORKFLOW_BINARY_PATH"
  fi
  echo "Installing local proxyma-client artifacts for $TARGET_OS/$TARGET_ARCH..."
else
  if [[ "$VERSION" == "latest" ]]; then
    RELEASE_URL="$REPOSITORY_URL/releases/latest/download"
  else
    RELEASE_URL="$REPOSITORY_URL/releases/download/$VERSION"
  fi
  echo "Downloading proxyma-client $VERSION for $TARGET_OS/$TARGET_ARCH..."
  release_assets=("$BINARY_NAME" "$ARCHIVE_NAME" "SHA256SUMS")
  if [[ "$WITH_VM_WORKFLOW" == true ]]; then
    release_assets+=("$VM_WORKFLOW_BINARY_NAME")
  fi
  for asset_name in "${release_assets[@]}"; do
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
  if [[ "$WITH_VM_WORKFLOW" == true ]]; then
    verify_checksum "$VM_WORKFLOW_BINARY_NAME" "$VM_WORKFLOW_BINARY_PATH"
  fi
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
  "$PAYLOAD_DIR/proxyma-client.json" \
  "$PAYLOAD_DIR/instruction-pack/proxyma-harness.md" \
  "$PAYLOAD_DIR/.agents/skills/proxyma-harness/SKILL.md" \
  "$PAYLOAD_DIR/.claude/skills/proxyma-harness/SKILL.md" \
  "$PAYLOAD_DIR/generic/skills/proxyma-harness/SKILL.md"; do
  if [[ ! -f "$required" || -L "$required" ]]; then
    echo "Harness archive is missing a required regular file: $required" >&2
    exit 1
  fi
done
if [[ "$WITH_VM_WORKFLOW" == true && ! -d "$PAYLOAD_DIR/workflows" ]]; then
  echo "Harness archive is missing optional workflow assets: $PAYLOAD_DIR/workflows" >&2
  exit 1
fi

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

render_workflow_cron() {
  local template_path="$1"
  local destination_path="$2"
  local content
  local project_q bin_q db_q config_q prompt_q launcher_q profile_q report_q

  project_q="$(printf '%q' "$PROJECT_DIR")"
  bin_q="$(printf '%q' "$INSTALLED_VM_WORKFLOW")"
  db_q="$(printf '%q' "/var/lib/proxyma-client-vulnerability-management/workflow.sqlite")"
  config_q="$(printf '%q' "$PROJECT_DIR/harness/proxyma-client.json")"
  prompt_q="$(printf '%q' "$PROJECT_DIR/harness/workflows/vulnerability-management/prompt.md")"
  launcher_q="$(printf '%q' "$PROJECT_DIR/harness/workflows/vulnerability-management/run-hermes-worker.sh")"
  profile_q="$(printf '%q' "$PROJECT_DIR/harness/workflows/vulnerability-management/profiles/nessus-detailed.json")"
  report_q="$(printf '%q' "/var/lib/proxyma-client-vulnerability-management/incoming/latest.csv")"
  content="$(<"$template_path")"
  content="${content//__PROJECT_DIR__/$project_q}"
  content="${content//__VM_WORKFLOW_BIN__/$bin_q}"
  content="${content//__VM_WORKFLOW_DB__/$db_q}"
  content="${content//__VM_WORKFLOW_PROXYMA_CONFIG__/$config_q}"
  content="${content//__VM_WORKFLOW_PROMPT__/$prompt_q}"
  content="${content//__VM_WORKFLOW_LAUNCHER__/$launcher_q}"
  content="${content//__VM_WORKFLOW_PROFILE__/$profile_q}"
  content="${content//__VM_WORKFLOW_REPORT__/$report_q}"
  if [[ "$content" == *__*__* ]]; then
    echo "Workflow cron template contains an unresolved placeholder: $template_path" >&2
    exit 1
  fi
  printf '%s\n' "$content" >"$destination_path"
  chmod 0640 "$destination_path"
}

render_workflow_prompt() {
  local template_path="$1"
  local destination_path="$2"
  local content value

  content="$(<"$template_path")"
  value="$INSTALLED_VM_WORKFLOW"
  value="${value//\'/\'\\\'\'}"
  content="${content//__VM_WORKFLOW_BIN__/$value}"
  value="/var/lib/proxyma-client-vulnerability-management/workflow.sqlite"
  value="${value//\'/\'\\\'\'}"
  content="${content//__VM_WORKFLOW_DB__/$value}"
  value="$PROJECT_DIR/harness/proxyma-client.json"
  value="${value//\'/\'\\\'\'}"
  content="${content//__VM_WORKFLOW_PROXYMA_CONFIG__/$value}"
  if [[ "$content" == *__VM_WORKFLOW_BIN__* || "$content" == *__VM_WORKFLOW_DB__* || "$content" == *__VM_WORKFLOW_PROXYMA_CONFIG__* ]]; then
    echo "Workflow prompt contains an unresolved static placeholder: $template_path" >&2
    exit 1
  fi
  if [[ "$content" != *__VM_WORKFLOW_WORKER_ID__* ]]; then
    echo "Workflow prompt is missing the worker-ID marker: $template_path" >&2
    exit 1
  fi
  printf '%s\n' "$content" >"$destination_path"
  chmod 0644 "$destination_path"
}

INSTALLED_BINARY="$BIN_DIR/proxyma-client"
INSTALLED_VM_WORKFLOW="$BIN_DIR/vm-workflow"
HARNESS_DIR="$PROJECT_DIR/harness"
ROOT_ADAPTER_DIR=""
ROOT_ADAPTER_PATH=""
if [[ "$HARNESS_TYPE" == "codex" ]]; then
  ROOT_ADAPTER_DIR="$PROJECT_DIR/.agents/skills"
  ROOT_ADAPTER_PATH="$ROOT_ADAPTER_DIR/proxyma-harness"
elif [[ "$HARNESS_TYPE" == "claude" ]]; then
  ROOT_ADAPTER_DIR="$PROJECT_DIR/.claude/skills"
  ROOT_ADAPTER_PATH="$ROOT_ADAPTER_DIR/proxyma-harness"
elif [[ "$HARNESS_TYPE" == "generic" ]]; then
  ROOT_ADAPTER_DIR="$SKILL_DIR"
  ROOT_ADAPTER_PATH="$ROOT_ADAPTER_DIR/proxyma-harness"
fi
if [[ -e "$INSTALLED_BINARY" && ( ! -f "$INSTALLED_BINARY" || -L "$INSTALLED_BINARY" ) ]]; then
  echo "Installed binary target must be a regular file and not a symbolic link: $INSTALLED_BINARY" >&2
  exit 1
fi
if [[ "$WITH_VM_WORKFLOW" == true && -e "$INSTALLED_VM_WORKFLOW" && ( ! -f "$INSTALLED_VM_WORKFLOW" || -L "$INSTALLED_VM_WORKFLOW" ) ]]; then
  echo "Installed vm-workflow target must be a regular file and not a symbolic link: $INSTALLED_VM_WORKFLOW" >&2
  exit 1
fi
if [[ -e "$HARNESS_DIR" && ( ! -d "$HARNESS_DIR" || -L "$HARNESS_DIR" ) ]]; then
  echo "Harness target must be a directory and not a symbolic link: $HARNESS_DIR" >&2
  exit 1
fi
for preserved_path in \
  "$HARNESS_DIR/proxyma-client.json" \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/agent-configuration/agent.example.json" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/active" \
  "$HARNESS_DIR/task-state/blocked" \
  "$HARNESS_DIR/task-state/closed" \
  "$HARNESS_DIR/task-state/summaries"; do
  if [[ -L "$preserved_path" ]]; then
    echo "Refusing symbolic link in preserved harness data: $preserved_path" >&2
    exit 1
  fi
done
if [[ -e "$HARNESS_DIR/proxyma-client.json" && ! -f "$HARNESS_DIR/proxyma-client.json" ]]; then
  echo "Harness configuration must be a regular file: $HARNESS_DIR/proxyma-client.json" >&2
  exit 1
fi
if [[ -e "$HARNESS_DIR/agent-configuration/agent.example.json" && ! -f "$HARNESS_DIR/agent-configuration/agent.example.json" ]]; then
  echo "Agent credential example must be a regular file: $HARNESS_DIR/agent-configuration/agent.example.json" >&2
  exit 1
fi
for preserved_dir in \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/active" \
  "$HARNESS_DIR/task-state/blocked" \
  "$HARNESS_DIR/task-state/closed" \
  "$HARNESS_DIR/task-state/summaries"; do
  if [[ -e "$preserved_dir" && ! -d "$preserved_dir" ]]; then
    echo "Preserved harness path must be a directory: $preserved_dir" >&2
    exit 1
  fi
done
if [[ -n "$ROOT_ADAPTER_DIR" ]]; then
  for path_component in "${ROOT_ADAPTER_DIR%/skills}" "$ROOT_ADAPTER_DIR"; do
    if [[ -e "$path_component" && ( ! -d "$path_component" || -L "$path_component" ) ]]; then
      echo "Skill directory must be a directory and not a symbolic link: $path_component" >&2
      exit 1
    fi
  done
fi
owned_destinations=(
  "$HARNESS_DIR/instruction-pack"
  "$HARNESS_DIR/examples"
  "$HARNESS_DIR/tests"
  "$HARNESS_DIR/.agents"
  "$HARNESS_DIR/.claude"
  "$HARNESS_DIR/RELEASE"
)
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  owned_destinations+=("$HARNESS_DIR/workflows")
  owned_destinations+=("$INSTALLED_VM_WORKFLOW")
fi
if [[ -n "$ROOT_ADAPTER_PATH" ]]; then
  owned_destinations+=("$ROOT_ADAPTER_PATH")
fi
for owned_target in "${owned_destinations[@]}"; do
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
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  NEW_VM_WORKFLOW="${INSTALLED_VM_WORKFLOW}.proxyma-new.$$"
  BACKUP_VM_WORKFLOW="${INSTALLED_VM_WORKFLOW}.proxyma-backup.$$"
  if [[ -e "$NEW_VM_WORKFLOW" || -e "$BACKUP_VM_WORKFLOW" ]]; then
    echo "Refusing conflicting vm-workflow transaction path for $INSTALLED_VM_WORKFLOW" >&2
    exit 1
  fi
fi
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  render_workflow_prompt \
    "$PAYLOAD_DIR/workflows/vulnerability-management/prompt.md" \
    "$TEMP_DIR/configured-prompt"
  install -m 0644 "$TEMP_DIR/configured-prompt" \
    "$PAYLOAD_DIR/workflows/vulnerability-management/prompt.md"
  render_workflow_cron \
    "$PAYLOAD_DIR/workflows/vulnerability-management/cron.example" \
    "$TEMP_DIR/configured-cron"
  install -m 0640 "$TEMP_DIR/configured-cron" \
    "$PAYLOAD_DIR/workflows/vulnerability-management/cron"
fi

install -m 0755 "$BINARY_PATH" "$NEW_BINARY"
if [[ -e "$INSTALLED_BINARY" ]]; then mv "$INSTALLED_BINARY" "$BACKUP_BINARY"; fi
if ! mv "$NEW_BINARY" "$INSTALLED_BINARY"; then
  [[ ! -e "$BACKUP_BINARY" ]] || mv "$BACKUP_BINARY" "$INSTALLED_BINARY"
  exit 1
fi
[[ ! -e "$BACKUP_BINARY" ]] || rm -f "$BACKUP_BINARY"
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  install -m 0755 "$VM_WORKFLOW_BINARY_PATH" "$NEW_VM_WORKFLOW"
  if [[ -e "$INSTALLED_VM_WORKFLOW" ]]; then mv "$INSTALLED_VM_WORKFLOW" "$BACKUP_VM_WORKFLOW"; fi
  if ! mv "$NEW_VM_WORKFLOW" "$INSTALLED_VM_WORKFLOW"; then
    [[ ! -e "$BACKUP_VM_WORKFLOW" ]] || mv "$BACKUP_VM_WORKFLOW" "$INSTALLED_VM_WORKFLOW"
    exit 1
  fi
  [[ ! -e "$BACKUP_VM_WORKFLOW" ]] || rm -f "$BACKUP_VM_WORKFLOW"
  if ! "$INSTALLED_VM_WORKFLOW" version >/dev/null; then
    echo "Installed vm-workflow failed its version check: $INSTALLED_VM_WORKFLOW" >&2
    exit 1
  fi
fi

mkdir -p "$HARNESS_DIR"
if [[ ! -e "$HARNESS_DIR/proxyma-client.json" ]]; then
  install -m 0600 "$PAYLOAD_DIR/proxyma-client.json" "$HARNESS_DIR/proxyma-client.json"
fi
install -d -m 0700 \
  "$HARNESS_DIR/agent-configuration" \
  "$HARNESS_DIR/task-state" \
  "$HARNESS_DIR/task-state/active" \
  "$HARNESS_DIR/task-state/blocked" \
  "$HARNESS_DIR/task-state/closed" \
  "$HARNESS_DIR/task-state/summaries"

if [[ ! -e "$HARNESS_DIR/agent-configuration/agent.example.json" ]]; then
  install -m 0644 "$PAYLOAD_DIR/agent-configuration/agent.example.json" "$HARNESS_DIR/agent-configuration/agent.example.json"
fi

for owned in instruction-pack examples tests .agents .claude generic RELEASE; do
  replace_path "$PAYLOAD_DIR/$owned" "$HARNESS_DIR/$owned"
done
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  replace_path "$PAYLOAD_DIR/workflows" "$HARNESS_DIR/workflows"
fi

if [[ "$HARNESS_TYPE" == "codex" ]]; then
  mkdir -p "$ROOT_ADAPTER_DIR"
  replace_path "$PAYLOAD_DIR/.agents/skills/proxyma-harness" "$ROOT_ADAPTER_PATH"
elif [[ "$HARNESS_TYPE" == "claude" ]]; then
  mkdir -p "$ROOT_ADAPTER_DIR"
  replace_path "$PAYLOAD_DIR/.claude/skills/proxyma-harness" "$ROOT_ADAPTER_PATH"
elif [[ "$HARNESS_TYPE" == "generic" ]]; then
  mkdir -p "$ROOT_ADAPTER_DIR"
  replace_path "$PAYLOAD_DIR/generic/skills/proxyma-harness" "$ROOT_ADAPTER_PATH"
fi

echo "Installed proxyma-client at $INSTALLED_BINARY"
echo "Project directory: $PROJECT_DIR"
echo "Installed the matching harness at $HARNESS_DIR"
if [[ "$WITH_VM_WORKFLOW" == true ]]; then
  echo "Installed vm-workflow at $INSTALLED_VM_WORKFLOW"
  echo "Generated configured workflow cron file at $HARNESS_DIR/workflows/vulnerability-management/cron"
fi
if [[ "$HARNESS_TYPE" == "codex" ]]; then
  echo "Installed the Codex adapter at $ROOT_ADAPTER_PATH"
elif [[ "$HARNESS_TYPE" == "claude" ]]; then
  echo "Installed the Claude adapter at $ROOT_ADAPTER_PATH"
elif [[ "$HARNESS_TYPE" == "generic" ]]; then
  echo "Skill directory: $SKILL_DIR"
  echo "Installed the generic skill at $ROOT_ADAPTER_PATH"
else
  echo "No project adapter selected. To enable Codex, run:"
  echo "  mkdir -p \"$PROJECT_DIR/.agents/skills/proxyma-harness\""
  echo "  cp \"$HARNESS_DIR/.agents/skills/proxyma-harness/SKILL.md\" \"$PROJECT_DIR/.agents/skills/proxyma-harness/SKILL.md\""
  echo "To enable Claude Code, run:"
  echo "  mkdir -p \"$PROJECT_DIR/.claude/skills/proxyma-harness\""
  echo "  cp \"$HARNESS_DIR/.claude/skills/proxyma-harness/SKILL.md\" \"$PROJECT_DIR/.claude/skills/proxyma-harness/SKILL.md\""
fi
case ":${PATH:-}:" in
  *":$BIN_DIR:"*) echo "$BIN_DIR is already on PATH." ;;
  *)
    echo "$BIN_DIR is not on PATH. Add it to your shell profile, then restart Codex or Claude Code."
    echo "For example: export PATH=\"$BIN_DIR:\$PATH\""
    ;;
esac
