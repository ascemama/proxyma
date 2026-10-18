param([switch]$Quiet)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$Errors = [System.Collections.Generic.List[string]]::new()

function Add-Error([string]$Message) { $Errors.Add($Message) }
function Read-Json([string]$RelativePath) {
    $path = Join-Path $RepoRoot $RelativePath
    if (-not (Test-Path -LiteralPath $path)) { Add-Error "Missing $RelativePath"; return $null }
    try { return Get-Content -LiteralPath $path -Raw | ConvertFrom-Json }
    catch { Add-Error "Invalid JSON in ${RelativePath}: $($_.Exception.Message)"; return $null }
}
function Require-Property($Object, [string]$Name, [string]$Label) {
    if ($null -eq $Object -or $Object.PSObject.Properties.Name -notcontains $Name) { Add-Error "$Label missing $Name" }
}
function Require-Text([string]$RelativePath, [string[]]$Terms) {
    $path = Join-Path $RepoRoot $RelativePath
    if (-not (Test-Path -LiteralPath $path)) { Add-Error "Missing $RelativePath"; return }
    $content = Get-Content -LiteralPath $path -Raw
    foreach ($term in $Terms) { if (-not $content.Contains($term)) { Add-Error "$RelativePath missing '$term'" } }
}

$config = Read-Json "harness/proxyma-agent.json"
foreach ($name in @("schema_version", "proxyma_base_url", "agent_config", "state_dir")) { Require-Property $config $name "harness/proxyma-agent.json" }
if ($null -ne $config -and $config.schema_version -ne "2") { Add-Error "Harness config must use schema version 2" }

$credentialExample = Read-Json "harness/agent-configuration/agent.example.json"
foreach ($name in @("agent_id", "agent_jwt_signing_key")) { Require-Property $credentialExample $name "harness/agent-configuration/agent.example.json" }
if ($null -ne $credentialExample) {
    if ($credentialExample.agent_id -ne "TOBECHANGED" -or $credentialExample.agent_jwt_signing_key -ne "TOBECHANGED") {
        Add-Error "Credential example must contain placeholders only"
    }
}

$configSchema = Read-Json "harness/instruction-pack/proxyma-agent.schema.json"
$stateSchema = Read-Json "harness/instruction-pack/task-state.schema.json"
if ($null -ne $stateSchema -and $stateSchema.properties.schema_version.const -ne "2") { Add-Error "Task schema must require version 2" }

$examples = @(
    foreach ($stateDir in @("active", "blocked", "closed")) {
        Get-ChildItem -LiteralPath (Join-Path $RepoRoot "harness/examples/task-state/$stateDir") -Filter "*.json" -File -ErrorAction SilentlyContinue
    }
)
if ($examples.Count -lt 2) { Add-Error "Expected at least two v2 task examples" }
foreach ($file in $examples) {
    $task = Read-Json $file.FullName.Substring($RepoRoot.Length + 1)
    foreach ($name in @("schema_version", "client_task_id", "revision", "user_goal", "status", "created_at", "updated_at", "remaining_work", "operations")) { Require-Property $task $name $file.Name }
    if ($null -ne $task -and $task.schema_version -ne "2") { Add-Error "$($file.Name) is not v2" }
    foreach ($operation in @($task.operations)) {
        foreach ($name in @("operation_id", "request_hash", "phase", "target", "payload", "payload_kind", "canonical_command", "reasoning", "requested_by", "prepared_at")) { Require-Property $operation $name "$($file.Name) operation" }
    }
}

$core = "harness/instruction-pack/proxyma-harness.md"
$codex = "harness/.agents/skills/proxyma-harness/SKILL.md"
$claude = "harness/.claude/skills/proxyma-harness/SKILL.md"
Require-Text $core @("proxyma-agent resume", "task-complete", "Never directly use SSH", "Never read or edit", "uncertain submission", "Harness v1")
Require-Text $codex @("proxyma-agent resume", 'requested_by: "codex"', "task-complete")
Require-Text $claude @("proxyma-agent resume", 'requested_by: "claude-code"', "task-complete")

foreach ($adapter in @($codex, $claude)) {
    $length = (Get-Content -LiteralPath (Join-Path $RepoRoot $adapter) -Raw).Length
    if ($length -ge 4000) { Add-Error "$adapter exceeds the 4,000-character adapter budget ($length)" }
}
$loadedBudget = (Get-Content -LiteralPath (Join-Path $RepoRoot $core) -Raw).Length + [Math]::Max(
    (Get-Content -LiteralPath (Join-Path $RepoRoot $codex) -Raw).Length,
    (Get-Content -LiteralPath (Join-Path $RepoRoot $claude) -Raw).Length)
if ($loadedBudget -ge 10000) { Add-Error "Automatically loaded instruction budget exceeds 10,000 characters ($loadedBudget)" }

foreach ($scenario in @("v2-resume-uncertain.md", "v2-completed-result.md", "v2-proxyma-only.md", "v2-revision-conflict.md", "v2-adapter-coverage.md")) {
    if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot "harness/tests/scenarios/$scenario"))) { Add-Error "Missing scenario $scenario" }
}

$modelFacing = @($core, $codex, $claude)
foreach ($file in $modelFacing) {
    $content = Get-Content -LiteralPath (Join-Path $RepoRoot $file) -Raw
    foreach ($forbidden in @("Authorization: Bearer", "new-proxyma-jwt", "POST /v1/agent/commands", "agent_jwt_signing_key")) {
        if ($content.Contains($forbidden)) { Add-Error "$file exposes procedural detail '$forbidden'" }
    }
}

if ($Errors.Count -gt 0) {
    if (-not $Quiet) { Write-Host "Proxyma harness v2 validation failed:"; $Errors | ForEach-Object { Write-Host "- $_" } }
    exit 1
}
if (-not $Quiet) {
    Write-Host "Proxyma harness v2 validation passed."
    Write-Host "Checked v2 config/state assets, CLI-only policy, adapters, scenarios, and instruction budgets."
}
