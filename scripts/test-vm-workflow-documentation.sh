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
prompt_configurator="$ROOT/harness/workflows/vulnerability-management/configure-hermes-worker-prompt.sh"

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

for file in "$release_doc" "$source_doc" "$testing_doc" "$workflow" "$workflow_doc" "$cron_example" "$worker_launcher" "$prompt_configurator"; do
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
grep -F 'vulnerability-management-workflow.md#install-and-enable-the-workflow' "$release_doc" >/dev/null || fail "release guide omits workflow installation link"
grep -F 'vulnerability-management-workflow.md#install-and-enable-the-workflow' "$source_doc" >/dev/null || fail "source guide omits workflow installation link"
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
source_db_line=$(grep -nF 'VM_DB="/var/lib/proxyma-client-vulnerability-management/workflow.sqlite"' "$source_doc" | tail -n 1 | cut -d: -f1)
source_project_line=$(awk -v db_line="$source_db_line" '/PROJECT_DIR="\$PWD"/ && NR < db_line { line = NR } END { print line }' "$source_doc")
[[ -n "$source_project_line" && -n "$source_db_line" && "$source_project_line" -lt "$source_db_line" ]] \
  || fail "source guide must define PROJECT_DIR before the workflow database and derived paths"
grep -F 'SHA256SUMS' "$testing_doc" >/dev/null || fail "release testing guide omits checksum coverage"
grep -F 'vm-workflow version' "$testing_doc" >/dev/null || fail "release testing guide omits workflow smoke check"

grep -F 'By default, the client installers do not install `vm-workflow`' "$release_doc" >/dev/null || fail "release guide does not document default opt-in behavior"
grep -F 'client installer does not' "$source_doc" >/dev/null || fail "source guide does not document default opt-in behavior"
grep -F -- '--vm-workflow' "$source_doc" >/dev/null || fail "source guide omits local vm-workflow installer argument"
grep -F -- '-VMWorkflow' "$source_doc" >/dev/null || fail "source guide omits PowerShell vm-workflow argument"
grep -F '### Install without `vm-workflow`' "$source_doc" >/dev/null || fail "source guide omits client-only installation subsection"
grep -F '### Install with `vm-workflow`' "$source_doc" >/dev/null || fail "source guide omits workflow installation subsection"
grep -F 'same directory as `proxyma-client`' "$release_doc" >/dev/null || fail "release guide omits shared binary directory"
grep -F 'same binary directory as' "$source_doc" >/dev/null || fail "source guide omits shared binary directory"
grep -F '## Remove the source installation' "$source_doc" >/dev/null || fail "source guide omits source uninstallation heading"
grep -F 'rm -f "$BIN_DIR/proxyma-client"' "$source_doc" >/dev/null || fail "source guide omits POSIX proxyma-client removal command"
grep -F 'rm -f "$BIN_DIR/vm-workflow"' "$source_doc" >/dev/null || fail "source guide omits POSIX vm-workflow removal command"
grep -F 'Join-Path $BinDir "proxyma-client.exe"' "$source_doc" >/dev/null || fail "source guide omits PowerShell proxyma-client removal command"
grep -F 'Join-Path $BinDir "vm-workflow.exe"' "$source_doc" >/dev/null || fail "source guide omits PowerShell vm-workflow removal command"
grep -F "workflow's cron entries or Windows Task Scheduler jobs" "$source_doc" >/dev/null || fail "source guide omits scheduled-job shutdown guidance"
grep -F 'preserve `workflow.sqlite`, reports, backups' "$source_doc" >/dev/null || fail "source guide omits workflow-state preservation guidance"
grep -F 'vulnerability-management-workflow.md#5-uninstall' "$source_doc" >/dev/null || fail "source guide omits workflow uninstallation link"
grep -F '**Safe cleanup:**' "$source_doc" >/dev/null || fail "source guide omits safe cleanup choice"
grep -F '**Complete workflow purge:**' "$source_doc" >/dev/null || fail "source guide omits complete purge choice"

grep -F 'VM_WORKFLOW_STATE_DIR="${VM_WORKFLOW_STATE_DIR:-/var/lib/proxyma-client-vulnerability-management}"' "$workflow_doc" >/dev/null || fail "workflow documentation omits canonical workflow state directory"
grep -F 'PROJECT_DIR="${PROJECT_DIR:?Set PROJECT_DIR to the Proxyma project directory}"' "$workflow_doc" >/dev/null || fail "workflow documentation does not mark PROJECT_DIR as required"
grep -F '### POSIX/Linux/macOS shell setup' "$workflow_doc" >/dev/null || fail "workflow documentation omits POSIX environment setup heading"
grep -F '`export` makes each variable available to child processes' "$workflow_doc" >/dev/null || fail "workflow documentation does not explain export"
grep -F 'source installation: `$HOME/.local/bin/vm-workflow`' "$workflow_doc" >/dev/null || fail "workflow documentation omits source binary location"
grep -F 'release installation: `$HOME/.local/bin/vm-workflow`' "$workflow_doc" >/dev/null || fail "workflow documentation omits release binary location"
grep -F 'export VM_WORKFLOW_BIN="${VM_WORKFLOW_BIN:-$HOME/.local/bin/vm-workflow}"' "$workflow_doc" >/dev/null || fail "workflow documentation omits the default workflow binary path"
grep -F 'export VM_WORKFLOW_REPORT="${VM_WORKFLOW_REPORT:?Set VM_WORKFLOW_REPORT to the downloaded CSV or XLSX report path}"' "$workflow_doc" >/dev/null || fail "workflow documentation does not require the report path"
grep -F 'latest.csv' "$workflow_doc" >/dev/null || fail "workflow documentation does not explain report filename configuration"
grep -F 'export VM_WORKFLOW_REPORT="$VM_WORKFLOW_INCOMING_DIR/Weekly - SecNet Vulnerabilities_20261116.csv"' "$workflow_doc" >/dev/null || fail "workflow documentation omits a report path example"
grep -F '### Windows PowerShell setup' "$workflow_doc" >/dev/null || fail "workflow documentation omits PowerShell environment setup heading"
grep -F '$env:VM_WORKFLOW_BIN' "$workflow_doc" >/dev/null || fail "workflow documentation omits PowerShell environment variables"
grep -F 'VM_WORKFLOW_DB="${VM_WORKFLOW_DB:-$VM_WORKFLOW_STATE_DIR/workflow.sqlite}"' "$workflow_doc" >/dev/null || fail "workflow documentation omits database path derived from workflow state directory"
grep -F 'VM_WORKFLOW_PROFILE' "$workflow_doc" >/dev/null || fail "workflow documentation omits profile configuration"
grep -F 'another vulnerability scanner' "$workflow_doc" >/dev/null || fail "workflow documentation omits alternate scanner profile guidance"
grep -F '/var/lib/proxyma-client-vulnerability-management/workflow.sqlite' "$cron_example" >/dev/null || fail "cron example omits canonical database path"
grep -F '/run/proxyma-client-vulnerability-management-report.lock' "$cron_example" >/dev/null || fail "cron example omits report lock path"
grep -F '/run/proxyma-client-vulnerability-management-worker.lock' "$cron_example" >/dev/null || fail "cron example omits worker lock path"
grep -F '/run/proxyma-client-vulnerability-management-pulse.lock' "$cron_example" >/dev/null || fail "cron example omits pulse lock path"
grep -F '## 1. Install and enable the workflow' "$workflow_doc" >/dev/null || fail "workflow documentation omits installation section"
grep -F '## 2. Configure scheduled operation' "$workflow_doc" >/dev/null || fail "workflow documentation omits scheduled operation section"
grep -F '## 3. Understand runtime behavior' "$workflow_doc" >/dev/null || fail "workflow documentation omits runtime behavior section"
grep -F '## 4. Monitor and troubleshoot' "$workflow_doc" >/dev/null || fail "workflow documentation omits troubleshooting section"
grep -F '## 5. Uninstall' "$workflow_doc" >/dev/null || fail "workflow documentation omits uninstall section"
grep -F 'pulse --action-required' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse setup"
grep -F 'exits `0`' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse exit code 0"
grep -F 'Exit `1`' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse exit code 1"
grep -F '2` or higher' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse error exit codes"
grep -F 'crontab -e' "$workflow_doc" >/dev/null || fail "workflow documentation omits cron installation"
grep -F '$PROJECT_DIR/harness/workflows/vulnerability-management/cron' "$workflow_doc" >/dev/null || fail "workflow documentation omits generated cron path"
grep -F 'CRON_FILE="$PROJECT_DIR/harness/workflows/vulnerability-management/cron"' "$workflow_doc" >/dev/null || fail "workflow documentation omits CRON_FILE setup"
grep -F 'crontab "$CRON_FILE"' "$workflow_doc" >/dev/null || fail "workflow documentation omits generated crontab installation"
grep -F 'replaces the current user crontab' "$workflow_doc" >/dev/null || fail "workflow documentation omits crontab replacement warning"
grep -F 'crontab -l > "$PROJECT_DIR/harness/workflows/vulnerability-management/crontab.backup"' "$workflow_doc" >/dev/null || fail "workflow documentation omits crontab backup command"
grep -F "grep -nE '^[^#[:space:]]' \"\$CRON_FILE\"" "$workflow_doc" >/dev/null || fail "workflow documentation omits generated cron verification"
if grep -F 'Install the POSIX examples from' "$workflow_doc" >/dev/null; then
  fail "workflow documentation still presents cron.example as the primary installation file"
fi
grep -F 'Task' "$workflow_doc" >/dev/null || fail "workflow documentation omits Windows scheduling"
grep -F 'Weekly report import' "$workflow_doc" >/dev/null || fail "workflow documentation omits weekly cron explanation"
grep -F 'Optional immediate worker kickoff' "$workflow_doc" >/dev/null || fail "workflow documentation omits worker kickoff explanation"
grep -F 'Five-minute pulse and readiness gate' "$workflow_doc" >/dev/null || fail "workflow documentation omits pulse explanation"
grep -F 'prompt.md' "$workflow_doc" >/dev/null || fail "workflow documentation omits prompt link"
grep -F 'Safe uninstall' "$workflow_doc" >/dev/null || fail "workflow documentation omits safe uninstall"
grep -F 'Complete purge' "$workflow_doc" >/dev/null || fail "workflow documentation omits complete purge"
grep -F 'preserve the database' "$workflow_doc" >/dev/null || fail "workflow documentation omits uninstall data preservation"
grep -F 'VM_WORKFLOW_BIN' "$worker_launcher" >/dev/null || fail "worker launcher omits binary contract"
grep -F ': "${VM_WORKFLOW_BIN:=${HOME}/.local/bin/vm-workflow}"' "$worker_launcher" >/dev/null || fail "worker launcher default does not match installer binary location"
grep -F 'VM_WORKFLOW_DB' "$worker_launcher" >/dev/null || fail "worker launcher omits database contract"
grep -F 'VM_WORKFLOW_PROXYMA_CONFIG' "$worker_launcher" >/dev/null || fail "worker launcher omits Proxyma config contract"
grep -F 'VM_WORKFLOW_WORKER_ID' "$worker_launcher" >/dev/null || fail "worker launcher omits worker ID contract"
grep -F -- '--in "$VM_WORKFLOW_PROJECT_DIR"' "$worker_launcher" >/dev/null || fail "worker launcher omits Hermes working-directory option"
grep -F 'chat' "$worker_launcher" >/dev/null || fail "worker launcher omits Hermes chat command"
grep -F -- '--query-file "$TEMP_PROMPT"' "$worker_launcher" >/dev/null || fail "worker launcher omits temporary Hermes query-file option"
grep -F -- '--oneshot' "$worker_launcher" >/dev/null || fail "worker launcher omits Hermes one-shot option"
grep -F -- '--skill proxyma-harness' "$worker_launcher" >/dev/null || fail "worker launcher omits harness skill option"
grep -F -- '--toolsets terminal,file,skills' "$worker_launcher" >/dev/null || fail "worker launcher omits Hermes workflow toolsets"
if grep -F -- '--workdir' "$worker_launcher" >/dev/null || grep -F -- '--prompt-file' "$worker_launcher" >/dev/null; then
  fail "worker launcher contains obsolete Hermes invocation options"
fi
grep -F 'run-hermes-worker.sh' "$cron_example" >/dev/null || fail "cron example does not use worker launcher"
grep -F 'PROJECT_DIR=__PROJECT_DIR__' "$cron_example" >/dev/null || fail "cron example omits project directory placeholder"
if grep -F '/srv/' "$cron_example" "$worker_launcher" "$workflow_doc" >/dev/null; then
  fail "workflow assets or documentation contain obsolete /srv paths"
fi
grep -F 'VM_WORKFLOW_LAUNCHER' "$cron_example" >/dev/null || fail "cron example omits launcher path"
if grep -n '\\$' "$cron_example" >/dev/null; then
  fail "cron example contains a physical-line continuation"
fi
[[ "$(grep -Ec '^(0 2|15 2|\*/5)' "$cron_example")" -eq 3 ]] || fail "cron example does not contain three single-line scheduled entries"
grep -F 'VM_WORKFLOW_WORKER_ID' "$workflow_doc" >/dev/null || fail "workflow documentation omits worker ID contract"
grep -F 'generates the' "$workflow_doc" >/dev/null || fail "workflow documentation omits configured cron output"
grep -F 'installer does not modify the active' "$workflow_doc" >/dev/null || fail "workflow documentation omits crontab safety"
grep -F 'VM_WORKFLOW_WORKER_ID' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits worker ID contract"
if grep -F 'required_environment_variables:' "$ROOT/harness/generic/skills/proxyma-harness/SKILL.md" >/dev/null; then
  fail "generic skill contains workflow-specific environment metadata"
fi
grep -F '__VM_WORKFLOW_BIN__' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits binary placeholder"
grep -F '__VM_WORKFLOW_DB__' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits database placeholder"
grep -F '__VM_WORKFLOW_PROXYMA_CONFIG__' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits Proxyma config placeholder"
grep -F "export VM_WORKFLOW_WORKER_ID='__VM_WORKFLOW_WORKER_ID__'" "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits worker initialization marker"
grep -F 'test -n "${VM_WORKFLOW_BIN:-}"' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits binary environment preflight"
grep -F 'test -n "${VM_WORKFLOW_WORKER_ID:-}"' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits worker environment preflight"
grep -F 'test -x "$VM_WORKFLOW_BIN"' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null || fail "workflow prompt omits binary validation"
grep -F 'PROJECT_DIR must identify' "$worker_launcher" >/dev/null || fail "worker launcher does not require PROJECT_DIR"
grep -F 'workflow prompt is not configured' "$worker_launcher" >/dev/null || fail "worker launcher does not reject unconfigured prompts"
grep -F 'TEMP_PROMPT' "$worker_launcher" >/dev/null || fail "worker launcher does not create a temporary worker prompt"
grep -F 'trap cleanup_prompt EXIT' "$worker_launcher" >/dev/null || fail "worker launcher does not clean up the temporary prompt"
grep -F 'configure-hermes-worker-prompt.sh' "$workflow_doc" >/dev/null || fail "workflow documentation omits manual prompt configurator"
if env -u PROJECT_DIR bash "$worker_launcher" >/dev/null 2>&1; then
  fail "worker launcher accepted a missing PROJECT_DIR"
fi
if grep -F '<worker-id>' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null; then
  fail "workflow prompt contains unresolved worker ID placeholder"
fi
if grep -F 'vm-workflow claim' "$ROOT/harness/workflows/vulnerability-management/prompt.md" >/dev/null; then
  fail "workflow prompt contains an unconfigured vm-workflow claim command"
fi

echo "vm-workflow documentation validation passed."
