param(
    [string]$TargetOS = $env:GOOS,
    [string]$TargetArch = $env:GOARCH,
    [string]$OutDir,
    [string]$Version = $env:PROXYMA_VERSION,
    [string]$Commit = $env:PROXYMA_COMMIT
)
$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ([string]::IsNullOrWhiteSpace($TargetOS)) { $TargetOS = & go env GOOS }
if ([string]::IsNullOrWhiteSpace($TargetArch)) { $TargetArch = & go env GOARCH }
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $Root "dist" }
if ([string]::IsNullOrWhiteSpace($Version)) { $Version = & git -C $Root describe --tags --long --always --dirty }
if ([string]::IsNullOrWhiteSpace($Commit)) { $Commit = & git -C $Root rev-parse --short=7 HEAD }
$extension = if ($TargetOS -eq "windows") { ".exe" } else { "" }
$output = Join-Path $OutDir "proxyma-agent-$TargetOS-$TargetArch$extension"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$oldOS, $oldArch, $oldCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
try {
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    $env:CGO_ENABLED = "0"
    & go build -trimpath -ldflags "-s -w -X proxyma/internal/buildinfo.rawVersion=$Version -X proxyma/internal/buildinfo.rawCommit=$Commit" -o $output ./cmd/proxyma-agent
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    $env:GOOS = $oldOS
    $env:GOARCH = $oldArch
    $env:CGO_ENABLED = $oldCGO
}
Write-Host "Built $output"
