# Installing `proxyma-agent` from a release

`proxyma-agent` is the deterministic client used by Codex or another AI coding
agent to work through Proxyma. It owns authentication, target resolution,
idempotent command submission, polling, and durable client task state.

The Proxyma server and `proxyma-agent` normally run on different machines:

- install the Proxyma server on the managed server host using one of the
  [deployment guides](deployment.md); and
- install `proxyma-agent`, its matching harness, and the model adapter on the
  workstation where Codex or Claude Code runs.

The standalone server installers remain server-only. Every GitHub release
publishes the agent binaries and `proxyma-agent-harness.tar.gz` together so the
client executable, instruction pack, schemas, and adapters come from the same
release.

## Supported client platforms

| Platform | AMD64 asset | ARM64 asset | Installer |
|---|---|---|---|
| Linux | `proxyma-agent-linux-amd64` | `proxyma-agent-linux-arm64` | `install-proxyma-agent.sh` |
| macOS | `proxyma-agent-darwin-amd64` | `proxyma-agent-darwin-arm64` | `install-proxyma-agent.sh` |
| Windows | `proxyma-agent-windows-amd64.exe` | `proxyma-agent-windows-arm64.exe` | `install-proxyma-agent.ps1` |

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

Run the installer from the root of the project where Codex will work. Download
it as a file so it can be inspected before execution; do not pipe a remote
script directly into a shell.

### Linux or macOS

```bash
curl -fL -o install-proxyma-agent.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-agent.sh
bash ./install-proxyma-agent.sh
```

The default executable location is `$HOME/.local/bin/proxyma-agent`.

### Windows

```powershell
Invoke-WebRequest `
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma-agent.ps1 `
  -OutFile .\install-proxyma-agent.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-agent.ps1
```

The default executable location is
`%LOCALAPPDATA%\Programs\Proxyma\bin\proxyma-agent.exe`.

Both installers:

1. Detect the native operating system and AMD64 or ARM64 architecture.
2. Download that binary, `proxyma-agent-harness.tar.gz`, and `SHA256SUMS` from
   one release.
3. Require exactly one valid checksum entry for each downloaded payload and
   verify both SHA-256 hashes.
4. Reject unsafe archive paths, links, special files, and unsafe install
   destinations.
5. Install the binary for the current user, create or refresh `./harness`, and
   install the Codex adapter at `.agents/skills/proxyma-harness/SKILL.md`.

The installers never request or create a signing key and never modify `PATH`.
If the user-local binary directory is not already on `PATH`, follow the printed
instruction and restart Codex so it inherits the new environment.

### Install a pinned release or custom location

Use the same exact release for the installer and its `--version`/`-Version`
argument. This keeps the downloaded binary and harness pinned during
reproducible provisioning or rollback.

Linux or macOS:

```bash
version=v1.2.3
curl -fL -o install-proxyma-agent.sh \
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-agent.sh"
bash ./install-proxyma-agent.sh \
  --version "$version" \
  --project-dir /path/to/project \
  --bin-dir "$HOME/.local/bin"
```

Windows:

```powershell
$version = "v1.2.3"
Invoke-WebRequest `
  "https://github.com/ascemama/proxyma/releases/download/$version/install-proxyma-agent.ps1" `
  -OutFile .\install-proxyma-agent.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass `
  -File .\install-proxyma-agent.ps1 `
  -Version $version `
  -ProjectDir C:\path\to\project `
  -BinDir "$env:LOCALAPPDATA\Programs\Proxyma\bin"
```

Versions must be `latest` or a complete semantic version beginning with `v`,
including an exact prerelease such as `v1.2.3-rc.1`. `latest` resolves only the
latest stable GitHub release.

### Verify the downloaded artifacts manually (optional)

If the installer cannot be used, download the binary for the client platform,
`proxyma-agent-harness.tar.gz`, and `SHA256SUMS` from the same exact release.
Require an exact filename match and verify both payloads before extraction or
installation. For example on Linux:

```bash
version=v1.2.3
asset=proxyma-agent-linux-amd64
base="https://github.com/ascemama/proxyma/releases/download/$version"
curl -fLO "$base/$asset"
curl -fLO "$base/proxyma-agent-harness.tar.gz"
curl -fLO "$base/SHA256SUMS"
grep "  $asset$" SHA256SUMS | sha256sum --check
grep '  proxyma-agent-harness.tar.gz$' SHA256SUMS | sha256sum --check
```

GitHub artifact attestations provide an additional provenance check:

```bash
gh attestation verify "$asset" --repo ascemama/proxyma
gh attestation verify proxyma-agent-harness.tar.gz --repo ascemama/proxyma
```

SHA-256 verification remains mandatory; attestation verification is an
additional check that GitHub Actions produced the asset for this repository.

## Installed project layout

After a new installation, the project contains:

```text
your-project/
├── .agents/
│   └── skills/
│       └── proxyma-harness/
│           └── SKILL.md
└── harness/
    ├── .agents/                 Release copy of the Codex adapter
    ├── .claude/                 Claude Code adapter
    ├── agent-configuration/     Local credentials; initially empty
    ├── examples/                Configuration and task examples
    ├── instruction-pack/        Harness policy, schemas, and API reference
    ├── task-state/              CLI-owned durable task state
    ├── tests/                   Validation material and scenarios
    ├── proxyma-agent.json       Local harness configuration
    └── RELEASE                  Harness release version and commit
```

The `.agents/skills/proxyma-harness` copy at the project root is what Codex
discovers. The `harness/` directory is the shared runtime root and can also be
used by the included Claude Code adapter.

## Safe updates and rollback

Run the selected release installer again. Updates replace the executable and
release-owned harness material transactionally. They preserve these local,
potentially sensitive paths:

- `harness/proxyma-agent.json`;
- `harness/agent-configuration/`; and
- `harness/task-state/`.

Instructions, schemas, examples, tests, bundled adapters, `RELEASE`, and the
project Codex adapter are refreshed from the selected release. The installer
refuses symbolic links or reparse points at managed destinations instead of
following them.

Back up the three preserved paths before a downgrade. Harness v1 state is not
compatible with harness v2 and is not migrated.

## Configure the harness

Edit `harness/proxyma-agent.json`:

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

Set `proxyma_base_url` to the server URL reachable from the Codex workstation.
Relative `agent_config` and `state_dir` paths resolve from the directory that
contains `proxyma-agent.json`.

Create the referenced credential file locally; it is deliberately absent from
the release package:

```json
{
  "agent_id": "YOUR-PROXYMA-AGENT-UUID",
  "agent_jwt_signing_key": "YOUR-AGENT-JWT-SIGNING-KEY"
}
```

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
/harness/task-state/.proxyma-agent.lock
```

## Validate the installation

Open a new terminal after any `PATH` change, then run from the project:

```bash
proxyma-agent version
proxyma-agent doctor
proxyma-agent resume
```

`doctor` validates configuration, credentials, state locking, server health,
and the required `agent_command_idempotency_v1` server capability. `resume`
recovers interrupted local work and polls existing non-final operations once.

Optionally resolve a target without submitting a command:

```bash
proxyma-agent inspect --target server.example
```

Invoke the installed Codex skill explicitly on the first run:

```text
$proxyma-harness Run proxyma-agent doctor and resume, then inspect the Proxyma
target server.example. Do not submit a command yet.
```

Task JSON under `harness/task-state/active/`, `blocked/`, and `closed/` is authoritative and CLI-owned. Do
not edit it manually.

## Configuration discovery and troubleshooting

The CLI selects configuration in this order:

1. `--config <path>` before the command name;
2. `PROXYMA_HARNESS_CONFIG`;
3. an upward search for `harness/proxyma-agent.json`.

Common failures:

- **`proxyma-agent` is not found:** add the printed user-local binary directory
  to `PATH`, restart Codex, and open a new terminal.
- **The skill is unavailable:** verify
  `<project>/.agents/skills/proxyma-harness/SKILL.md`, then restart Codex.
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
- **`submission_unknown`:** run `proxyma-agent resume`; never create a
  replacement submission.

The CLI reduces accidental secret exposure but is not a security boundary
against another unrestricted process running as the same operating-system user.
