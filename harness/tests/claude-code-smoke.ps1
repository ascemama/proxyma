param([string]$ClaudeCommand = "claude")

$ErrorActionPreference = "Stop"
if ($env:PROXYMA_CLAUDE_SMOKE -ne "1") {
    Write-Host "Skipped Claude Code smoke test; set PROXYMA_CLAUDE_SMOKE=1 to run it."
    exit 0
}
$HarnessRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$prompt = "A user task requires a command on a Proxyma-managed machine. Using the discovered Proxyma harness skill, reply with only the first CLI command you must run."
$output = & $ClaudeCommand --add-dir $HarnessRoot -p $prompt
if ($LASTEXITCODE -ne 0) { throw "Claude Code smoke invocation failed" }
if (($output -join "`n") -notmatch "proxyma-agent resume") {
    throw "Claude Code did not select resume-first behavior: $output"
}
Write-Host "Claude Code Proxyma skill smoke test passed."
