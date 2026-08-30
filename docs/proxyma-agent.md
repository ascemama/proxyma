# `proxyma-agent` Harness CLI

`proxyma-agent` is a deterministic, cross-platform client for AI coding harnesses. It
keeps JWT creation, target resolution, canonical command construction, idempotent
submission, polling, and durable state outside the model prompt.

## Install

Download the binary matching the local harness machine from a Proxyma GitHub release:

- Linux: `proxyma-agent-linux-amd64` or `proxyma-agent-linux-arm64`
- Windows: `proxyma-agent-windows-amd64.exe` or `proxyma-agent-windows-arm64.exe`
- macOS: `proxyma-agent-darwin-amd64` or `proxyma-agent-darwin-arm64`

Verify it against `SHA256SUMS`, rename it to `proxyma-agent` (or
`proxyma-agent.exe`), and place it on `PATH`. The server may remain on Linux; the CLI
runs wherever the agent harness runs.

On Linux and macOS, mark the download executable and install it in a PATH directory,
for example `chmod +x proxyma-agent && install proxyma-agent ~/.local/bin/`. On
Windows, rename the download to `proxyma-agent.exe` and move it into a directory
already listed in the user `Path` environment variable.

For a source build:

```sh
go build -o proxyma-agent ./cmd/proxyma-agent
```

## Configure

Copy `harness/examples/proxyma-agent.json` to `harness/proxyma-agent.json`. Paths to
`agent_config` and `state_dir` are relative to that configuration file. Keep the agent
signing key out of source control; inject `PROXYMA_AGENT_JWT_SIGNING_KEY` when desired.

Configuration lookup is `--config`, then `PROXYMA_HARNESS_CONFIG`, then an upward
search for `harness/proxyma-agent.json`.

Run:

```sh
proxyma-agent doctor
proxyma-agent resume
```

The server must advertise `agent_command_idempotency_v1`. This prevents an uncertain
network response or client crash from creating duplicate remote commands.

## Commands

`submit`, `task-update`, and `task-complete` accept strict JSON through `--input FILE`
or stdin with `--input -`. `poll --task ID` and `inspect --task ID` address the logical
client task, not the Proxyma server task. `inspect --target HOST_OR_IP` returns safe
transport and payload-kind information.

Every response is a protocol-v1 JSON envelope. A successful envelope may request
`wait`, `continue`, `blocked`, or `completed`. Non-zero exit codes distinguish invalid
input/configuration (2), state locking/revision errors (3), and Proxyma/authentication
errors (4).

## Clean v2 upgrade

Harness v1 run configuration and directory-based task state are not migrated. Archive
or remove v1 state explicitly, create `proxyma-agent.json`, and run `doctor`. Existing
server API clients remain compatible when they omit `client_operation_id`, but the v2
CLI requires the idempotency capability before it submits.

## Harness adapters

For Codex, copy or symlink `harness/.agents/skills/proxyma-harness` into a
repository or user `.agents/skills` directory and keep the harness root available to
the agent. Codex discovers those locations and can invoke the skill explicitly as
`$proxyma-harness`; see the [official Codex skill-location documentation](https://developers.openai.com/codex/skills#where-codex-loads-local-skills).

Claude Code can load its project skill directly with:

```sh
claude --add-dir /absolute/path/to/harness
```

The same neutral policy remains under `harness/instruction-pack/`. Adapters contain
only discovery and CLI-use guidance; they do not contain authentication or HTTP
procedures.

## Troubleshooting

- `server_capability_missing`: upgrade the Proxyma server to the same release.
- `state_locked`: another CLI process is active; retry after it completes.
- `state_conflict`: inspect the task and retry with its latest revision.
- `state_corrupt`: preserve the reported file and repair or restore it manually; the
  CLI will not overwrite malformed state.
- `submission_unknown`: run `resume`; never create a replacement submission.

The CLI reduces accidental secret exposure but is not a security boundary against
another unrestricted process running as the same OS user.
