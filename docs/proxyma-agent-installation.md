# Installing the `proxyma-agent` Harness

`proxyma-agent` is the deterministic client used by an AI coding agent to work
through Proxyma. The executable owns authentication, target resolution, canonical
command construction, idempotent submission, polling, and durable task state. The
Codex skill supplies only the agent-facing policy and lifecycle instructions.

This guide uses a project-local installation because it is the simplest Codex setup:

```text
your-project/
├── .agents/
│   └── skills/
│       └── proxyma-harness/
│           └── SKILL.md
└── harness/
    ├── proxyma-agent.json
    ├── agent-configuration/
    │   └── local-agent.json
    ├── instruction-pack/
    │   └── proxyma-harness.md
    └── task-state/
```

The two directories have different purposes:

- `.agents/skills/proxyma-harness/` is the small Codex adapter. Codex discovers the
  skill there.
- `harness/` is the shared, harness-neutral runtime root. Keep its configuration,
  policy, schemas, and CLI-owned state together rather than copying its files into
  `.agents/skills`.

## Prerequisites

Before installation, obtain:

- a Proxyma server that includes the `agent_command_idempotency_v1` capability;
- the UUID and JWT signing key of a Proxyma agent authorized for the required
  targets;
- the `harness/` directory from this repository; and
- either a released `proxyma-agent` executable or Go for a source build.

The CLI runs on the same machine as Codex. The Proxyma server and managed targets can
run on other machines.

## 1. Install the CLI executable

Download the executable for the machine that runs Codex from a
[Proxyma release](https://github.com/ascemama/proxyma/releases):

| Platform | Intel/AMD 64-bit | ARM 64-bit |
|---|---|---|
| Linux | `proxyma-agent-linux-amd64` | `proxyma-agent-linux-arm64` |
| Windows | `proxyma-agent-windows-amd64.exe` | `proxyma-agent-windows-arm64.exe` |
| macOS | `proxyma-agent-darwin-amd64` | `proxyma-agent-darwin-arm64` |

Verify the downloaded file against the release's `SHA256SUMS` before installing it.

### Windows

Rename the download to `proxyma-agent.exe` and put it in a directory on your user
`Path`. For example, after creating a user tools directory and adding it to `Path` in
Windows **Environment Variables**, copy the executable there:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\Proxyma\bin"
Copy-Item .\proxyma-agent-windows-amd64.exe `
  "$env:LOCALAPPDATA\Programs\Proxyma\bin\proxyma-agent.exe"
```

Add `%LOCALAPPDATA%\Programs\Proxyma\bin` to the user `Path` if it is not already
present. Restart Codex after changing `Path`; an already running application does not
inherit later environment changes.

Verify the release checksum with PowerShell:

```powershell
Get-FileHash .\proxyma-agent-windows-amd64.exe -Algorithm SHA256
```

Compare the displayed hash with the entry in `SHA256SUMS`.

### Linux

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 ./proxyma-agent-linux-amd64 "$HOME/.local/bin/proxyma-agent"
sha256sum ./proxyma-agent-linux-amd64
```

Use the ARM64 filename on an ARM64 host. Ensure `$HOME/.local/bin` is on `PATH` before
starting Codex.

### macOS

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 ./proxyma-agent-darwin-arm64 "$HOME/.local/bin/proxyma-agent"
shasum -a 256 ./proxyma-agent-darwin-arm64
```

Use `darwin-amd64` on an Intel Mac. Ensure `$HOME/.local/bin` is on `PATH` before
starting Codex.

### Build from source

From the Proxyma repository root:

```powershell
# Windows
New-Item -ItemType Directory -Force .\bin
go build -o .\bin\proxyma-agent.exe .\cmd\proxyma-agent
```

```sh
# Linux or macOS
mkdir -p ./bin
go build -o ./bin/proxyma-agent ./cmd/proxyma-agent
```

Move the built executable to a directory on `PATH`, or add its directory to `PATH`
before launching Codex.

Confirm that a new terminal can find the executable:

```sh
proxyma-agent version
proxyma-agent --help
```

The command returns a compact JSON envelope. `version` does not require a harness
configuration.

## 2. Place the shared harness root

Copy the complete repository `harness/` directory to the root of the project where
Codex will work. Do not copy each instruction-pack file into a Codex directory.

For an initial installation, the useful runtime files are:

| Path under `harness/` | Purpose | Required at runtime |
|---|---|---|
| `proxyma-agent.json` | CLI configuration and path anchor | Yes |
| `agent-configuration/<name>.json` | Agent UUID and optionally its signing key | Yes |
| `instruction-pack/proxyma-harness.md` | Harness-neutral policy loaded by the skill | Yes |
| `task-state/` | Durable state written exclusively by the CLI | Created as needed |
| `.agents/skills/proxyma-harness/` | Source copy of the Codex adapter | Copied in step 3 |
| `.claude/skills/` | Claude Code adapter | No for Codex |
| `examples/`, `tests/`, and schemas | Examples, validation, and operator reference | No, but safe to retain |

Keeping the complete directory is recommended. It preserves the schemas and
diagnostic material and makes the same root reusable with another supported adapter.

If you are working directly in this Proxyma repository, `harness/` is already in the
correct location; do not make a second copy.

## 3. Install the Codex skill adapter

Codex discovers repository skills under `.agents/skills` from the current working
directory through the repository root. It does not discover the nested
`harness/.agents/skills` directory merely because `harness/` exists below the project
root. Copy or symlink the supplied adapter into the project discovery location.

From the target project root on Windows:

```powershell
New-Item -ItemType Directory -Force .\.agents\skills
Copy-Item -Recurse -Force `
  .\harness\.agents\skills\proxyma-harness `
  .\.agents\skills\proxyma-harness
```

On Linux or macOS:

```sh
mkdir -p .agents/skills
cp -R harness/.agents/skills/proxyma-harness .agents/skills/
```

The resulting file must be:

```text
<project-root>/.agents/skills/proxyma-harness/SKILL.md
```

Codex normally detects skill changes automatically. If the skill does not appear,
restart Codex and open the project root again. Invoke it explicitly in a prompt as
`$proxyma-harness`.

For a personal installation shared by many projects, place the adapter at
`$HOME/.agents/skills/proxyma-harness/` instead. The shared skill still needs a
harness root for each task, either at `<project>/harness` or selected with the
configuration override described below.

See the [official Codex skills documentation](https://developers.openai.com/codex/skills)
for current discovery locations and explicit skill invocation.

## 4. Configure `harness/proxyma-agent.json`

This is the harness configuration you normally edit. Start with
`harness/examples/proxyma-agent.json`, or update the supplied
`harness/proxyma-agent.json`:

```json
{
  "schema_version": "2",
  "proxyma_base_url": "https://proxyma.example",
  "agent_config": "agent-configuration/local-agent.json",
  "state_dir": "task-state",
  "http_timeout_seconds": 15,
  "lock_timeout_seconds": 5,
  "active_poll_interval_seconds": 30
}
```

Change at least:

- `proxyma_base_url` to the URL reachable from the machine running Codex; and
- `agent_config` to the agent credential file created in step 5.

Relative `agent_config` and `state_dir` paths are resolved from the directory that
contains `proxyma-agent.json`, not from the shell's current directory. Keeping them
relative makes the complete `harness/` directory portable.

Use HTTPS outside an explicitly controlled local-development environment. A private
or self-signed certificate must be trusted by the operating system used by the CLI.

The included `df9ec8bb-1a22-4181-a7c8-126ad553221d.json` is a development fixture.
Do not use its key as a production credential.

## 5. Configure the Proxyma agent credential

Create the file referenced by `agent_config`, for example
`harness/agent-configuration/local-agent.json`:

```json
{
  "agent_id": "YOUR-PROXYMA-AGENT-UUID",
  "agent_jwt_signing_key": "YOUR-AGENT-JWT-SIGNING-KEY"
}
```

The UUID and signing key must match an enabled agent record on the Proxyma server,
and that agent must be assigned to the intended targets. Restrict the file so only
the local user running Codex can read it.

For injected-secret environments, the file can contain only `agent_id`. Set
`PROXYMA_AGENT_JWT_SIGNING_KEY` through the operating system, a secret manager, or the
process launcher. The environment value overrides a key in the file. Set it before
starting Codex so CLI processes launched by Codex inherit it; never place the secret
in a Codex prompt or a CLI argument.

When adding the harness to another repository, add its local credentials and runtime
state to that repository's ignore rules. For example:

```gitignore
/harness/agent-configuration/local-agent.json
/harness/task-state/tasks/
/harness/task-state/summaries/
/harness/task-state/.journal/
/harness/task-state/.proxyma-agent.lock
```

Adjust the credential filename if you chose a different one. The Proxyma repository
already contains equivalent ignore rules for its local harness state.

## 6. Understand configuration discovery

The CLI selects one configuration in this order:

1. `--config <path>` before the command name;
2. `PROXYMA_HARNESS_CONFIG`;
3. an upward search from the current directory for
   `harness/proxyma-agent.json`.

With the recommended project-local layout, start Codex anywhere inside the project
and no override is needed.

For an explicit configuration:

```sh
proxyma-agent --config /absolute/path/to/harness/proxyma-agent.json doctor
```

On Windows:

```powershell
proxyma-agent --config C:\absolute\path\to\harness\proxyma-agent.json doctor
```

The global `--config` option must appear before `doctor`, `resume`, or another command.

If the harness root is outside the project, set `PROXYMA_HARNESS_CONFIG` to the
absolute configuration path before starting Codex. Also tell Codex the absolute
harness-root path in the first prompt so the skill can read
`instruction-pack/proxyma-harness.md`.

## 7. Validate the installation

From the target project root, run:

```sh
proxyma-agent version
proxyma-agent doctor
proxyma-agent resume
```

`doctor` validates configuration, credentials, task state, locking, server health,
and the required server capability. `resume` recovers interrupted local work and
polls existing non-final operations once. On a new installation it should report no
open tasks.

Optionally verify a target without submitting a command:

```sh
proxyma-agent inspect --target server.example
```

These commands write machine-readable JSON to stdout and diagnostics to stderr.
Exit code 2 indicates usage or configuration trouble, 3 indicates state or revision
conflicts, and 4 indicates a Proxyma, connectivity, or authentication failure.

## 8. Run the first Codex test

Open the target project in Codex and use a prompt that names the skill and harness
root explicitly on the first run, for example:

```text
$proxyma-harness Use the harness at ./harness. Run proxyma-agent doctor and resume,
then inspect the Proxyma target server.example. Do not submit a command yet.
```

After that succeeds, ask Codex to perform a harmless remote check through Proxyma.
The agent should run `resume` first, submit only the remote payload through
`proxyma-agent`, and never construct its own SSH or WinRM wrapper.

Task JSON under `harness/task-state/tasks/` is authoritative and CLI-owned. Do not
edit it manually. Use `inspect`, `task-update`, and `task-complete` for lifecycle
changes.

## Using the same harness outside a project

A single shared harness root can serve multiple Codex projects when local state and
credentials are intentionally shared:

1. Install the skill once under `$HOME/.agents/skills/proxyma-harness/`.
2. Keep the complete harness at a stable absolute path.
3. Set `PROXYMA_HARNESS_CONFIG` before starting Codex.
4. State the harness-root path when invoking the skill.

Be aware that one `state_dir` means every project sees the same durable logical task
set. Use a separate `proxyma-agent.json` and state directory when projects require
separate credentials, authorization boundaries, or task histories.

## Claude Code adapter

The same shared `harness/` root also contains a Claude Code adapter. Keep the root
intact and launch Claude Code with:

```sh
claude --add-dir /absolute/path/to/harness
```

Do not copy the Claude adapter into the Codex `.agents/skills` directory. Both
adapters call the same CLI and use the same harness-neutral policy and state.

## Clean v2 upgrade

Harness v1 run configuration and status-directory task state are not migrated.
Archive v1 state outside the configured v2 `state_dir`, create the v2
`proxyma-agent.json`, and run `doctor`. Do not copy old pending, active, completed,
failed, or blocked files into `task-state/tasks/`.

Existing API clients remain compatible when they omit `client_operation_id`, but the
v2 CLI refuses state-changing submissions unless the server advertises
`agent_command_idempotency_v1`.

## Command quick reference

| Command | Purpose |
|---|---|
| `--help` or `<command> --help` | Show machine-readable global or command help |
| `version` | Show CLI, commit, protocol, and state-schema versions |
| `doctor` | Validate the complete local and server setup |
| `resume` | Recover writes and safely resume all open operations |
| `inspect --target HOST` | Resolve safe target metadata without submission |
| `inspect --task ID` | Read authoritative logical task state |
| `submit --input FILE` | Create or extend a task from strict JSON input |
| `poll --task ID` | Poll all non-final operations for one task |
| `task-update --input FILE` | Update agent-owned task fields by revision |
| `task-complete --input FILE` | Record the final summary and complete a task |

`submit`, `task-update`, and `task-complete` accept `--input -` for stdin. Avoid
putting secrets in input documents or command arguments.

## Troubleshooting

- **`proxyma-agent` is not recognized or found:** the executable directory is not on
  the environment inherited by Codex. Fix `Path`/`PATH`, restart Codex, and rerun
  `proxyma-agent version`.
- **The skill is not available:** verify the exact path
  `<project>/.agents/skills/proxyma-harness/SKILL.md`, then restart Codex.
- **`config_not_found`:** put `harness/proxyma-agent.json` beneath the project root,
  or select it with `--config` or `PROXYMA_HARNESS_CONFIG`.
- **`server_capability_missing`:** upgrade the Proxyma server to a harness-v2 release.
- **Authentication failure:** verify the agent UUID, signing key, server time, and
  that the environment variable was present when Codex started.
- **Target authorization failure:** ensure the server target record lists the same
  agent UUID.
- **TLS error:** use a certificate trusted by the CLI host; do not disable TLS
  verification as a routine fix.
- **`state_locked`:** another CLI process is mutating the same state directory; wait
  for it to finish and retry.
- **`state_conflict`:** inspect the task and retry with its latest revision.
- **`state_corrupt`:** preserve the reported file and repair or restore it manually;
  the CLI intentionally leaves malformed state untouched.
- **`submission_unknown`:** run `proxyma-agent resume`; never create a replacement
  submission.

The CLI reduces accidental secret exposure but is not a security boundary against
another unrestricted process running as the same operating-system user.
