[CmdletBinding()]
param(
    [string]$Version = "latest",
    [string]$ProjectDir = (Get-Location).Path,
    [string]$BinDir = "",
    [string]$Binary = "",
    [string]$Harness = "",
    [string]$HarnessType = "",
    [string]$SkillDir = ""
)

$ErrorActionPreference = "Stop"
$HarnessType = $HarnessType.ToLowerInvariant()
if ($HarnessType -notin @("", "codex", "claude", "generic")) {
    throw "Invalid harness type '$HarnessType'. Use codex, claude, or generic."
}
if ($HarnessType -eq "generic" -and [string]::IsNullOrWhiteSpace($SkillDir)) {
    throw "-SkillDir is required when -HarnessType Generic is selected."
}
if ($HarnessType -ne "generic" -and -not [string]::IsNullOrWhiteSpace($SkillDir)) {
    throw "-SkillDir is only valid with -HarnessType Generic."
}
$RepositoryUrl = if ($env:PROXYMA_RELEASE_BASE_URL) { $env:PROXYMA_RELEASE_BASE_URL.TrimEnd('/') } else { "https://github.com/ascemama/proxyma" }
$ArchiveName = "proxyma-client-harness.tar.gz"

function Test-Version([string]$Value) {
    if ($Value -eq "latest") { return }
    if ($Value -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$') {
        throw "Invalid version '$Value'. Use latest or vMAJOR.MINOR.PATCH[-PRERELEASE]."
    }
    if ($Matches[4]) {
        foreach ($identifier in $Matches[4].TrimStart('-').Split('.')) {
            if ($identifier -match '^[0-9]+$' -and $identifier.Length -gt 1 -and $identifier.StartsWith('0')) {
                throw "Invalid numeric prerelease identifier with a leading zero: $identifier"
            }
        }
    }
}

function Assert-NotReparsePoint([string]$Path, [string]$Label) {
    if (Test-Path -LiteralPath $Path) {
        $item = Get-Item -Force -LiteralPath $Path
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.LinkType) {
            throw "$Label must not be a symbolic link or reparse point: $Path"
        }
    }
}

function Copy-ReleaseAsset([string]$Source, [string]$Destination) {
    $uri = [Uri]$Source
    if ($uri.IsFile) {
        Copy-Item -LiteralPath $uri.LocalPath -Destination $Destination
    }
    else {
        Invoke-WebRequest -Uri $uri -OutFile $Destination -UseBasicParsing
    }
}

function Get-ExpectedHash([string]$ChecksumPath, [string]$AssetName) {
    $foundHashes = @()
    foreach ($line in Get-Content -LiteralPath $ChecksumPath) {
        if ($line -match '^([0-9A-Fa-f]{64})[\t ]+\*?(.+?)\s*$' -and $Matches[2] -eq $AssetName) {
            $foundHashes += $Matches[1].ToLowerInvariant()
        }
    }
    if ($foundHashes.Count -ne 1) {
        throw "SHA256SUMS does not contain exactly one valid entry for $AssetName."
    }
    return $foundHashes[0]
}

function Confirm-Checksum([string]$ChecksumPath, [string]$AssetPath, [string]$AssetName) {
    $expected = Get-ExpectedHash $ChecksumPath $AssetName
    $actual = (Get-FileHash -LiteralPath $AssetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "SHA-256 checksum mismatch for $AssetName." }
    Write-Host "Verified SHA-256 checksum for $AssetName."
}

function Replace-InstalledPath([string]$Source, [string]$Destination) {
    Assert-NotReparsePoint $Destination "Install destination"
    $newPath = "$Destination.proxyma-new.$PID"
    $backupPath = "$Destination.proxyma-backup.$PID"
    if ((Test-Path -LiteralPath $newPath) -or (Test-Path -LiteralPath $backupPath)) {
        throw "Conflicting temporary install path for $Destination."
    }
    Copy-Item -LiteralPath $Source -Destination $newPath -Recurse
    $hadDestination = Test-Path -LiteralPath $Destination
    if ($hadDestination) { Move-Item -LiteralPath $Destination -Destination $backupPath }
    try {
        Move-Item -LiteralPath $newPath -Destination $Destination
    }
    catch {
        if ($hadDestination -and (Test-Path -LiteralPath $backupPath)) {
            Move-Item -LiteralPath $backupPath -Destination $Destination
        }
        throw
    }
    if (Test-Path -LiteralPath $backupPath) { Remove-Item -LiteralPath $backupPath -Recurse -Force }
}

$localMode = -not [string]::IsNullOrWhiteSpace($Binary) -or -not [string]::IsNullOrWhiteSpace($Harness)
if ($localMode) {
    if ([string]::IsNullOrWhiteSpace($Binary) -or [string]::IsNullOrWhiteSpace($Harness)) {
        throw "-Binary and -Harness must be supplied together."
    }
    if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) { throw "Local binary must be a regular file: $Binary" }
    if (-not (Test-Path -LiteralPath $Harness -PathType Leaf)) { throw "Local harness archive must be a regular file: $Harness" }
    Assert-NotReparsePoint $Binary "Local binary"
    Assert-NotReparsePoint $Harness "Local harness archive"
}
else {
    Test-Version $Version
}
if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA) -and [string]::IsNullOrWhiteSpace($BinDir)) {
    throw "LOCALAPPDATA is required when -BinDir is not supplied."
}
if ([string]::IsNullOrWhiteSpace($BinDir)) {
    $BinDir = Join-Path $env:LOCALAPPDATA "Programs\Proxyma\bin"
}

$targetOS = if ($env:PROXYMA_CLIENT_OS) { $env:PROXYMA_CLIENT_OS } else { "windows" }
if ($targetOS -ne "windows") { throw "Unsupported operating system: $targetOS" }
$targetArch = if ($env:PROXYMA_CLIENT_ARCH) { $env:PROXYMA_CLIENT_ARCH } else { [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant() }
switch ($targetArch.ToLowerInvariant()) {
    { $_ -in @("x64", "x86_64", "amd64") } { $targetArch = "amd64"; break }
    { $_ -in @("arm64", "aarch64") } { $targetArch = "arm64"; break }
    default { throw "Unsupported architecture: $targetArch" }
}

if (-not (Test-Path -LiteralPath $ProjectDir -PathType Container)) {
    throw "Project directory must be an existing directory: $ProjectDir"
}
Assert-NotReparsePoint $ProjectDir "Project directory"
$ProjectDir = (Resolve-Path -LiteralPath $ProjectDir).Path

if ($HarnessType -eq "generic") {
    Assert-NotReparsePoint $SkillDir "Skill directory"
    if ((Test-Path -LiteralPath $SkillDir) -and -not (Test-Path -LiteralPath $SkillDir -PathType Container)) {
        throw "Skill directory must be a directory: $SkillDir"
    }
    New-Item -ItemType Directory -Force -Path $SkillDir | Out-Null
    $SkillDir = (Resolve-Path -LiteralPath $SkillDir).Path
}

Assert-NotReparsePoint $BinDir "Binary directory"
if ((Test-Path -LiteralPath $BinDir) -and -not (Test-Path -LiteralPath $BinDir -PathType Container)) {
    throw "Binary directory must be a directory: $BinDir"
}
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$BinDir = (Resolve-Path -LiteralPath $BinDir).Path

$binaryName = "proxyma-client-windows-$targetArch.exe"
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("proxyma-client-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    $binaryPath = Join-Path $tempDir $binaryName
    $archivePath = Join-Path $tempDir $ArchiveName
    $checksumPath = Join-Path $tempDir "SHA256SUMS"
    if (-not [string]::IsNullOrWhiteSpace($Binary)) {
        Copy-Item -LiteralPath $Binary -Destination $binaryPath
        Copy-Item -LiteralPath $Harness -Destination $archivePath
        Write-Host "Installing local proxyma-client artifacts for windows/$targetArch..."
    }
    else {
        $releaseUrl = if ($Version -eq "latest") { "$RepositoryUrl/releases/latest/download" } else { "$RepositoryUrl/releases/download/$Version" }
        Write-Host "Downloading proxyma-client $Version for windows/$targetArch..."
        foreach ($assetName in @($binaryName, $ArchiveName, "SHA256SUMS")) {
            Copy-ReleaseAsset "$releaseUrl/$assetName" (Join-Path $tempDir $assetName)
        }
        Confirm-Checksum $checksumPath $binaryPath $binaryName
        Confirm-Checksum $checksumPath $archivePath $ArchiveName
    }

    $entries = @(& tar -tzf $archivePath)
    if ($LASTEXITCODE -ne 0) { throw "Unable to inspect the harness archive." }
    foreach ($entry in $entries) {
        $normalized = $entry.Replace('\', '/')
        if ($normalized -ne "harness" -and -not $normalized.StartsWith("harness/")) {
            throw "Harness archive contains an unsafe path: $entry"
        }
        if ("/$normalized/" -match '/\.\.?/') { throw "Harness archive contains path traversal: $entry" }
    }
    $verboseEntries = @(& tar -tvzf $archivePath)
    if ($LASTEXITCODE -ne 0) { throw "Unable to inspect harness archive entry types." }
    foreach ($listing in $verboseEntries) {
        if ($listing.Length -eq 0 -or $listing[0] -notin @('-', 'd')) {
            throw "Harness archive contains a link or special file."
        }
    }
    & tar -xzf $archivePath -C $tempDir
    if ($LASTEXITCODE -ne 0) { throw "Unable to extract the harness archive." }
    $payloadDir = Join-Path $tempDir "harness"
    foreach ($relative in @(
        "RELEASE",
        "proxyma-client.json",
        "instruction-pack\proxyma-harness.md",
        ".agents\skills\proxyma-harness\SKILL.md",
        ".claude\skills\proxyma-harness\SKILL.md",
        "generic\skills\proxyma-harness\SKILL.md"
    )) {
        $required = Join-Path $payloadDir $relative
        if (-not (Test-Path -LiteralPath $required -PathType Leaf)) { throw "Harness archive is missing required file: $relative" }
        Assert-NotReparsePoint $required "Harness payload file"
    }

    $installedBinary = Join-Path $BinDir "proxyma-client.exe"
    $harnessDir = Join-Path $ProjectDir "harness"
    $rootAdapterDir = ""
    $rootAdapterPath = ""
    $rootAdapterSource = ""
    if ($HarnessType -eq "codex") {
        $rootAdapterDir = Join-Path $ProjectDir ".agents\skills"
        $rootAdapterPath = Join-Path $rootAdapterDir "proxyma-harness"
        $rootAdapterSource = Join-Path $payloadDir ".agents\skills\proxyma-harness"
    }
    elseif ($HarnessType -eq "claude") {
        $rootAdapterDir = Join-Path $ProjectDir ".claude\skills"
        $rootAdapterPath = Join-Path $rootAdapterDir "proxyma-harness"
        $rootAdapterSource = Join-Path $payloadDir ".claude\skills\proxyma-harness"
    }
    elseif ($HarnessType -eq "generic") {
        $rootAdapterDir = $SkillDir
        $rootAdapterPath = Join-Path $rootAdapterDir "proxyma-harness"
        $rootAdapterSource = Join-Path $payloadDir "generic\skills\proxyma-harness"
    }
    Assert-NotReparsePoint $installedBinary "Installed binary"
    if ((Test-Path -LiteralPath $installedBinary) -and -not (Test-Path -LiteralPath $installedBinary -PathType Leaf)) {
        throw "Installed binary target must be a regular file: $installedBinary"
    }
    Assert-NotReparsePoint $harnessDir "Harness target"
    if ((Test-Path -LiteralPath $harnessDir) -and -not (Test-Path -LiteralPath $harnessDir -PathType Container)) {
        throw "Harness target must be a directory: $harnessDir"
    }
    $configPath = Join-Path $harnessDir "proxyma-client.json"
    foreach ($preserved in @(
        $configPath,
        (Join-Path $harnessDir "agent-configuration"),
        (Join-Path (Join-Path $harnessDir "agent-configuration") "agent.example.json"),
        (Join-Path $harnessDir "task-state"),
        (Join-Path $harnessDir "task-state\active"),
        (Join-Path $harnessDir "task-state\blocked"),
        (Join-Path $harnessDir "task-state\closed"),
        (Join-Path $harnessDir "task-state\summaries")
    )) {
        Assert-NotReparsePoint $preserved "Preserved harness data"
    }
    if ((Test-Path -LiteralPath $configPath) -and -not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        throw "Harness configuration must be a regular file: $configPath"
    }
    $exampleConfigPath = Join-Path $harnessDir "agent-configuration\agent.example.json"
    if ((Test-Path -LiteralPath $exampleConfigPath) -and -not (Test-Path -LiteralPath $exampleConfigPath -PathType Leaf)) {
        throw "Agent credential example must be a regular file: $exampleConfigPath"
    }
    foreach ($preservedDir in @("agent-configuration", "task-state", "task-state\active", "task-state\blocked", "task-state\closed", "task-state\summaries")) {
        $path = Join-Path $harnessDir $preservedDir
        if ((Test-Path -LiteralPath $path) -and -not (Test-Path -LiteralPath $path -PathType Container)) {
            throw "Preserved harness path must be a directory: $path"
        }
    }
    if ($HarnessType) {
        $rootAdapterParent = Split-Path -Parent $rootAdapterDir
        foreach ($directory in @($rootAdapterParent, $rootAdapterDir)) {
            Assert-NotReparsePoint $directory "Skill directory"
            if ((Test-Path -LiteralPath $directory) -and -not (Test-Path -LiteralPath $directory -PathType Container)) {
                throw "Skill directory must be a directory: $directory"
            }
        }
    }
    $ownedDestinations = @(
        (Join-Path $harnessDir "instruction-pack"),
        (Join-Path $harnessDir "examples"),
        (Join-Path $harnessDir "tests"),
        (Join-Path $harnessDir ".agents"),
        (Join-Path $harnessDir ".claude"),
        (Join-Path $harnessDir "generic"),
        (Join-Path $harnessDir "RELEASE"),
        $installedBinary
    )
    if ($HarnessType) { $ownedDestinations += $rootAdapterPath }
    foreach ($destination in $ownedDestinations) {
        Assert-NotReparsePoint $destination "Install destination"
        if ((Test-Path -LiteralPath "$destination.proxyma-new.$PID") -or (Test-Path -LiteralPath "$destination.proxyma-backup.$PID")) {
            throw "Conflicting temporary install path for $destination."
        }
    }

    Replace-InstalledPath $binaryPath $installedBinary
    New-Item -ItemType Directory -Force -Path $harnessDir | Out-Null
    if (-not (Test-Path -LiteralPath $configPath)) {
        Copy-Item -LiteralPath (Join-Path $payloadDir "proxyma-client.json") -Destination $configPath
    }
    foreach ($relativeDir in @("agent-configuration", "task-state", "task-state\active", "task-state\blocked", "task-state\closed", "task-state\summaries")) {
        New-Item -ItemType Directory -Force -Path (Join-Path $harnessDir $relativeDir) | Out-Null
    }
    if (-not (Test-Path -LiteralPath $exampleConfigPath)) {
        Copy-Item -LiteralPath (Join-Path $payloadDir "agent-configuration\agent.example.json") -Destination $exampleConfigPath
    }
    foreach ($owned in @("instruction-pack", "examples", "tests", ".agents", ".claude", "generic", "RELEASE")) {
        Replace-InstalledPath (Join-Path $payloadDir $owned) (Join-Path $harnessDir $owned)
    }
    if ($HarnessType) {
        New-Item -ItemType Directory -Force -Path $rootAdapterDir | Out-Null
        Replace-InstalledPath $rootAdapterSource $rootAdapterPath
    }

    Write-Host "Installed proxyma-client at $installedBinary"
    Write-Host "Project directory: $ProjectDir"
    Write-Host "Installed the matching harness at $harnessDir"
    if ($HarnessType -eq "codex") {
        Write-Host "Installed the Codex adapter at $rootAdapterPath"
    }
    elseif ($HarnessType -eq "claude") {
        Write-Host "Installed the Claude adapter at $rootAdapterPath"
    }
    elseif ($HarnessType -eq "generic") {
        Write-Host "Skill directory: $SkillDir"
        Write-Host "Installed the generic skill at $rootAdapterPath"
    }
    else {
        $codexSkill = Join-Path $harnessDir ".agents\skills\proxyma-harness\SKILL.md"
        $claudeSkill = Join-Path $harnessDir ".claude\skills\proxyma-harness\SKILL.md"
        $codexDestination = Join-Path $ProjectDir ".agents\skills\proxyma-harness\SKILL.md"
        $claudeDestination = Join-Path $ProjectDir ".claude\skills\proxyma-harness\SKILL.md"
        Write-Host "No project adapter selected. To enable Codex, run:"
        Write-Host "  New-Item -ItemType Directory -Force '$(Split-Path -Parent $codexDestination)'"
        Write-Host "  Copy-Item -LiteralPath '$codexSkill' -Destination '$codexDestination' -Force"
        Write-Host "To enable Claude Code, run:"
        Write-Host "  New-Item -ItemType Directory -Force '$(Split-Path -Parent $claudeDestination)'"
        Write-Host "  Copy-Item -LiteralPath '$claudeSkill' -Destination '$claudeDestination' -Force"
    }
    $pathEntries = @($env:PATH -split ';' | ForEach-Object { $_.TrimEnd('\') })
    if ($pathEntries -contains $BinDir.TrimEnd('\')) {
        Write-Host "$BinDir is already on PATH."
    }
    else {
        Write-Host "$BinDir is not on PATH. Add it to the user Path in Windows Environment Variables, then restart Codex or Claude Code."
    }
}
finally {
    if (Test-Path -LiteralPath $tempDir) { Remove-Item -LiteralPath $tempDir -Recurse -Force }
}
