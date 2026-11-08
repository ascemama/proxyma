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
publishes the client binaries and `proxyma-client-harness.tar.gz` together so the
client executable, instruction pack, schemas, and adapters come from the same
release.

## Supported client platforms

| Platform | AMD64 asset | ARM64 asset | Installer |
|---|---|---|---|
| Linux | `proxyma-client-linux-amd64` | `proxyma-client-linux-arm64` | `install-proxyma-client.sh` |
| macOS | `proxyma-client-darwin-amd64` | `proxyma-client-darwin-arm64` | `install-proxyma-client.sh` |
| Windows | `proxyma-client-windows-amd64.exe` | `proxyma-client-windows-arm64.exe` | `install-proxyma-client.ps1` |

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

Run the installer from the root of the project where Codex or Claude Code will
work. Download it as a file so it can be inspected before execution; do not
pipe a remote script directly into a shell.

### Linux or macOS

```bash
curl -fL -o install-proxyma-client.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.sh
bash ./install-proxyma-client.sh
# Add --harness-type codex or --harness-type claude to install the project adapter.
```

The default executable location is `$HOME/.local/bin/proxyma-client`.

### Windows

```powershell
Invoke-WebRequest `
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-client.ps1 `
  -OutFile .\install-proxyma-client.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-client.ps1
# Add -HarnessType Codex or -HarnessType Claude to install the project adapter.
```

The default executable location is
`%LOCALAPPDATA%\Programs\Proxyma\bin\proxyma-client.exe`.

Both installers:

1. Detect the native operating system and AMD64 or ARM64 architecture.
2. Download that binary, `proxyma-client-harness.tar.gz`, and `SHA256SUMS` from
   one release.
3. Require exactly one valid checksum entry for each downloaded payload and
   verify both SHA-256 hashes.
4. Reject unsafe archive paths, links, special files, and unsafe install
   destinations.
5. Install the binary for the current user and create or refresh `./harness`.
   If `--harness-type codex` or `--harness-type claude` is supplied, install
   only that selected project adapter at the corresponding root skill path.

The installers never request or create a signing key and never modify `PATH`.
If the user-local binary directory is not already on `PATH`, follow the printed
instruction and restart Codex or Claude Code so it inherits the new environment.

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

Use the same exact release and target architecture for every transferred
artifact. On the staging machine, download:

- the matching client binary, such as `proxyma-client-linux-amd64`,
  `proxyma-client-darwin-arm64`, or `proxyma-client-windows-amd64.exe`;
- `proxyma-client-harness.tar.gz`;
- the matching installer script; and
- `SHA256SUMS`.

Verify the binary and harness archive against `SHA256SUMS` before transferring
them. GitHub artifact attestations can provide an additional provenance check.
Do not put agent credentials or a JWT signing key in the transfer bundle.

Transfer the verified binary, harness archive, and installer script to the
target machine using approved removable media or an internal transfer method.
The checksum manifest does not need to be transferred because local-artifact
mode does not contact GitHub or perform installer-side checksum verification;
integrity verification must happen before transfer.

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
    ├── agent-configuration/     Local credentials and non-secret template
    │   └── agent.example.json   Copy this before creating agent.json
    ├── examples/                Configuration and task examples
    ├── instruction-pack/        Harness policy, CLI contract, schemas, and examples
    ├── task-state/              CLI-owned durable task state
    ├── tests/                   Validation material and scenarios
    ├── proxyma-client.json       Local harness configuration
    └── RELEASE                  Harness release version and commit
```

The `harness/` directory is the shared runtime root. Codex discovers the
project-root `.agents/skills/proxyma-harness/SKILL.md` adapter, while Claude
Code discovers `.claude/skills/proxyma-harness/SKILL.md`. Only the adapter
selected with `--harness-type` is installed automatically.

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
