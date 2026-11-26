# Installing `proxyma-client` from a release

`proxyma-client` is the deterministic client used by Codex or another AI coding
agent to work through Proxyma. It owns authentication, target resolution,
idempotent command submission, polling, and durable client task state.

The Proxyma server and `proxyma-client` normally run on different machines:

- install the Proxyma server on the managed server host using one of the
  [server installation guides](proxyma-server-installation.md); and
- install `proxyma-client`, its matching harness, and the model adapter on the
  workstation where Codex or Claude Code runs.

The standalone server installers remain server-only. Every GitHub release
publishes the client binaries, the optional `vm-workflow` binaries, and
`proxyma-client-harness.tar.gz` together so the client executable, workflow
binary, instruction pack, schemas, and adapters can come from the same release.

## Supported client platforms

| Platform | AMD64 asset | ARM64 asset | Installer |
|---|---|---|---|
| Linux | `proxyma-client-linux-amd64` | `proxyma-client-linux-arm64` | `install-proxyma-client.sh` |
| macOS | `proxyma-client-darwin-amd64` | `proxyma-client-darwin-arm64` | `install-proxyma-client.sh` |
| Windows | `proxyma-client-windows-amd64.exe` | `proxyma-client-windows-arm64.exe` | `install-proxyma-client.ps1` |

The optional vulnerability-management workflow has matching standalone
artifacts:

| Platform | AMD64 asset | ARM64 asset |
|---|---|---|
| Linux | `vm-workflow-linux-amd64` | `vm-workflow-linux-arm64` |
| macOS | `vm-workflow-darwin-amd64` | `vm-workflow-darwin-arm64` |
| Windows | `vm-workflow-windows-amd64.exe` | `vm-workflow-windows-arm64.exe` |

The POSIX installer requires `curl`, `tar`, `mktemp`, and either `sha256sum` or
`shasum`. The Windows installer supports Windows PowerShell 5.1 and PowerShell
7 and requires `Get-FileHash` and the `tar.exe` included with supported Windows
versions.

Before configuring the harness, obtain the UUID and JWT signing key of a
Proxyma agent authorized for the required targets. Do not put that signing key
in a prompt, installer argument, command history, or release download.

This is the recommended path for production. The installer downloads the
platform-specific executable, the matching harness archive, and the checksum
manifest from one GitHub release, verifies them, and installs them for the
current user and project.

### Install the latest stable release

Download the installer as a file so it can be inspected before execution; do
not pipe a remote script directly into a shell. Always choose a harness type
when installing a new project. In every example, `PROJECT_DIR` is the harness
working directory and receives the shared `harness/` runtime.

#### Codex

Linux or macOS:

```bash
PROJECT_DIR="$HOME/projects/my-proxyma-project"
curl -fL -o install-proxyma-client.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.sh
bash ./install-proxyma-client.sh \
  --harness-type codex \
  --project-dir "$PROJECT_DIR"
```

Windows PowerShell:

```powershell
$ProjectDir = Join-Path $HOME "projects\my-proxyma-project"
Invoke-WebRequest `
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.ps1 `
  -OutFile .\install-proxyma-client.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 `
  -HarnessType Codex `
  -ProjectDir $ProjectDir
```

#### Claude Code

Use the same commands, replacing `codex` with `claude` and `Codex` with
`Claude`:

```bash
bash ./install-proxyma-client.sh \
  --harness-type claude \
  --project-dir "$PROJECT_DIR"
```

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 `
  -HarnessType Claude `
  -ProjectDir $ProjectDir
```

#### Generic harness

The generic adapter requires both a skill discovery directory and a project
directory. The installer creates `proxyma-harness/SKILL.md` below
`SKILL_DIR` and creates `harness/` below `PROJECT_DIR`.

Linux or macOS:

```bash
PROJECT_DIR="$HOME/projects/my-proxyma-project"
SKILL_DIR="$HOME/.config/proxyma/skills"
bash ./install-proxyma-client.sh \
  --harness-type generic \
  --skill-dir "$SKILL_DIR" \
  --project-dir "$PROJECT_DIR"
```

Windows PowerShell:

```powershell
$ProjectDir = Join-Path $HOME "projects\my-proxyma-project"
$SkillDir = Join-Path $HOME ".config\proxyma\skills"
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 `
  -HarnessType Generic `
  -SkillDir $SkillDir `
  -ProjectDir $ProjectDir
```

The default executable locations are `$HOME/.local/bin/proxyma-client` on
Linux/macOS and `%LOCALAPPDATA%\Programs\Proxyma\bin\proxyma-client.exe` on
Windows.

### Install for Hermes

Hermes uses the generic adapter. Install the skill in Hermes' global skill
directory, but keep Proxyma configuration, credentials, and task state in the
project directory used as Hermes' working directory.

On Linux or macOS:

```bash
PROJECT_DIR="$HOME/projects/my-proxyma-project"
SKILL_DIR="$HOME/.hermes/skills"
curl -fL -o install-proxyma-client.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.sh
bash ./install-proxyma-client.sh \
  --harness-type generic \
  --skill-dir "$SKILL_DIR" \
  --project-dir "$PROJECT_DIR"

cd "$PROJECT_DIR"
hermes skills trust
```

For Hermes on Windows:

```powershell
$ProjectDir = Join-Path $HOME "projects\my-proxyma-project"
$SkillDir = Join-Path $HOME ".hermes\skills"
Invoke-WebRequest `
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.ps1 `
  -OutFile .\install-proxyma-client.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 `
  -HarnessType Generic `
  -SkillDir $SkillDir `
  -ProjectDir $ProjectDir

Set-Location $ProjectDir
hermes skills trust
```

For scheduled Hermes work, use the same project directory and attach the
skill:

```bash
hermes cron create "every 1d at 09:00" \
  "Check the managed server and report issues." \
  --workdir "$PROJECT_DIR" \
  --skill proxyma-harness
```

Hermes documents `--workdir` as an absolute per-job working directory for
terminal/file operations and project-skill discovery; see the [Hermes cron
documentation](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/cron.md).

Both installers:

1. Detect the native operating system and AMD64 or ARM64 architecture.
2. Download that binary, `proxyma-client-harness.tar.gz`, and `SHA256SUMS` from
   one release. With `--with-vm-workflow`/`-WithVMWorkflow`, they also download
   the matching `vm-workflow` asset.
3. Require exactly one valid checksum entry for each downloaded payload and
   verify every SHA-256 hash.
4. Reject unsafe archive paths, links, special files, and unsafe install
   destinations.
5. Install the binary for the current user and create or refresh `harness/`
   under `--project-dir`/`-ProjectDir`.
   If `--harness-type codex` or `--harness-type claude` is supplied, install
   only that selected project adapter at the corresponding project-root skill
   path. If `generic` is selected, `--skill-dir`/`-SkillDir` is required and
   receives the neutral skill.

The installers never request or create a signing key and never modify `PATH`.
If the user-local binary directory is not already on `PATH`, follow the printed
instruction and restart Codex or Claude Code so it inherits the new environment.

By default, the client installers do not install `vm-workflow` or copy the
optional workflow assets. When `--with-vm-workflow`/`-WithVMWorkflow` is used,
the installer installs the matching `vm-workflow` executable beside
`proxyma-client` and copies the workflow assets into the project harness.
The opt-in workflow components must come from the same release as the client
executable and harness.

### Install the optional `vm-workflow`

`vm-workflow` is useful when Hermes or another harness is managing recurring
CSV/XLSX vulnerability reports. It owns the workflow database, leases, plans,
and audit state, while the existing Proxyma client configuration remains the
source of Proxyma credentials and connectivity. The workflow database,
downloaded reports, and cron runtime state are deployment data and should not
be placed in the tracked project or harness package.

The installer can perform the complete opt-in installation. For example:

```bash
bash ./install-proxyma-client.sh \
  --version v1.2.3 \
  --project-dir "$PROJECT_DIR" \
  --with-vm-workflow
```

This installs the matching binary in the same directory as `proxyma-client`
and copies `harness/workflows/vulnerability-management/` into the project.

If the binary must be downloaded manually, use the following fallback. Replace
the asset name for macOS, Windows, or ARM64, and use the same exact release as
the client executable and harness:

```bash
version=v1.2.3
asset=vm-workflow-linux-amd64
base="https://github.com/ascemama/proxyma/releases/download/$version"
bin_dir="$HOME/.local/bin"
mkdir -p "$bin_dir"
curl -fL -o "$asset" "$base/$asset"
curl -fL -o SHA256SUMS "$base/SHA256SUMS"
grep "  $asset$" SHA256SUMS | sha256sum --check
install -m 0755 "$asset" "$bin_dir/vm-workflow"
vm-workflow version
```

On Windows PowerShell:

```powershell
$Version = "v1.2.3"
$Asset = "vm-workflow-windows-amd64.exe"
$Base = "https://github.com/ascemama/proxyma/releases/download/$Version"
$BinDir = Join-Path $env:LOCALAPPDATA "Programs\Proxyma\bin"
$Downloaded = Join-Path $PWD $Asset
$Checksums = Join-Path $PWD "SHA256SUMS"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Invoke-WebRequest "$Base/$Asset" -OutFile $Downloaded
Invoke-WebRequest "$Base/SHA256SUMS" -OutFile $Checksums
$Expected = (Select-String -Path $Checksums -Pattern "  $([regex]::Escape($Asset))$").Line.Split(' ')[0]
$Actual = (Get-FileHash $Downloaded -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Expected.ToLowerInvariant() -ne $Actual) { throw "SHA-256 verification failed" }
Copy-Item $Downloaded (Join-Path $BinDir "vm-workflow.exe") -Force
& (Join-Path $BinDir "vm-workflow.exe") version
```

Install the binary in the same user-local binary directory as
`proxyma-client`. The workflow binary does not need a second credential file.
It uses the existing `harness/proxyma-client.json` and its referenced agent
configuration when `--proxyma-config` is supplied.

After installing the harness and workflow binary, initialize or inspect the
workflow database outside the repository. For example:

```bash
PROJECT_DIR="$HOME/projects/my-proxyma-project"
VM_DB="/var/lib/proxyma-client-vulnerability-management/workflow.sqlite"
PROFILE="$PROJECT_DIR/harness/workflows/vulnerability-management/profiles/nessus-detailed.json"
PROXYMA_CONFIG="$PROJECT_DIR/harness/proxyma-client.json"
sudo install -d -m 0750 "$(dirname "$VM_DB")"
sudo chown "$USER":"$(id -gn)" "$(dirname "$VM_DB")"
vm-workflow --db "$VM_DB" status
vm-workflow --db "$VM_DB" --proxyma-config "$PROXYMA_CONFIG" doctor --check-proxyma
vm-workflow --db "$VM_DB" import --profile "$PROFILE" --input /path/to/report.csv
vm-workflow --db "$VM_DB" --proxyma-config "$PROXYMA_CONFIG" reconcile-access
```

The optional workflow assets are included below
`harness/workflows/vulnerability-management/` in the harness package, but the
client installer does not copy them unless explicitly requested. To install
them into the project harness, add `--with-vm-workflow` to the POSIX installer:

```bash
bash ./install-proxyma-client.sh \
  --harness-type generic \
  --skill-dir "$SKILL_DIR" \
  --project-dir "$PROJECT_DIR" \
  --with-vm-workflow
```

On Windows PowerShell, use `-WithVMWorkflow`:

```powershell
.\install-proxyma-client.ps1 `
  -HarnessType Generic `
  -SkillDir $SkillDir `
  -ProjectDir $ProjectDir `
  -WithVMWorkflow
```

After this opt-in installation, review
the [workflow README](../harness/workflows/vulnerability-management/README.md),
[Hermes prompt](../harness/workflows/vulnerability-management/prompt.md),
[cron example](../harness/workflows/vulnerability-management/cron.example),
and [workflow documentation](vulnerability-management-workflow.md#operational-setup) before
enabling scheduled remediation. The cron example runs the static `pulse`
readiness gate and starts Hermes only when actionable work exists.

### Install a pinned release or custom location

Use the same exact release for the installer and its `--version`/`-Version`
argument. This keeps the downloaded binary and harness pinned during
reproducible provisioning or rollback.

Linux or macOS:

```bash
version=v1.2.3
curl -fL -o install-proxyma-client.sh \
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-client.sh"
bash ./install-proxyma-client.sh \
  --version "$version" \
  --harness-type codex \
  --project-dir /path/to/project \
  --bin-dir "$HOME/.local/bin"
```

Windows:

```powershell
$version = "v1.2.3"
Invoke-WebRequest `
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-client.ps1" `
  -OutFile .\install-proxyma-client.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 `
  -Version $version `
  -HarnessType Codex `
  -ProjectDir C:\path\to\project `
  -BinDir "$env:LOCALAPPDATA\Programs\Proxyma\bin"
```

Versions must be `latest` or a complete semantic version beginning with `v`,
including an exact prerelease such as `v1.2.3-rc.1`. `latest` resolves only the
latest stable GitHub release.

### Install on a machine without internet access

The client can be installed on an isolated machine from artifacts transferred
from an internet-connected staging machine. In this workflow, "offline" means
that the target machine does not need GitHub or public internet access. It still
needs network access to the Proxyma server when the client is used.

Do not run the installer with only its normal options on the isolated machine:
without local artifact arguments it tries to download the release from GitHub.
Use `--binary` and `--harness` (or `-Binary` and `-Harness` on Windows) so the
installer operates entirely on files already present on the target machine.

Use the same exact release and target architecture for every transferred
artifact. On the staging machine, download:

- the matching client binary, such as `proxyma-client-linux-amd64`,
  `proxyma-client-darwin-arm64`, or `proxyma-client-windows-amd64.exe`;
- `proxyma-client-harness.tar.gz`;
- the matching installer script; and
- `SHA256SUMS`.

Verify the binary, harness archive, and installer script against `SHA256SUMS`
before transferring them. GitHub artifact attestations can provide an
additional provenance check.
Do not put agent credentials or a JWT signing key in the transfer bundle.

For example, on a Linux staging machine, prepare a bundle for release `v1.2.3`:

```bash
version=v1.2.3
asset=proxyma-client-linux-amd64
base="https://github.com/ascemama/proxyma/releases/download/$version"
mkdir -p proxyma-client-offline
curl -fL -o "proxyma-client-offline/$asset" "$base/$asset"
curl -fL -o proxyma-client-offline/proxyma-client-harness.tar.gz \
  "$base/proxyma-client-harness.tar.gz"
curl -fL -o proxyma-client-offline/install-proxyma-client.sh \
  "$base/install-proxyma-client.sh"
curl -fL -o proxyma-client-offline/SHA256SUMS "$base/SHA256SUMS"
(
  cd proxyma-client-offline
  grep "  $asset$" SHA256SUMS | sha256sum --check
  grep '  proxyma-client-harness.tar.gz$' SHA256SUMS | sha256sum --check
  grep '  install-proxyma-client.sh$' SHA256SUMS | sha256sum --check
)
```

Transfer the verified `proxyma-client-offline` directory to the target machine
using approved removable media or an internal transfer method, then use the
local paths shown below. Keep `SHA256SUMS` with the bundle if the target's
operators need to repeat or audit the verification; the local-artifact
installer itself does not need it because it does not contact GitHub or perform
installer-side checksum verification.

On Linux or macOS, provide `--binary` and `--harness` together. These options
select local-artifact mode and avoid all network access by the installer:

```bash
PROXYMA_CLIENT_OS=linux \
PROXYMA_CLIENT_ARCH=amd64 \
bash /media/proxyma/install-proxyma-client.sh \
  --binary /media/proxyma/proxyma-client-linux-amd64 \
  --harness /media/proxyma/proxyma-client-harness.tar.gz \
  --harness-type codex \
  --project-dir /path/to/project \
  --bin-dir "$HOME/.local/bin"
```

Use `PROXYMA_CLIENT_OS=darwin` and the matching `PROXYMA_CLIENT_ARCH` for
macOS. The local POSIX installation requires `tar` and `mktemp`; it does not
require `curl` or `sha256sum`/`shasum`.

On Windows, use the transferred PowerShell installer and matching local
artifacts:

```powershell
$env:PROXYMA_CLIENT_OS = "windows"
$env:PROXYMA_CLIENT_ARCH = "amd64"

& E:\proxyma\install-proxyma-client.ps1 `
  -Binary E:\proxyma\proxyma-client-windows-amd64.exe `
  -Harness E:\proxyma\proxyma-client-harness.tar.gz `
  -HarnessType Codex `
  -ProjectDir C:\path\to\project `
  -BinDir "$env:LOCALAPPDATA\Programs\Proxyma\bin"
```

The Windows local installation requires Windows PowerShell 5.1 or PowerShell
7 and the supported `tar.exe`; it does not require GitHub access. The
`-Binary` and `-Harness` options must be supplied together.

After installation, configure `harness/proxyma-client.json` with the reachable
Proxyma server URL. Create the local agent configuration from
`harness/agent-configuration/agent.example.json` and fill in the authorized
agent UUID and JWT signing key. Keep that key only in the target machine's
local configuration and protect the file with the appropriate filesystem
permissions.

Validate the local installation without contacting the server:

```bash
proxyma-client version
```

`proxyma-client doctor` additionally checks connectivity to the configured
Proxyma server, so it requires the target machine to be able to reach that
server. The installer preserves the existing
`harness/proxyma-client.json`, `harness/agent-configuration/`, and
`harness/task-state/` paths when a transferred release is used to update or
replace an existing installation.

Maintainers who build or publish releases should follow the
[release artifact testing guide](release-testing.md). It documents the
GitHub Actions gates, release candidate workflow, protected stable publication,
post-publication verification, and private/local staging procedure.

### Verify the downloaded artifacts manually (optional)

If the installer cannot be used, download the binary for the client platform,
`proxyma-client-harness.tar.gz`, and `SHA256SUMS` from the same exact release.
Require an exact filename match and verify both payloads before extraction or
installation. For example on Linux:

```bash
version=v1.2.3
asset=proxyma-client-linux-amd64
base="https://github.com/ascemama/proxyma/releases/download/$version"
curl -fLO "$base/$asset"
curl -fLO "$base/proxyma-client-harness.tar.gz"
curl -fLO "$base/SHA256SUMS"
grep "  $asset$" SHA256SUMS | sha256sum --check
grep '  proxyma-client-harness.tar.gz$' SHA256SUMS | sha256sum --check
```

GitHub artifact attestations provide an additional provenance check:

```bash
gh attestation verify "$asset" --repo ascemama/proxyma
gh attestation verify proxyma-client-harness.tar.gz --repo ascemama/proxyma
```

SHA-256 verification remains mandatory; attestation verification is an
additional check that GitHub Actions produced the asset for this repository.

## Installed project layout

After a new installation, the project contains:

```text
your-project/
├── .agents/                    Created when `codex` is selected
│   └── skills/proxyma-harness/SKILL.md
├── .claude/                    Created when `claude` is selected
│   └── skills/proxyma-harness/SKILL.md
└── harness/
    ├── .agents/                 Release copy of the Codex adapter
    ├── .claude/                 Claude Code adapter
    ├── generic/                 Neutral adapter source
    ├── agent-configuration/     Local credentials and non-secret template
    │   └── agent.example.json   Copy this before creating agent.json
    ├── examples/                Configuration and task examples
    ├── instruction-pack/        Harness policy, CLI contract, schemas, and examples
    ├── workflows/               Optional vulnerability-management profiles and prompt
    ├── task-state/              CLI-owned durable task state
    ├── tests/                   Validation material and scenarios
    ├── proxyma-client.json       Local harness configuration
    └── RELEASE                  Harness release version and commit
```

The `harness/` directory is the shared runtime root and belongs under the
project directory where the harness works. A skill directory is separate: it
is the parent discovery directory into which the selected skill is installed.
Codex discovers the project-root `.agents/skills/proxyma-harness/SKILL.md`
adapter, Claude Code discovers `.claude/skills/proxyma-harness/SKILL.md`, and a
generic harness receives `proxyma-harness/SKILL.md` below the explicit
`--skill-dir` destination.

Only the adapter selected with `--harness-type` is installed automatically.

If the option is omitted, copy the desired adapter from the shared harness
after installation:

```bash
# Codex
mkdir -p .agents/skills/proxyma-harness
cp harness/.agents/skills/proxyma-harness/SKILL.md \
  .agents/skills/proxyma-harness/SKILL.md

# Claude Code
mkdir -p .claude/skills/proxyma-harness
cp harness/.claude/skills/proxyma-harness/SKILL.md \
  .claude/skills/proxyma-harness/SKILL.md

# Generic harness
mkdir -p "$HOME/.hermes/skills/proxyma-harness"
cp harness/generic/skills/proxyma-harness/SKILL.md \
  "$HOME/.hermes/skills/proxyma-harness/SKILL.md"
```

On Windows PowerShell, create the corresponding directory and use
`Copy-Item` for the same source and destination paths. Restart Codex or Claude
Code after installing or copying the adapter.

## Update and rollback

Update by downloading the installer for the release you want and running it
again from the same project directory. The installer replaces the executable
and release-owned harness material transactionally. It preserves these local,
potentially sensitive paths:

- `harness/proxyma-client.json`;
- `harness/agent-configuration/`; and
- `harness/task-state/`.

Instructions, schemas, examples, tests, bundled adapters, `RELEASE`, and the
selected project adapter is refreshed from the selected release. The installer
refuses symbolic links or reparse points at managed destinations instead of
following them.

Back up the three preserved paths before a downgrade. Harness v1 state is not
compatible with harness v2 and is not migrated.

To update to the latest stable release on Linux or macOS:

```bash
curl -fL -o install-proxyma-client.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.sh
bash ./install-proxyma-client.sh
```

To update to an exact release, use the matching installer and version in the
same command. Keep any non-default `--project-dir` and `--bin-dir` options you
used during the original installation:

```bash
version=v1.2.3
curl -fL -o install-proxyma-client.sh \
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-client.sh"
bash ./install-proxyma-client.sh --version "$version"
```

On Windows, download the matching PowerShell installer and run it with
`-Version` for an exact release:

```powershell
$version = "v1.2.3"
Invoke-WebRequest `
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-client.ps1" `
  -OutFile .\install-proxyma-client.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1 -Version $version
```

Run `proxyma-client version` and `proxyma-client doctor` after an update. To
roll back, repeat the same procedure with the previous release version after
backing up the preserved configuration, credentials, and task state.

## Remove the client installation

The release package does not install a system service or provide a separate
uninstaller. Remove the binary, the project harness, and any project adapter
manually. First stop Codex or Claude Code and make sure no other
project still uses this harness. The following commands remove the local
credentials and durable task state as well as the installed release files.

On Linux or macOS, run from the project directory used during installation:

```bash
rm -f "$HOME/.local/bin/proxyma-client"
rm -rf ./harness ./.agents/skills/proxyma-harness ./.claude/skills/proxyma-harness
```

If a custom `--bin-dir` was used, remove `proxyma-client` from that directory
instead. If `--project-dir` pointed elsewhere, run the project-directory
command there. Remove the installer download separately when it is no longer
needed:

```bash
rm -f ./install-proxyma-client.sh
```

On Windows, run from the installed project directory in PowerShell:

```powershell
Remove-Item -LiteralPath "$env:LOCALAPPDATA\Programs\Proxyma\bin\proxyma-client.exe" -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath ".\harness", ".\.agents\skills\proxyma-harness", ".\.claude\skills\proxyma-harness" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath ".\install-proxyma-client.ps1" -Force -ErrorAction SilentlyContinue
```

If a custom `-BinDir` or `-ProjectDir` was used, substitute those locations.
Do not remove `harness/` or its credential and task-state directories when the
project is still used by another Proxyma client installation; update that
installation instead.

## Configure the harness

### Set the Proxyma server URL

The server URL is configured in `harness/proxyma-client.json` on the
workstation where `proxyma-client` runs. It must point to the running Proxyma
server that this workstation can reach; it is not the URL of the agent
machine. For a local development server, this may be
`http://127.0.0.1:8080`. For a remote server, use its complete HTTPS URL.

Edit `harness/proxyma-client.json`:

```json
{
  "schema_version": "2",
  "proxyma_base_url": "https://proxyma.example",
  "agent_config": "agent-configuration/agent.json",
  "state_dir": "task-state",
  "http_timeout_seconds": 15,
  "lock_timeout_seconds": 5,
  "active_poll_interval_seconds": 30
}
```

Set the optional harness identity in the same file when the default adapter
name is not specific enough for your logs:

```json
"harness_name": "hermes-nightly"
```

The adapter passes this value as `harness_name` in every client submission, and
the Proxyma server records it in task, interaction, audit, and log output. If
the property is absent, the Codex, Claude Code, and generic adapters use
`codex`, `claude`, and `default` respectively. Never put credentials or other
configuration values in the harness name or in a prompt.

Replace the example `proxyma_base_url` value with that server URL. Do this
before testing the agent or running `proxyma-client doctor`; the agent cannot
authenticate or contact Proxyma until this value is correct. Relative
`agent_config` and `state_dir` paths resolve from the directory that contains
`proxyma-client.json`.

### Add the agent credentials

Create the referenced credential file locally from the packaged example:

```bash
cp ./harness/agent-configuration/agent.example.json \
   ./harness/agent-configuration/agent.json
```

Edit `harness/agent-configuration/agent.json` and replace both placeholders:

```json
{
  "agent_id": "YOUR-PROXYMA-AGENT-UUID",
  "agent_jwt_signing_key": "YOUR-AGENT-JWT-SIGNING-KEY"
}
```

The example file is safe to distribute because it contains placeholders only.
The real `agent.json` is never included in a package and is preserved during
installer upgrades.

Restrict it to the user who runs Codex. On Linux or macOS:

```bash
chmod 700 ./harness/agent-configuration
chmod 600 ./harness/agent-configuration/agent.json
```

On Windows, remove inherited access and grant the current user access through
the Security tab or an equivalent managed endpoint policy. For example:

```powershell
icacls .\harness\agent-configuration\agent.json /inheritance:r `
  /grant:r "$($env:USERNAME):(R,W)"
```

For injected-secret environments, store only `agent_id` in the file and set
`PROXYMA_AGENT_JWT_SIGNING_KEY` through the operating system, secret manager, or
process launcher before starting Codex. The environment value overrides a key
in the file. Never pass the key to the installer or place it in a prompt.

Add local credentials and state to the containing project's ignore rules:

```gitignore
/harness/agent-configuration/*.json
/harness/task-state/active/
/harness/task-state/blocked/
/harness/task-state/closed/
/harness/task-state/summaries/
/harness/task-state/.journal/
/harness/task-state/.proxyma-client.lock
```

## Validate the installation

Open a new terminal after any `PATH` change, then run from the project:

```bash
proxyma-client version
proxyma-client doctor
proxyma-client resume
```

`doctor` validates configuration, credentials, state locking, server health,
and the required `agent_command_idempotency_v1` server capability. `resume`
recovers interrupted local work and polls existing non-final operations once.

Optionally resolve a target without submitting a command:

```bash
proxyma-client inspect --target server.example
```

Before choosing the first command for a target, retrieve its current policy
guidance. The response is target-authorized and contains only blacklist and
whitelist templates:

```bash
proxyma-client policy --target server.example
```

This guidance is provided because an agent can often accomplish the same goal
with multiple command forms. It helps the harness choose a command matching a
pre-approved whitelist template and avoid forms covered by the blacklist. It is
not an authorization decision; Proxyma checks every submitted command against
the current server-side policy.

Reuse that guidance for the target during the current harness session. Fetch it
again after restarting the harness. Proxyma still evaluates every submitted
command against the current server-side policy.

Invoke the installed project skill explicitly on the first run:

```text
$proxyma-harness Run proxyma-client doctor and resume, then inspect the Proxyma
target server.example. Do not submit a command yet.
```

Task JSON under `harness/task-state/active/`, `blocked/`, and `closed/` is authoritative and CLI-owned. Do
not edit it manually.

## Configuration discovery and troubleshooting

The CLI selects configuration in this order:

1. `--config <path>` before the command name;
2. `PROXYMA_HARNESS_CONFIG`;
3. an upward search for `harness/proxyma-client.json`.

Common failures:

- **`proxyma-client` is not found:** add the printed user-local binary directory
  to `PATH`, restart Codex or Claude Code, and open a new terminal.
- **The skill is unavailable:** verify the selected adapter exists at
  `<project>/.agents/skills/proxyma-harness/SKILL.md` for Codex or
  `<project>/.claude/skills/proxyma-harness/SKILL.md` for Claude Code, then
  restart that harness.
- **Checksum or archive validation fails:** do not install the download. Remove
  it and retry from the same release; report persistent failures.
- **The installer refuses an existing path:** remove the unexpected symlink,
  reparse point, non-regular file, or stale `.proxyma-*` transaction path only
  after confirming it is safe.
- **`config_not_found`:** run inside the project or select the configuration
  with `--config` or `PROXYMA_HARNESS_CONFIG`.
- **`server_capability_missing`:** upgrade the Proxyma server.
- **Authentication failure:** verify the agent UUID, signing key, server time,
  and that the secret environment was inherited when Codex started.
- **`submission_unknown`:** run `proxyma-client resume`; never create a
  replacement submission.

The CLI reduces accidental secret exposure but is not a security boundary
against another unrestricted process running as the same operating-system user.
