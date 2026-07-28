param(
    [switch]$Quiet
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$Errors = New-Object System.Collections.Generic.List[string]

function Add-ValidationError {
    param([string]$Message)
    $script:Errors.Add($Message) | Out-Null
}

function Read-JsonFile {
    param([string]$Path)

    try {
        return Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    } catch {
        Add-ValidationError "JSON parse failed for $Path`: $($_.Exception.Message)"
        return $null
    }
}

function Assert-FileExists {
    param(
        [string]$RelativePath,
        [string]$Label
    )

    $path = Join-Path $RepoRoot $RelativePath
    if (-not (Test-Path -LiteralPath $path)) {
        Add-ValidationError "$Label missing at $RelativePath"
        return $null
    }
    return $path
}

function Assert-ContainsAll {
    param(
        [string]$RelativePath,
        [string[]]$Terms,
        [string]$Label
    )

    $path = Assert-FileExists -RelativePath $RelativePath -Label $Label
    if ($null -eq $path) {
        return
    }

    $content = (Get-Content -LiteralPath $path -Raw).ToLowerInvariant()
    foreach ($term in $Terms) {
        if (-not $content.Contains($term.ToLowerInvariant())) {
            Add-ValidationError "$Label is missing required phrase '$term'"
        }
    }
}

function Assert-ExcludesAll {
    param(
        [string]$RelativePath,
        [string[]]$Terms,
        [string]$Label
    )

    $path = Assert-FileExists -RelativePath $RelativePath -Label $Label
    if ($null -eq $path) {
        return
    }

    $content = (Get-Content -LiteralPath $path -Raw).ToLowerInvariant()
    foreach ($term in $Terms) {
        if ($content.Contains($term.ToLowerInvariant())) {
            Add-ValidationError "$Label contains forbidden phrase '$term'"
        }
    }
}

function Has-Property {
    param(
        [object]$Object,
        [string]$Name
    )

    return $Object.PSObject.Properties.Name -contains $Name
}

function Assert-RequiredProperties {
    param(
        [object]$Object,
        [string[]]$Properties,
        [string]$Label
    )

    foreach ($property in $Properties) {
        if (-not (Has-Property -Object $Object -Name $property)) {
            Add-ValidationError "$Label is missing required property '$property'"
        }
    }
}

$schemaPaths = @(
    "harness/instruction-pack/task-state.schema.json",
    "harness/instruction-pack/proxyma-harness-run.schema.json"
)

[void](Assert-FileExists -RelativePath "harness/instruction-pack/proxyma-api-reference.md" -Label "Instruction-pack API reference")
[void](Assert-FileExists -RelativePath "harness/instruction-pack/auth/proxyma-auth.md" -Label "Instruction-pack auth reference")
[void](Assert-FileExists -RelativePath "harness/instruction-pack/auth/new-proxyma-jwt.ps1" -Label "PowerShell Proxyma JWT script")
[void](Assert-FileExists -RelativePath "harness/instruction-pack/auth/new-proxyma-jwt.sh" -Label "Bash Proxyma JWT script")
[void](Assert-FileExists -RelativePath "harness/agent-configuration/df9ec8bb-1a22-4181-a7c8-126ad553221d.json" -Label "Harness agent config")

$apiExamplePaths = @(
    "harness/instruction-pack/api-examples/commands.request.json",
    "harness/instruction-pack/api-examples/targets.resolve.response.json",
    "harness/instruction-pack/api-examples/commands.created.response.json",
    "harness/instruction-pack/api-examples/commands.transcriptor-error.response.json",
    "harness/instruction-pack/api-examples/tasks.running.response.json",
    "harness/instruction-pack/api-examples/tasks.completed.response.json",
    "harness/instruction-pack/api-examples/tasks.failed.response.json",
    "harness/instruction-pack/api-examples/health.response.json"
)

foreach ($relativePath in $apiExamplePaths) {
    $path = Assert-FileExists -RelativePath $relativePath -Label "API example"
    if ($null -ne $path) {
        [void](Read-JsonFile -Path $path)
    }
}

foreach ($relativePath in $schemaPaths) {
    $path = Assert-FileExists -RelativePath $relativePath -Label "Schema"
    if ($null -ne $path) {
        [void](Read-JsonFile -Path $path)
    }
}

$runSchemaPath = Join-Path $RepoRoot "harness\instruction-pack\proxyma-harness-run.schema.json"
$runSchema = Read-JsonFile -Path $runSchemaPath
if ($null -ne $runSchema) {
    if ($runSchema.required -notcontains "proxyma_base_url") {
        Add-ValidationError "Harness run schema must require 'proxyma_base_url'"
    }
    if ($runSchema.required -notcontains "agent_config") {
        Add-ValidationError "Harness run schema must require 'agent_config'"
    }

    if (-not (Has-Property -Object $runSchema -Name "properties")) {
        Add-ValidationError "Harness run schema is missing properties"
    } else {
        $proxymaBaseUrlProperty = $runSchema.properties.proxyma_base_url
        if ($null -eq $proxymaBaseUrlProperty) {
            Add-ValidationError "Harness run schema must define properties.proxyma_base_url"
        } else {
            if ($proxymaBaseUrlProperty.type -ne "string") {
                Add-ValidationError "Harness run schema proxyma_base_url must be a string"
            }
            if ($proxymaBaseUrlProperty.format -ne "uri") {
                Add-ValidationError "Harness run schema proxyma_base_url must use format 'uri'"
            }
        }
        $agentConfigProperty = $runSchema.properties.agent_config
        if ($null -eq $agentConfigProperty) {
            Add-ValidationError "Harness run schema must define properties.agent_config"
        } else {
            if ($agentConfigProperty.type -ne "string") {
                Add-ValidationError "Harness run schema agent_config must be a string"
            }
        }
    }
}

$liveRunConfigPath = Join-Path $RepoRoot "harness\proxyma-harness-run.json"
if (Test-Path -LiteralPath $liveRunConfigPath) {
    $liveRunConfig = Read-JsonFile -Path $liveRunConfigPath
    if ($null -ne $liveRunConfig) {
        Assert-RequiredProperties -Object $liveRunConfig -Properties @("run_id", "started_at", "proxyma_base_url", "agent_config", "state_dir", "active_poll_interval_seconds") -Label "harness/proxyma-harness-run.json"
    }
} else {
    Add-ValidationError "Live run configuration missing at harness/proxyma-harness-run.json"
}

$runExamplePath = Join-Path $RepoRoot "harness\examples\proxyma-harness-run.json"
if (Test-Path -LiteralPath $runExamplePath) {
    $runExample = Read-JsonFile -Path $runExamplePath
    if ($null -ne $runExample) {
        Assert-RequiredProperties -Object $runExample -Properties @("run_id", "started_at", "proxyma_base_url", "agent_config", "state_dir", "active_poll_interval_seconds") -Label "harness/examples/proxyma-harness-run.json"
    }
} else {
    Add-ValidationError "Run configuration example missing at harness/examples/proxyma-harness-run.json"
}

$exampleRoot = Join-Path $RepoRoot "harness\examples\task-state"
$exampleFiles = @(Get-ChildItem -LiteralPath $exampleRoot -Recurse -Filter "*.json" -ErrorAction SilentlyContinue)
if ($exampleFiles.Count -eq 0) {
    Add-ValidationError "No task-state JSON examples found under harness/examples/task-state"
}

$validStatuses = @("pending", "waiting_proxyma", "done", "failed", "blocked")
$requiredTaskFields = @("schema_version", "user_goal", "status", "created_at", "updated_at", "remaining_work")
$requiredCommandFields = @("proxyma_task_id", "target", "command", "reasoning", "submitted_at", "status")
$requiredResultFields = @("proxyma_task_id", "status", "checked_at")

foreach ($file in $exampleFiles) {
    $json = Read-JsonFile -Path $file.FullName
    if ($null -eq $json) {
        continue
    }

    $label = $file.FullName.Substring($RepoRoot.Path.Length + 1)
    Assert-RequiredProperties -Object $json -Properties $requiredTaskFields -Label $label

    if ((Has-Property -Object $json -Name "status") -and ($validStatuses -notcontains $json.status)) {
        Add-ValidationError "$label has invalid status '$($json.status)'"
    }

    if ($json.status -eq "blocked") {
        Assert-RequiredProperties -Object $json -Properties @("blocked_reason", "required_next_action") -Label $label
    }

    if ($json.status -eq "failed") {
        Assert-RequiredProperties -Object $json -Properties @("final_summary") -Label $label
    }

    if (Has-Property -Object $json -Name "proxyma_commands") {
        foreach ($command in @($json.proxyma_commands)) {
            Assert-RequiredProperties -Object $command -Properties $requiredCommandFields -Label "$label proxyma_commands"
            if ((Has-Property -Object $command -Name "target")) {
                $target = $command.target
                if (($null -eq $target) -or ((-not (Has-Property -Object $target -Name "hostname")) -and (-not (Has-Property -Object $target -Name "ip")))) {
                    Add-ValidationError "$label proxyma_commands target must include hostname or ip"
                }
            }
        }
    }

    if (Has-Property -Object $json -Name "proxyma_results") {
        foreach ($result in @($json.proxyma_results)) {
            Assert-RequiredProperties -Object $result -Properties $requiredResultFields -Label "$label proxyma_results"
        }
    }
}

$multiCommandPath = Join-Path $RepoRoot "harness\examples\task-state\done\20260515-113000-check-target-disk-usage-through-proxyma.json"
if (Test-Path -LiteralPath $multiCommandPath) {
    $multi = Read-JsonFile -Path $multiCommandPath
    if ($null -ne $multi -and @($multi.proxyma_commands).Count -lt 2) {
        Add-ValidationError "Done example must contain at least two Proxyma command references"
    }
} else {
    Add-ValidationError "Multi-command done example missing"
}

$scenarioFiles = @(
    "harness/tests/scenarios/us1-pending-working.md",
    "harness/tests/scenarios/us1-completed-result.md",
    "harness/tests/scenarios/us1-Proxyma-unreachable.md",
    "harness/tests/scenarios/us1-Proxyma-final-failure.md",
    "harness/tests/scenarios/us2-Proxyma-only-target-access.md",
    "harness/tests/scenarios/us2-no-local-ssh-scp.md",
    "harness/tests/scenarios/us2-no-credential-file-lookup.md",
    "harness/tests/scenarios/us2-reject-bare-remote-payload.md",
    "harness/tests/scenarios/us2-reject-direct-target-bypass.md",
    "harness/tests/scenarios/us3-restart-resume.md",
    "harness/tests/scenarios/us3-duplicate-prevention.md",
    "harness/tests/scenarios/us3-malformed-state.md",
    "harness/tests/scenarios/us3-json-authority.md",
    "harness/tests/scenarios/us4-instruction-pack-coverage.md",
    "harness/tests/scenarios/us4-codex-adapter-coverage.md",
    "harness/tests/scenarios/us4-future-harness-adaptation.md",
    "harness/tests/scenarios/us4-configured-Proxyma-base-url.md",
    "harness/tests/scenarios/us4-missing-Proxyma-base-url.md",
    "harness/tests/scenarios/us-placeholder-command-generation.md",
    "harness/tests/scenarios/us-placeholder-no-credential-lookup.md",
    "harness/tests/scenarios/us-placeholder-no-credential-leakage.md",
    "harness/tests/scenarios/us-transcriptor-canonical-command.md",
    "harness/tests/scenarios/us-transcriptor-no-local-command.md",
    "harness/tests/scenarios/us-transcriptor-retryable-error.md"
)

foreach ($scenario in $scenarioFiles) {
    [void](Assert-FileExists -RelativePath $scenario -Label "Scenario")
}

$credentialScanRoots = @(
    "harness/examples/task-state",
    "harness/examples/task-state/summaries"
)

foreach ($root in $credentialScanRoots) {
    $scanPath = Join-Path $RepoRoot $root
    $files = @(Get-ChildItem -LiteralPath $scanPath -Recurse -File -Include "*.json", "*.md" -ErrorAction SilentlyContinue)
    foreach ($file in $files) {
        $content = Get-Content -LiteralPath $file.FullName -Raw
        foreach ($forbidden in @("ssh demo@", "demo-password")) {
            if ($content.Contains($forbidden)) {
                $label = $file.FullName.Substring($RepoRoot.Path.Length + 1)
                Add-ValidationError "$label contains resolved demo credential marker '$forbidden'"
            }
        }
    }
}

Assert-ContainsAll -RelativePath "harness/instruction-pack/proxyma-harness.md" -Label "Instruction pack" -Terms @(
    "resume first",
    "active polling",
    "Proxyma-only target access",
    "JSON task-state files are authoritative",
    "one JSON source-of-truth state file per logical client task",
    "Proxyma statuses",
    "Future Harness Adapters",
    "{user}",
    "{password}",
    "must not look up",
    'must not inspect `targets/*.json`',
    "directly SCP",
    "Do not send",
	"canonical SSH and WinRM commands",
	"Invoke-Command",
    "proxyma_base_url",
    "agent_config",
    "Authorization: Bearer",
    "token_expired",
    "proxyma-auth.md",
    "configuration error",
    "trailing slash",
    "harness/proxyma-harness-run.json",
    "proxyma-api-reference.md",
    "api-examples",
    'Do not probe `/swagger.json`, `/openapi.json`, or other undocumented endpoints',
    "explicit target",
    "GET /v1/agent/targets/resolve",
    "payload_kind",
    "directly probe the target",
    'The request body must have a `target` object',
    "only state-changing operation",
    "must never submit an approval"
)

Assert-ContainsAll -RelativePath "harness/adapters/codex/SKILL.md" -Label "Codex adapter" -Terms @(
    "authoritative behavior source",
    "Before processing new user work",
    "re-check pending Proxyma tasks every 30 seconds",
    "Do not directly SSH",
    "directly SCP",
    "{user}",
    "{password}",
    "Do not look up",
    'Do not read `targets/*.json`',
    "Do not strip transport",
    "Append multiple Proxyma commands",
    "blocked_reason",
    "target hostname or IP address",
    "proxyma_base_url",
    "agent_config",
    "Authorization: Bearer",
    "token_expired",
    "proxyma-auth.md",
    "Do not invent",
    "trailing slash",
    "harness/proxyma-harness-run.json",
    "proxyma-api-reference.md",
    "api-examples",
    "API: GET /v1/agent/targets/resolve",
    "API: POST /v1/agent/commands",
    "API: GET /v1/agent/tasks/{task_id}",
    "API: GET /v1/health",
    "canonical_command.prefix",
    "payload_kind",
    "directly probe the target",
    'ssh {user}@20.218.123.238 "ls /home"',
	'winrm {user}@win-srv-01 Get-Service',
	"Invoke-Command",
    'do not probe `/swagger.json`,',
    'explicit `target` object',
    'The request body does not contain `asset_id`',
    "only state-changing agent operation",
    "Never submit an approval"
)

Assert-ContainsAll -RelativePath "harness/instruction-pack/proxyma-api-reference.md" -Label "Instruction-pack API reference" -Terms @(
    "POST /v1/agent/commands",
    "GET /v1/agent/targets/resolve",
    "GET /v1/agent/tasks/{task_id}",
    "GET /v1/health",
    "Authorization: Bearer",
    "token_expired",
    "proxyma-auth.md",
    "202 Accepted",
    "200 OK",
    "404",
    "asset_id",
    'requires an explicit `target` object',
    "payload_kind",
    "posix_shell",
    "windows_shell",
    'ssh {user}@20.218.123.238 "ls /home"',
	'winrm {user}@win-srv-01 Get-Service',
	"Invoke-Command",
    'do not probe `/swagger.json`, `/openapi.json`',
    "Literal request-body shape",
    "api-examples",
    "commands.request.json",
    "tasks.completed.response.json",
    "health.response.json",
    "cannot answer interactions"
)

Assert-ExcludesAll -RelativePath "harness/instruction-pack/proxyma-api-reference.md" -Label "Instruction-pack API reference" -Terms @(
    "POST /v1/agent/targets/resolve",
    "POST /v1/agent/interactions"
)

Assert-ExcludesAll -RelativePath "harness/instruction-pack/proxyma-harness.md" -Label "Instruction pack" -Terms @(
    "POST /v1/agent/targets/resolve",
    "POST /v1/agent/interactions"
)

Assert-ExcludesAll -RelativePath "harness/adapters/codex/SKILL.md" -Label "Codex adapter" -Terms @(
    "POST /v1/agent/targets/resolve",
    "POST /v1/agent/interactions"
)

Assert-ContainsAll -RelativePath "harness/tests/README.md" -Label "Validation README" -Terms @(
    "Unit-level validation",
    "Integration-level validation",
    "End-to-end validation"
)

if ($Errors.Count -gt 0) {
    if (-not $Quiet) {
        Write-Host "Proxyma harness asset validation failed:"
        foreach ($errorMessage in $Errors) {
            Write-Host "- $errorMessage"
        }
    }
    exit 1
}

if (-not $Quiet) {
    Write-Host "Proxyma harness asset validation passed."
    Write-Host "Checked schemas, task-state examples, scenario files, instruction-pack rules, and Codex adapter coverage."
}

exit 0
