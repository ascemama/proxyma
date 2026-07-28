param(
    [string]$ConfigPath
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path $ScriptDir "..\..\agent-configuration\df9ec8bb-1a22-4181-a7c8-126ad553221d.json"
}

function ConvertTo-Base64Url {
    param([byte[]]$Bytes)
    return [Convert]::ToBase64String($Bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

$config = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($config.agent_id) -or [string]::IsNullOrWhiteSpace($config.shared_secret)) {
    throw "Agent config must include agent_id and shared_secret."
}

$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$header = @{
    alg = "HS256"
    typ = "JWT"
    kid = $config.agent_id
} | ConvertTo-Json -Compress
$payload = @{
    sub = $config.agent_id
    iat = $now
    exp = $now + 600
} | ConvertTo-Json -Compress

$encoding = [Text.Encoding]::UTF8
$encodedHeader = ConvertTo-Base64Url -Bytes $encoding.GetBytes($header)
$encodedPayload = ConvertTo-Base64Url -Bytes $encoding.GetBytes($payload)
$signingInput = "$encodedHeader.$encodedPayload"
$hmac = [Security.Cryptography.HMACSHA256]::new($encoding.GetBytes($config.shared_secret))
$signature = ConvertTo-Base64Url -Bytes $hmac.ComputeHash($encoding.GetBytes($signingInput))

"$signingInput.$signature"
