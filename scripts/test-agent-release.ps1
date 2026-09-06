[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$ReleaseBaseUrl = "https://github.com/ascemama/proxyma"
)

$ErrorActionPreference = "Stop"
$ReleaseBaseUrl = $ReleaseBaseUrl.TrimEnd('/')
$ReleaseUrl = "$ReleaseBaseUrl/releases/download/$Version"
$TestRoot = Join-Path ([IO.Path]::GetTempPath()) ("proxyma-agent-release-" + [Guid]::NewGuid().ToString("N"))
$ProjectDir = Join-Path $TestRoot "project"
$BinDir = Join-Path $TestRoot "bin"
$Installer = Join-Path $TestRoot "install-proxyma-agent.ps1"
$oldBaseUrl = $env:PROXYMA_RELEASE_BASE_URL

function Copy-ReleaseFile([string]$Source, [string]$Destination) {
    $uri = [Uri]$Source
    if ($uri.IsFile) {
        Copy-Item -LiteralPath $uri.LocalPath -Destination $Destination
        return
    }
    for ($attempt = 1; $attempt -le 5; $attempt++) {
        try {
            Invoke-WebRequest -Uri $uri -OutFile $Destination -UseBasicParsing
            return
        }
        catch {
            if ($attempt -eq 5) { throw }
            Start-Sleep -Seconds 2
        }
    }
}

try {
    New-Item -ItemType Directory -Force -Path $ProjectDir | Out-Null
    Copy-ReleaseFile "$ReleaseUrl/install-proxyma-agent.ps1" $Installer

    $env:PROXYMA_RELEASE_BASE_URL = $ReleaseBaseUrl
    & $Installer `
        -Version $Version `
        -ProjectDir $ProjectDir `
        -BinDir $BinDir

    $installedBinary = Join-Path $BinDir "proxyma-agent.exe"
    $releaseFile = Join-Path $ProjectDir "harness\RELEASE"
    $configFile = Join-Path $ProjectDir "harness\proxyma-agent.json"
    $stateFile = Join-Path $ProjectDir "harness\task-state\active\task.json"
    $skillFile = Join-Path $ProjectDir ".agents\skills\proxyma-harness\SKILL.md"
    if (-not (Test-Path -LiteralPath $installedBinary -PathType Leaf)) { throw "Installed binary is missing." }
    if (-not (Test-Path -LiteralPath $releaseFile -PathType Leaf)) { throw "Harness RELEASE file is missing." }
    if (-not (Test-Path -LiteralPath $configFile -PathType Leaf)) { throw "Harness configuration is missing." }
    if (-not (Test-Path -LiteralPath $skillFile -PathType Leaf)) { throw "Codex adapter is missing." }

    $versionOutput = & $installedBinary version | Out-String
    if ($versionOutput -notmatch [regex]::Escape($Version)) { throw "Installed binary reported the wrong version." }
    if ((Get-Content -LiteralPath $releaseFile -Raw).Trim() -notmatch [regex]::Escape("version=$Version")) {
        throw "Harness RELEASE file reported the wrong version."
    }

    Set-Content -LiteralPath $configFile -Value "preserved-config"
    Set-Content -LiteralPath $stateFile -Value "preserved-state"

    & $Installer `
        -Version $Version `
        -ProjectDir $ProjectDir `
        -BinDir $BinDir | Out-Null

    if ((Get-Content -LiteralPath $configFile -Raw).Trim() -ne "preserved-config") { throw "Configuration was not preserved." }
    if ((Get-Content -LiteralPath $stateFile -Raw).Trim() -ne "preserved-state") { throw "Task state was not preserved." }
    Write-Host "Agent release smoke test passed for $Version on Windows."
}
finally {
    if ($null -eq $oldBaseUrl) { Remove-Item Env:PROXYMA_RELEASE_BASE_URL -ErrorAction SilentlyContinue }
    else { $env:PROXYMA_RELEASE_BASE_URL = $oldBaseUrl }
    if (Test-Path -LiteralPath $TestRoot) { Remove-Item -LiteralPath $TestRoot -Recurse -Force }
}
