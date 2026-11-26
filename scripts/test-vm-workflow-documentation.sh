#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_doc="$ROOT/docs/proxyma-client-installation-release.md"
source_doc="$ROOT/docs/proxyma-client-installation-source.md"
testing_doc="$ROOT/docs/release-testing.md"
workflow="$ROOT/.github/workflows/publish_proxyma-server_and_client-release.yml"
workflow_doc="$ROOT/docs/vulnerability-management-workflow.md"
cron_example="$ROOT/harness/workflows/vulnerability-management/cron.example"
worker_launcher="$ROOT/harness/workflows/vulnerability-management/run-hermes-worker.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assets=(
  vm-workflow-linux-amd64
  vm-workflow-linux-arm64
  vm-workflow-darwin-amd64
  vm-workflow-darwin-arm64
  vm-workflow-windows-amd64.exe
  vm-workflow-windows-arm64.exe
)

for file in "$release_doc" "$source_doc" "$testing_doc" "$workflow" "$workflow_doc" "$cron_example" "$worker_launcher"; do
  [[ -f "$file" ]] || fail "missing documentation or release workflow: $file"
done

for asset in "${assets[@]}"; do
  grep -F "$asset" "$release_doc" >/dev/null || fail "release guide omits $asset"
  grep -F "$asset" "$workflow" >/dev/null || fail "release workflow omits $asset"
done

grep -F 'scripts/build-vm-workflow-release.sh' "$source_doc" >/dev/null || fail "source guide omits POSIX build command"
grep -F 'scripts\build-vm-workflow-release.ps1' "$source_doc" >/dev/null || fail "source guide omits PowerShell build command"
grep -F 'vulnerability-management/README.md' "$release_doc" >/dev/null || fail "release guide omits workflow README link"
grep -F 'vulnerability-management/prompt.md' "$release_doc" >/dev/null || fail "release guide omits workflow prompt link"
grep -F 'vulnerability-management/cron.example' "$release_doc" >/dev/null || fail "release guide omits cron example link"
grep -F 'vulnerability-management-workflow.md' "$release_doc" >/dev/null || fail "release guide omits workflow documentation link"
grep -F 'vulnerability-management/README.md' "$source_doc" >/dev/null || fail "source guide omits workflow README link"
grep -F 'vulnerability-management-workflow.md' "$source_doc" >/dev/null || fail "source guide omits workflow documentation link"
grep -F 'vulnerability-management-workflow.md#operational-setup' "$release_doc" >/dev/null || fail "release guide omits operational setup link"
grep -F 'vulnerability-management-workflow.md#operational-setup' "$source_doc" >/dev/null || fail "source guide omits operational setup link"
grep -F -- '--with-vm-workflow' "$release_doc" >/dev/null || fail "release guide omits POSIX workflow opt-in flag"
grep -F -- '-WithVMWorkflow' "$release_doc" >/dev/null || fail "release guide omits PowerShell workflow opt-in flag"
grep -F -- '--with-vm-workflow' "$source_doc" >/dev/null || fail "source guide omits POSIX workflow opt-in flag"
grep -F -- '-WithVMWorkflow' "$source_doc" >/dev/null || fail "source guide omits PowerShell workflow opt-in flag"
for doc in "$release_doc" "$source_doc"; do
  grep -F '/var/lib/proxyma-client-vulnerability-management/workflow.sqlite' "$doc" >/dev/null || fail "installation guide omits canonical workflow database path: $doc"
  if grep -F '$HOME/.local/state/proxyma-vulnerability-management/workflow.sqlite' "$doc" >/dev/null; then
    fail "installation guide contains the obsolete user-local workflow database path: $doc"
  fi
done
source_project_line=$(grep -nF 'PROJECT_DIR="$PWD"' "$source_doc" | tail -n 1 | cut -d: -f1)
source_db_line=$(grep -nF 'VM_DB="/var/lib/proxyma-client-vulnerability-management/workflow.sqlite"' "$source_doc" | tail -n 1 | cut -d: -f1)
[[ -n "$source_project_line" && -n "$source_db_line" && "$source_project_line" -lt "$source_db_line" ]] \
  || fail "source guide must define PROJECT_DIR before the workflow database and derived paths"
grep -F 'SHA256SUMS' "$testing_doc" >/dev/null || fail "release testing guide omits checksum coverage"
grep -F 'vm-workflow version' "$testing_doc" >/dev/null || fail "release testing guide omits workflow smoke check"

grep -F 'By default, the client installers do not install `vm-workflow`' "$release_doc" >/dev/null || fail "release guide does not document default opt-in behavior"
grep -F 'client installer does not' "$source_doc" >/dev/null || fail "source guide does not document default opt-in behavior"
grep -F -- '--vm-workflow' "$source_doc" >/dev/null || fail "source guide omits local vm-workflow installer argument"
grep -F -- '-VMWorkflow' "$source_doc" >/dev/null || fail "source guide omits PowerShell vm-workflow argument"
grep -F 'same directory as `proxyma-client`' "$release_doc" >/dev/null || fail "release guide omits shared binary directory"
grep -F 'same binary directory as' "$source_doc" >/dev/null || fail "source guide omits shared binary directory"

grep -F 'VM_WORKFLOW_STATE_DIR="${VM_WORKFLOW_STATE_DIR:-/var/lib/proxyma-client-vulnerability-management}"' "$workflow_doc" >/dev/null || fail "workflow documentation omits canonical workflow state directory"
grep -F 'VM_WORKFLOW_DB="${VM_WORKFLOW_DB:-$VM_WORKFLOW_STATE_DIR/workflow.sqlite}"' "$workflow_doc" >/dev/null || fail "workflow documentation omits database path derived from workflow state directory"
grep -F '/var/lib/proxyma-client-vulnerability-management/workflow.sqlite' "$cron_example" >/dev/null || fail "cron example omits canonical database path"
grep -F '/run/proxyma-client-vulnerability-management-report.lock' "$cron_example" >/dev/null || fail "cron example omits report lock path"
grep -F '/run/proxyma-client-vulnerability-management-worker.lock' "$cron_example" >/dev/null || fail "cron example omits worker lock path"
grep -F '/run/proxyma-client-vulnerability-management-pulse.lock' "$cron_example" >/dev/null || fail "cron example omits pulse lock path"
grep -F '## Operational setup' "$workflow_doc" >/dev/null || fail "workflow documentation omits operational setup"
grep -F 'pulse --action-required' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse setup"
grep -F 'exits `0`' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse exit code 0"
grep -F 'Exit `1`' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse exit code 1"
grep -F '2` or higher' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse error exit codes"
grep -F 'crontab -e' "$workflow_doc" >/dev/null || fail "workflow documentation omits cron installation"
grep -F 'Task' "$workflow_doc" >/dev/null || fail "workflow documentation omits Windows scheduling"
grep -F 'VM_WORKFLOW_BIN' "$worker_launcher" >/dev/null || fail "worker launcher omits binary contract"
grep -F 'VM_WORKFLOW_DB' "$worker_launcher" >/dev/null || fail "worker launcher omits database contract"
grep -F 'VM_WORKFLOW_PROXYMA_CONFIG' "$worker_launcher" >/dev/null || fail "worker launcher omits Proxyma config contract"
grep -F 'VM_WORKFLOW_WORKER_ID' "$worker_launcher" >/dev/null || fail "worker launcher omits worker ID contract"
grep -F 'run-hermes-worker.sh' "$cron_example" >/dev/null || fail "cron example does not use worker launcher"
grep -F 'VM_WORKFLOW_LAUNCHER' "$cron_example" >/dev/null || fail "cron example omits launcher path"
grep -F 'VM_WORKFLOW_WORKER_ID' "$workflow_doc" >/dev/null || fail "workflow documentation omits worker ID contract"
grep -F 'VM_WORKFLOW_WORKER_ID' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits worker ID contract"
if grep -F '<worker-id>' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null; then
  fail "workflow prompt contains unresolved worker ID placeholder"
fi
if grep -F 'vm-workflow claim' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null; then
  fail "workflow prompt contains an unconfigured vm-workflow claim command"
fi

echo "vm-workflow documentation validation passed."
