$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Installer = Join-Path $Root "scripts\install-proxyma-client.ps1"
$TestRoot = Join-Path ([IO.Path]::GetTempPath()) ("proxyma-client-test-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TestRoot | Out-Null

function Fail([string]$Message) { throw "FAIL: $Message" }

function New-HarnessArchive([string]$Output, [string]$Marker = "current") {
    $payload = Join-Path $TestRoot "payload"
    if (Test-Path -LiteralPath $payload) { Remove-Item -LiteralPath $payload -Recurse -Force }
    $harness = Join-Path $payload "harness"
    foreach ($relative in @(
        "instruction-pack", "examples", "tests", ".agents\skills\proxyma-harness",
        ".claude\skills\proxyma-harness", "generic\skills\proxyma-harness", "workflows\vulnerability-management", "agent-configuration", "task-state\active", "task-state\blocked", "task-state\closed", "task-state\summaries"
    )) {
        New-Item -ItemType Directory -Force -Path (Join-Path $harness $relative) | Out-Null
    }
    Set-Content -LiteralPath (Join-Path $harness "RELEASE") -Value @("name=proxyma-client-harness", "version=v1.2.3", "commit=deadbeef")
    Set-Content -LiteralPath (Join-Path $harness "proxyma-client.json") -Value '{"schema_version":"2","proxyma_base_url":"https://proxyma.example","agent_config":"agent-configuration/agent.json","state_dir":"task-state"}'
    Set-Content -LiteralPath (Join-Path $harness "agent-configuration\agent.example.json") -Value '{"agent_id":"TOBECHANGED","agent_jwt_signing_key":"TOBECHANGED"}'
    Set-Content -LiteralPath (Join-Path $harness "instruction-pack\proxyma-harness.md") -Value $Marker
    Set-Content -LiteralPath (Join-Path $harness "examples\example.txt") -Value "example"
    Set-Content -LiteralPath (Join-Path $harness "tests\test.txt") -Value "test"
    Set-Content -LiteralPath (Join-Path $harness ".agents\skills\proxyma-harness\SKILL.md") -Value $Marker
    Set-Content -LiteralPath (Join-Path $harness ".claude\skills\proxyma-harness\SKILL.md") -Value $Marker
    Set-Content -LiteralPath (Join-Path $harness "generic\skills\proxyma-harness\SKILL.md") -Value $Marker
    Set-Content -LiteralPath (Join-Path $harness "workflows\vulnerability-management\prompt.md") -Value $Marker
    & tar -czf $Output -C $payload harness
    if ($LASTEXITCODE -ne 0) { Fail "could not create harness fixture" }
}

function New-Release([string]$ReleaseDir, [string]$Arch, [string]$Marker) {
    New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null
    $binaryName = "proxyma-client-windows-$Arch.exe"
    Set-Content -LiteralPath (Join-Path $ReleaseDir $binaryName) -Value $Marker
    Copy-Item -LiteralPath (Join-Path $TestRoot "vm-workflow-fixture.exe") -Destination (Join-Path $ReleaseDir "vm-workflow-windows-$Arch.exe")
    New-HarnessArchive (Join-Path $ReleaseDir "proxyma-client-harness.tar.gz") $Marker
    $lines = foreach ($name in @($binaryName, "proxyma-client-harness.tar.gz", "vm-workflow-windows-$Arch.exe")) {
        $hash = (Get-FileHash -LiteralPath (Join-Path $ReleaseDir $name) -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $name"
    }
    Set-Content -LiteralPath (Join-Path $ReleaseDir "SHA256SUMS") -Value $lines
}

function Invoke-ExpectedFailure([scriptblock]$Action, [string]$Label) {
    try { & $Action; Fail "$Label was accepted" }
    catch {
        if ($_.Exception.Message -like "FAIL:*") { throw }
    }
}

$oldBase = $env:PROXYMA_RELEASE_BASE_URL
$oldOS = $env:PROXYMA_CLIENT_OS
$oldArch = $env:PROXYMA_CLIENT_ARCH
$originalPath = $env:PATH
$oldGoCache = $env:GOCACHE
$testGoCache = Join-Path $TestRoot "go-cache"
New-Item -ItemType Directory -Force -Path $testGoCache | Out-Null
try {
    $env:GOCACHE = $testGoCache
    $env:PROXYMA_RELEASE_BASE_URL = ([Uri]::new($TestRoot)).AbsoluteUri.TrimEnd('/')
    $env:PROXYMA_CLIENT_OS = "windows"
    $env:PROXYMA_CLIENT_ARCH = "x64"
    & go build -o (Join-Path $TestRoot "vm-workflow-fixture.exe") ./cmd/vm-workflow
    if ($LASTEXITCODE -ne 0) { Fail "could not build vm-workflow test fixture" }
    $exact = Join-Path $TestRoot "releases\download\v1.2.3"
    New-Release $exact "amd64" "first"
    $project = Join-Path $TestRoot "project"
    $bin = Join-Path $TestRoot "bin"
    New-Item -ItemType Directory -Path $project | Out-Null
    & $Installer -Version v1.2.3 -HarnessType codex -ProjectDir $project -BinDir $bin
    if ($env:PATH -ne $originalPath) { Fail "installer changed PATH" }
    if ((Get-Content -LiteralPath (Join-Path $bin "proxyma-client.exe") -Raw).Trim() -ne "first") { Fail "wrong binary was installed" }
    if (Test-Path -LiteralPath (Join-Path $bin "vm-workflow.exe")) { Fail "vm-workflow was installed without opt-in" }
    if ((Get-Content -LiteralPath (Join-Path $project "harness\instruction-pack\proxyma-harness.md") -Raw).Trim() -ne "first") { Fail "harness was not installed" }
    if (-not (Test-Path -LiteralPath (Join-Path $project ".agents\skills\proxyma-harness\SKILL.md"))) { Fail "Codex adapter was not installed" }
    if (Test-Path -LiteralPath (Join-Path $project ".claude\skills\proxyma-harness")) { Fail "Claude adapter was installed during Codex install" }
    $examplePath = Join-Path $project "harness\agent-configuration\agent.example.json"
    if (-not (Test-Path -LiteralPath $examplePath -PathType Leaf)) { Fail "installer omitted credential example" }
    if ((Get-Content -LiteralPath $examplePath -Raw) -notlike '*TOBECHANGED*') { Fail "credential example is incorrect" }
    if (Test-Path -LiteralPath (Join-Path $project "harness\workflows")) { Fail "workflow assets installed without opt-in flag" }

    Set-Content -LiteralPath (Join-Path $project "harness\proxyma-client.json") -Value "preserved-config"
    Set-Content -LiteralPath (Join-Path $project "harness\agent-configuration\agent.json") -Value "preserved-secret"
    Set-Content -LiteralPath $examplePath -Value "custom-example"
    Set-Content -LiteralPath (Join-Path $project "harness\task-state\active\task.json") -Value "preserved-state"
    New-Release $exact "amd64" "second"
    & $Installer -Version v1.2.3 -HarnessType codex -ProjectDir $project -BinDir $bin
    if ((Get-Content -LiteralPath (Join-Path $project "harness\proxyma-client.json") -Raw).Trim() -ne "preserved-config") { Fail "configuration was overwritten" }
    if ((Get-Content -LiteralPath (Join-Path $project "harness\agent-configuration\agent.json") -Raw).Trim() -ne "preserved-secret") { Fail "credential was overwritten" }
    if ((Get-Content -LiteralPath $examplePath -Raw).Trim() -ne "custom-example") { Fail "credential example was overwritten" }
    if ((Get-Content -LiteralPath (Join-Path $project "harness\task-state\active\task.json") -Raw).Trim() -ne "preserved-state") { Fail "task state was overwritten" }
    if ((Get-Content -LiteralPath (Join-Path $project "harness\instruction-pack\proxyma-harness.md") -Raw).Trim() -ne "second") { Fail "release-owned content was not refreshed" }

    $vmProject = Join-Path $TestRoot "vm-project"
    New-Item -ItemType Directory -Path $vmProject | Out-Null
    & $Installer -Version v1.2.3 -WithVMWorkflow -ProjectDir $vmProject -BinDir (Join-Path $TestRoot "vm-bin")
    $vmPrompt = Join-Path $vmProject "harness\workflows\vulnerability-management\prompt.md"
    if ((Get-Content -LiteralPath $vmPrompt -Raw).Trim() -ne "second") { Fail "opt-in workflow assets were not installed" }
    if (-not (Test-Path -LiteralPath (Join-Path $TestRoot "vm-bin\vm-workflow.exe"))) { Fail "opt-in vm-workflow was not installed" }
    Set-Content -LiteralPath $vmPrompt -Value "local-workflow"
    & $Installer -Version v1.2.3 -ProjectDir $vmProject -BinDir (Join-Path $TestRoot "vm-bin")
    if ((Get-Content -LiteralPath $vmPrompt -Raw).Trim() -ne "local-workflow") { Fail "unflagged reinstall modified workflow assets" }
    & $Installer -Version v1.2.3 -WithVMWorkflow -ProjectDir $vmProject -BinDir (Join-Path $TestRoot "vm-bin")
    if ((Get-Content -LiteralPath $vmPrompt -Raw).Trim() -ne "second") { Fail "flagged reinstall did not refresh workflow assets" }

    $env:PROXYMA_CLIENT_ARCH = "arm64"
    $latest = Join-Path $TestRoot "releases\latest\download"
    New-Release $latest "arm64" "latest"
    $latestProject = Join-Path $TestRoot "latest-project"
    New-Item -ItemType Directory -Path $latestProject | Out-Null
    & $Installer -ProjectDir $latestProject -BinDir (Join-Path $TestRoot "latest-bin")
    if ((Get-Content -LiteralPath (Join-Path $TestRoot "latest-bin\proxyma-client.exe") -Raw).Trim() -ne "latest") { Fail "ARM64 latest asset was not selected" }
    if (Test-Path -LiteralPath (Join-Path $latestProject ".agents\skills\proxyma-harness")) { Fail "Codex adapter was installed without selection" }
    if (Test-Path -LiteralPath (Join-Path $latestProject ".claude\skills\proxyma-harness")) { Fail "Claude adapter was installed without selection" }
    if (-not (Test-Path -LiteralPath (Join-Path $latestProject "harness\.agents\skills\proxyma-harness\SKILL.md"))) { Fail "manual Codex adapter source is missing" }
    if (-not (Test-Path -LiteralPath (Join-Path $latestProject "harness\.claude\skills\proxyma-harness\SKILL.md"))) { Fail "manual Claude adapter source is missing" }
    if (-not (Test-Path -LiteralPath (Join-Path $latestProject "harness\generic\skills\proxyma-harness\SKILL.md"))) { Fail "manual generic skill source is missing" }

    $claudeProject = Join-Path $TestRoot "claude-project"
    $claudeBin = Join-Path $TestRoot "claude-bin"
    New-Item -ItemType Directory -Path $claudeProject | Out-Null
    & $Installer -HarnessType claude -ProjectDir $claudeProject -BinDir $claudeBin
    if (-not (Test-Path -LiteralPath (Join-Path $claudeProject ".claude\skills\proxyma-harness\SKILL.md"))) { Fail "Claude adapter was not installed" }
    if (Test-Path -LiteralPath (Join-Path $claudeProject ".agents\skills\proxyma-harness")) { Fail "Codex adapter was installed during Claude install" }

    $genericProject = Join-Path $TestRoot "generic-project"
    $genericSkillDir = Join-Path $TestRoot "hermes\skills"
    $genericBin = Join-Path $TestRoot "generic-bin"
    New-Item -ItemType Directory -Path $genericProject | Out-Null
    & $Installer -HarnessType generic -SkillDir $genericSkillDir -ProjectDir $genericProject -BinDir $genericBin
    if (-not (Test-Path -LiteralPath (Join-Path $genericSkillDir "proxyma-harness\SKILL.md"))) { Fail "generic skill was not installed" }
    if (-not (Test-Path -LiteralPath (Join-Path $genericProject "harness\generic\skills\proxyma-harness\SKILL.md"))) { Fail "generic source skill is missing" }
    if (Test-Path -LiteralPath (Join-Path $genericProject ".agents\skills\proxyma-harness")) { Fail "generic install created the Codex adapter" }
    if (Test-Path -LiteralPath (Join-Path $genericProject ".claude\skills\proxyma-harness")) { Fail "generic install created the Claude adapter" }
    Invoke-ExpectedFailure { & $Installer -HarnessType generic -ProjectDir (Join-Path $TestRoot "missing-skill-project") -BinDir (Join-Path $TestRoot "missing-skill-bin") } "generic install accepted a missing skill directory"

    $invalidProject = Join-Path $TestRoot "invalid-project"
    Invoke-ExpectedFailure { & $Installer -HarnessType invalid -ProjectDir $invalidProject -BinDir (Join-Path $TestRoot "invalid-bin") } "invalid harness type"
    if (Test-Path -LiteralPath $invalidProject) { Fail "invalid harness type created an install directory" }

    $localRelease = Join-Path $TestRoot "local-artifacts"
    New-Release $localRelease "arm64" "local"
    $localProject = Join-Path $TestRoot "local-project"
    $localBin = Join-Path $TestRoot "local-bin"
    New-Item -ItemType Directory -Path $localProject | Out-Null
    & $Installer `
        -Binary (Join-Path $localRelease "proxyma-client-windows-arm64.exe") `
        -Harness (Join-Path $localRelease "proxyma-client-harness.tar.gz") `
        -HarnessType claude `
        -ProjectDir $localProject -BinDir $localBin
    if ((Get-Content -LiteralPath (Join-Path $localBin "proxyma-client.exe") -Raw).Trim() -ne "local") { Fail "local binary was not installed" }
    if (-not (Test-Path -LiteralPath (Join-Path $localProject ".claude\skills\proxyma-harness\SKILL.md"))) { Fail "local Claude adapter was not installed" }
    if (Test-Path -LiteralPath (Join-Path $localProject ".agents\skills\proxyma-harness")) { Fail "local install created the wrong adapter" }
    Invoke-ExpectedFailure { & $Installer -Binary (Join-Path $localRelease "proxyma-client-windows-arm64.exe") -ProjectDir $localProject -BinDir $localBin } "local mode accepted only one artifact"
    Invoke-ExpectedFailure { & $Installer -Binary (Join-Path $localRelease "proxyma-client-windows-arm64.exe") -Harness (Join-Path $localRelease "proxyma-client-harness.tar.gz") -WithVMWorkflow -ProjectDir $localProject -BinDir $localBin } "local workflow install accepted a missing vm-workflow path"
    & $Installer -Binary (Join-Path $localRelease "proxyma-client-windows-arm64.exe") -Harness (Join-Path $localRelease "proxyma-client-harness.tar.gz") -VMWorkflow (Join-Path $localRelease "vm-workflow-windows-arm64.exe") -WithVMWorkflow -ProjectDir $localProject -BinDir $localBin
    if (-not (Test-Path -LiteralPath (Join-Path $localBin "vm-workflow.exe"))) { Fail "local vm-workflow was not installed" }

    $env:PROXYMA_CLIENT_ARCH = "amd64"
    $bad = Join-Path $TestRoot "releases\download\v1.2.4"
    New-Release $bad "amd64" "bad"
    $sums = Get-Content -LiteralPath (Join-Path $bad "SHA256SUMS")
    $sums[0] = ("0" * 64) + "  proxyma-client-windows-amd64.exe"
    Set-Content -LiteralPath (Join-Path $bad "SHA256SUMS") -Value $sums
    $badProject = Join-Path $TestRoot "bad-project"
    New-Item -ItemType Directory -Path $badProject | Out-Null
    Invoke-ExpectedFailure { & $Installer -Version v1.2.4 -ProjectDir $badProject -BinDir (Join-Path $TestRoot "bad-bin") } "checksum mismatch"

    $duplicate = Join-Path $TestRoot "releases\download\v1.2.5"
    New-Release $duplicate "amd64" "duplicate"
    $duplicateLine = (Get-Content -LiteralPath (Join-Path $duplicate "SHA256SUMS"))[0]
    Add-Content -LiteralPath (Join-Path $duplicate "SHA256SUMS") -Value $duplicateLine
    $duplicateProject = Join-Path $TestRoot "duplicate-project"
    New-Item -ItemType Directory -Path $duplicateProject | Out-Null
    Invoke-ExpectedFailure { & $Installer -Version v1.2.5 -ProjectDir $duplicateProject -BinDir (Join-Path $TestRoot "duplicate-bin") } "duplicate checksum"

    $missing = Join-Path $TestRoot "releases\download\v1.2.7"
    New-Release $missing "amd64" "missing"
    $binaryOnly = (Get-Content -LiteralPath (Join-Path $missing "SHA256SUMS")) | Where-Object { $_ -like "*proxyma-client-windows-amd64.exe" }
    Set-Content -LiteralPath (Join-Path $missing "SHA256SUMS") -Value $binaryOnly
    $missingProject = Join-Path $TestRoot "missing-project"
    New-Item -ItemType Directory -Path $missingProject | Out-Null
    Invoke-ExpectedFailure { & $Installer -Version v1.2.7 -ProjectDir $missingProject -BinDir (Join-Path $TestRoot "missing-bin") } "missing checksum"

    $unsafe = Join-Path $TestRoot "releases\download\v1.2.6"
    New-Release $unsafe "amd64" "unsafe"
    $unsafePayload = Join-Path $TestRoot "unsafe-payload"
    New-Item -ItemType Directory -Path (Join-Path $unsafePayload "unexpected") -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $unsafePayload "unexpected\file") -Value "unsafe"
    & tar -czf (Join-Path $unsafe "proxyma-client-harness.tar.gz") -C $unsafePayload unexpected
    $binaryHash = (Get-FileHash -LiteralPath (Join-Path $unsafe "proxyma-client-windows-amd64.exe") -Algorithm SHA256).Hash.ToLowerInvariant()
    $archiveHash = (Get-FileHash -LiteralPath (Join-Path $unsafe "proxyma-client-harness.tar.gz") -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -LiteralPath (Join-Path $unsafe "SHA256SUMS") -Value @("$binaryHash  proxyma-client-windows-amd64.exe", "$archiveHash  proxyma-client-harness.tar.gz")
    $unsafeProject = Join-Path $TestRoot "unsafe-project"
    New-Item -ItemType Directory -Path $unsafeProject | Out-Null
    Invoke-ExpectedFailure { & $Installer -Version v1.2.6 -ProjectDir $unsafeProject -BinDir (Join-Path $TestRoot "unsafe-bin") } "unsafe archive"

    if ($IsWindows) {
        $linkProject = Join-Path $TestRoot "link-project"
        $linkTarget = Join-Path $TestRoot "link-target"
        $linkBin = Join-Path $TestRoot "link-bin"
        New-Item -ItemType Directory -Path $linkProject, $linkTarget | Out-Null
        New-Item -ItemType Junction -Path (Join-Path $linkProject "harness") -Target $linkTarget | Out-Null
        Invoke-ExpectedFailure { & $Installer -Version v1.2.3 -ProjectDir $linkProject -BinDir $linkBin } "reparse-point harness target"
        if (Test-Path -LiteralPath (Join-Path $linkBin "proxyma-client.exe")) { Fail "binary was installed before reparse-point preflight failed" }
    }

    $env:PROXYMA_CLIENT_OS = "freebsd"
    Invoke-ExpectedFailure { & $Installer -ProjectDir $project -BinDir $bin } "unsupported operating system"
    $env:PROXYMA_CLIENT_OS = "windows"
    $env:PROXYMA_CLIENT_ARCH = "ppc64le"
    Invoke-ExpectedFailure { & $Installer -ProjectDir $project -BinDir $bin } "unsupported architecture"
    $env:PROXYMA_CLIENT_ARCH = "amd64"
    Invoke-ExpectedFailure { & $Installer -Version v1.2 -ProjectDir $project -BinDir $bin } "invalid version"

    Write-Host "Windows client installer tests passed."
}
finally {
    $env:PROXYMA_RELEASE_BASE_URL = $oldBase
    $env:PROXYMA_CLIENT_OS = $oldOS
    $env:PROXYMA_CLIENT_ARCH = $oldArch
    $env:PATH = $originalPath
    $env:GOCACHE = $oldGoCache
    if (Test-Path -LiteralPath $TestRoot) { Remove-Item -LiteralPath $TestRoot -Recurse -Force }
}
