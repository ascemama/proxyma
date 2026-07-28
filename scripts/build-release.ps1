param(
    [string] $TargetOS = $env:GOOS,
    [string] $TargetArch = $env:GOARCH,
    [string] $CgoEnabled = $env:CGO_ENABLED,
    [string] $BuildVersion = $env:PROXYMA_VERSION,
    [string] $BuildCommit = $env:PROXYMA_COMMIT,
    [string] $OutDir = ""
)

$ErrorActionPreference = "Stop"

function Invoke-Native {
    param(
        [string] $FilePath,
        [string[]] $ArgumentList
    )

    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath exited with code $LASTEXITCODE"
    }
}

function Get-NativeOutput {
    param(
        [string] $FilePath,
        [string[]] $ArgumentList
    )

    $output = & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath exited with code $LASTEXITCODE"
    }
    return ($output | Out-String).Trim()
}

if ([string]::IsNullOrWhiteSpace($TargetOS)) {
    $TargetOS = "linux"
}
if ([string]::IsNullOrWhiteSpace($TargetArch)) {
    $TargetArch = "amd64"
}
if ([string]::IsNullOrWhiteSpace($CgoEnabled)) {
    $CgoEnabled = "0"
}

$Root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if ([string]::IsNullOrWhiteSpace($BuildVersion)) {
    $BuildVersion = Get-NativeOutput "git" @("-C", $Root, "describe", "--tags", "--long", "--always", "--dirty")
}
if ([string]::IsNullOrWhiteSpace($BuildCommit)) {
    $BuildCommit = Get-NativeOutput "git" @("-C", $Root, "rev-parse", "--short=7", "HEAD")
}
$LdFlags = "-s -w -X proxyma/internal/buildinfo.rawVersion=$BuildVersion -X proxyma/internal/buildinfo.rawCommit=$BuildCommit"

if ([string]::IsNullOrWhiteSpace($OutDir)) {
    $OutDir = Join-Path $Root "dist"
}
$EmbedDir = Join-Path $Root "internal\web\dist"

if (Test-Path -LiteralPath $EmbedDir) {
    Remove-Item -LiteralPath $EmbedDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $EmbedDir, $OutDir | Out-Null

Push-Location (Join-Path $Root "frontend")
try {
    Invoke-Native "npm.cmd" @("ci")
    Invoke-Native "npm.cmd" @("run", "build")
}
finally {
    Pop-Location
}

Copy-Item -Path (Join-Path $Root "frontend\dist\*") -Destination $EmbedDir -Recurse -Force

$serverExt = ""
if ($TargetOS -eq "windows") {
    $serverExt = ".exe"
}

$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
try {
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    $env:CGO_ENABLED = $CgoEnabled

    Invoke-Native "go" @(
        "build",
        "-tags", "embedded_frontend",
        "-trimpath",
        "-ldflags=$LdFlags",
        "-o", (Join-Path $OutDir "Proxyma-$TargetOS-$TargetArch$serverExt"),
        "./cmd/proxyma"
    )

    Invoke-Native "go" @(
        "build",
        "-trimpath",
        "-ldflags=-s -w",
        "-o", (Join-Path $OutDir "proxymactl-$TargetOS-$TargetArch$serverExt"),
        "./cmd/proxymactl"
    )
}
finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
}

Write-Host "Built:"
Write-Host "  $(Join-Path $OutDir "Proxyma-$TargetOS-$TargetArch$serverExt")"
Write-Host "  $(Join-Path $OutDir "proxymactl-$TargetOS-$TargetArch$serverExt")"
