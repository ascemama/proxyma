[CmdletBinding()]
param(
    [switch]$Yes
)

$ErrorActionPreference = "Stop"
$RepositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

if (-not (Test-Path -LiteralPath (Join-Path $RepositoryRoot ".git") -PathType Container) -or
    -not (Test-Path -LiteralPath (Join-Path $RepositoryRoot "go.mod") -PathType Leaf) -or
    -not (Test-Path -LiteralPath (Join-Path $RepositoryRoot "frontend\package.json") -PathType Leaf)) {
    throw "Refusing to clean an unverified Proxyma repository root: $RepositoryRoot"
}

$CleanupPaths = @(
    "bin", "dist", "build", "internal/web/dist",
    "frontend",
    ".gocache", ".gotmp", ".tmp", "var", "proxyma.config.json",
    "asset", "targets", "target", "policies", "users", "agents", "secrets", "interactions",
    "task-state", "harness/task-state", "harness/agent-configuration",
    "coverage.out", "coverage.html", "*.coverprofile", "*.prof", "*.pprof",
    "*.test", "*.out", "*.log"
)

Write-Host "Ignored files under these repository paths will be removed:"
$CleanupPaths | ForEach-Object { Write-Host "  $_" }
Write-Host "Tracked files, the repository, Go, Node.js, npm, and global caches will be retained."

if (-not $Yes) {
    Write-Host "Re-run with -Yes to continue."
    exit 2
}

& git -C $RepositoryRoot clean -fdX -- @CleanupPaths
if ($LASTEXITCODE -ne 0) {
    throw "git clean failed with exit code $LASTEXITCODE"
}

Write-Host "Removed local Proxyma development artifacts."
