#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIRMED=false

usage() {
  cat <<EOF
Usage: bash ./scripts/uninstall-local-development.sh --yes

Removes known ignored build outputs, dependencies, caches, configuration,
secrets, and runtime state from this development checkout. Tracked files,
the source checkout, and globally installed tools are retained.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes)
      CONFIRMED=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

if [[ ! -d "$ROOT/.git" || ! -f "$ROOT/go.mod" || ! -f "$ROOT/frontend/package.json" ]]; then
  echo "Refusing to clean an unverified Proxyma repository root: $ROOT" >&2
  exit 1
fi

cleanup_paths=(
  bin dist build internal/web/dist
  frontend
  .gocache .gotmp .tmp var proxyma.config.json
  asset targets target policies users agents secrets interactions
  task-state harness/task-state harness/agent-configuration
  coverage.out coverage.html '*.coverprofile' '*.prof' '*.pprof' '*.test' '*.out' '*.log'
)

echo "Ignored files under these repository paths will be removed:"
printf '  %s\n' "${cleanup_paths[@]}"
echo "Tracked files, the repository, Go, Node.js, npm, and global caches will be retained."

if [[ "$CONFIRMED" != true ]]; then
  echo "Re-run with --yes to continue."
  exit 2
fi

git -C "$ROOT" clean -fdX -- "${cleanup_paths[@]}"
echo "Removed local Proxyma development artifacts."
