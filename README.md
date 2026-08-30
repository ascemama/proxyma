<p align="center">
  <img src="frontend/public/proxyma-mark.svg" alt="Proxyma logo" width="112">
</p>

<h1 align="center">Proxyma</h1>

<p align="center">
  A policy-enforcing proxy for managing AI agent access to remote systems.
</p>

## Why Proxyma

Giving an autonomous agent direct credentials or administrative access to a
production machine is dangerous. Proxyma places a controlled boundary between
the agent and the target: the agent asks Proxyma to run a command, and Proxyma
authenticates the caller, authorizes the target, evaluates command policy,
requests human approval when required, and performs the remote execution
without disclosing target credentials to the agent.

A typical use case is an operations agent that needs to inspect or maintain
Linux and Windows servers. The agent knows the target hostname or IP and the
work it needs to perform, while Proxyma owns the target connection details,
credentials, command policy, audit state, and approval channel.

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [How It Works](#how-it-works)
  - [Policy-approved command](#policy-approved-command)
  - [Approval-required command](#approval-required-command)
- [Supported Capabilities](#supported-capabilities)
- [Installation](#installation)
- [Repository Layout](#repository-layout)
- [`proxymactl` Command Reference](#proxymactl-command-reference)
- [Runtime Configuration](#runtime-configuration)
- [Agent Harness](#agent-harness)
- [Documentation](#documentation)
- [License](#license)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## How It Works

### Policy-approved command

1. The agent authenticates to Proxyma and resolves a configured target by
   hostname or IP.
2. Proxyma returns the canonical SSH or WinRM command shape without exposing
   credentials.
3. The agent submits the command with safe placeholders such as `{user}`.
4. Proxyma verifies that the agent may use the target and evaluates the
   target's ordered command policies.
5. An allowed command is executed through Proxyma using the target's stored
   credentials. The task and result are persisted for the agent to poll.

### Approval-required command

1. The command follows the same authentication, target resolution, and policy
   path.
2. When policy returns `approval`, Proxyma pauses the task and sends an
   approval request through the agent's configured local-console or ntfy
   interaction provider.
3. Approval starts remote execution; rejection or expiration blocks the task.
4. The agent continues polling the durable task record and receives the final
   status, stdout, stderr, and exit status.

See the [detailed usage flow](docs/proxyma-typical-usage-flow.md) and the
[agent API reference](harness/instruction-pack/proxyma-api-reference.md) for
the complete lifecycle.

## Supported Capabilities

| Area | Support |
| --- | --- |
| Remote transports | SSH and WinRM |
| Target environments | Linux shell, Windows shell over SSH, and Windows PowerShell over WinRM |
| Target authentication | Passwords and unencrypted SSH private keys; WinRM uses password authentication with NTLM |
| Agent authentication | Short-lived HS256 JWTs signed with an Agent JWT Signing Key |
| User authentication | Argon2id passwords, persisted cookie sessions, CSRF protection, activation links, and password-reset links |
| Server transport security | HTTPS with an automatically managed self-signed certificate or an operator-provided certificate and key; explicit HTTP mode for local development |
| Human interaction | Built-in local-console approval and configurable ntfy instances |
| Command policy | Ordered `allow`, `approval`, and `deny` rules assigned to targets |
| Persistence | JSON-backed tasks and interactions, plus a SQLite interaction-history search index |
| Administration | Browser GUI and the filesystem-oriented `proxymactl` CLI |

Proxyma does not currently support target-side certificate authentication.
HTTPS certificates protect the Proxyma service; target authentication uses a
password or SSH private key.

## Installation

Choose the deployment model that matches the environment. Each linked guide
covers prerequisites, installation, verification, updates, rollback, backup,
troubleshooting, and removal.

| Installation type | Intended use | Documentation |
| --- | --- | --- |
| Local development | Change or test the Go service and React frontend | [Local development](docs/deployment-local.md) |
| Precompiled Linux binary | Recommended host-level Linux installation without a compiler or source checkout | [Precompiled Linux](docs/deployment-linux-precompiled.md) |
| Source-built Linux binary | Build and install a selected source revision on Linux | [Source-built Linux](docs/deployment-linux-binary.md) |
| Docker | Run the production service and embedded frontend in a container | [Docker](docs/deployment-docker.md) |

See the [deployment overview](docs/deployment.md) for a side-by-side comparison.

## Repository Layout

```text
.github/workflows/         Release and container automation
cmd/proxyma/               Proxyma server entry point
cmd/proxyma-agent/         Cross-platform deterministic agent harness CLI
cmd/proxymactl/            Administrator CLI entry point
deploy/systemd/            Linux systemd service definition
docs/                      Deployment, operation, API, and architecture guides
frontend/                  React/Vite GUI, shared logo, and browser tests
harness/                   Agent instruction pack, adapters, schemas, and examples
internal/                  Go implementation packages
policies/                  Checked-in command policy definitions
schemas/                   JSON Schemas for configuration records
scripts/                   Build, install, release, test, and uninstall scripts
tests/                     Service-level fixtures and end-to-end tests
Dockerfile                 Production container build
proxyma.config.example.json Exhaustive runtime configuration example
```

Targets, agents, users, interaction instances, secrets, TLS material, and
`var/proxyma/` state are runtime data. They are created by an installation or
administrator and are not part of the tracked source layout.

## `proxymactl` Command Reference

`proxymactl` manages Proxyma's on-disk records and transport configuration. It
does not call the running HTTP service. Resource create and update commands
accept `--file <path>` or `--file -`; create input omits `id` because the CLI
generates a UUID v4.

Global configuration flags must appear before the resource and action:

```powershell
.\bin\proxymactl.exe --user-dir .\users users create --file .\new-user.json
```

| Command | Purpose |
| --- | --- |
| `users create --file <path> [--password-stdin]` | Create a user; `--password-stdin` hashes the supplied password with Argon2id and activates the user. |
| `users update <user-id> --file <path>` | Replace a user record while preserving its ID. |
| `users reset-password <user-id> --password-stdin` | Replace the password, activate the user, and revoke existing sessions. |
| `users delete <user-id>` | Delete a user record. |
| `agents create --file <path>` | Create an agent with a generated UUID. |
| `agents update <agent-id> --file <path>` | Replace an agent record while preserving its ID. |
| `agents delete <agent-id>` | Delete an agent record. |
| `targets create --file <path>` | Create a target with a generated UUID. |
| `targets update <target-id> --file <path>` | Replace a target record while preserving its ID. |
| `targets delete <target-id>` | Delete a target record. |
| `certificates validate --cert-file <path> --key-file <path>` | Validate that a PEM certificate and unencrypted private key match and are currently usable. |
| `certificates install --cert-file <path> --key-file <path> --dest-cert-file <path> --dest-key-file <path>` | Validate and atomically install a certificate pair at managed destinations. |
| `config update-transport --file <path> [--scheme <https\|http>] [--tls-mode <self_signed\|provided>] [--tls-cert-file <path> --tls-key-file <path>] [--tls-server-name <name>]` | Update server transport fields in a Proxyma JSON configuration file. |

Record formats, examples, relationships, and schema links are documented in
[Configuration](docs/configuration.md).

## Runtime Configuration

Proxyma loads settings in increasing order of precedence:

```text
hardcoded defaults < proxyma.config.json < PROXYMA_* environment variables < CLI arguments
```

Copy [proxyma.config.example.json](proxyma.config.example.json) to
`proxyma.config.json` for an exhaustive file-based configuration. The following
arguments are the highest-precedence runtime overrides:

| Argument | Default | Purpose |
| --- | --- | --- |
| `--config` | `proxyma.config.json` | Use an explicit JSON configuration file; a missing explicit file is an error. |
| `--no-config` | `false` | Ignore the default JSON configuration file. |
| `--listen` | `127.0.0.1:8080` | HTTP or HTTPS listen address. |
| `--scheme` | `https` | Server scheme: `https` or explicit development mode `http`. |
| `--tls-mode` | `self_signed` | HTTPS certificate mode: `self_signed` or `provided`. |
| `--tls-cert-file` | `tls/self-signed.crt` | Active certificate or PEM full-chain path. |
| `--tls-key-file` | `tls/self-signed.key` | Matching unencrypted PEM private-key path. |
| `--tls-server-name` | empty | Additional DNS name or IP for a generated certificate SAN. |
| `--target-dir` | `targets` | Target JSON directory. |
| `--policy-dir` | `policies` | Command policy JSON directory. |
| `--agent-dir` | `agents` | Agent JSON directory. |
| `--user-dir` | `users` | User JSON directory. |
| `--task-dir` | `var/proxyma/tasks` | Persisted task JSON directory. |
| `--interaction-dir` | `var/proxyma/interactions` | Persisted interaction JSON directory. |
| `--interaction-history-db` | `var/proxyma/interaction-history.db` | SQLite interaction-history search index. |
| `--interaction-config-dir` | `interactions` | Interaction instance configuration directory. |
| `--user-session-dir` | `var/proxyma/user_sessions` | Persisted user session directory. |
| `--credential-token-dir` | `var/proxyma/credential_tokens` | Hashed activation and password-reset token directory. |
| `--known-hosts-path` | `var/proxyma/.ssh/known_hosts` | Proxyma-owned OpenSSH host-key trust file. |
| `--command-timeout` | `5m` | Default command execution timeout using Go duration syntax. |
| `--interaction-timeout` | `24h` | Pending human-interaction timeout using Go duration syntax. |
| `--user-session-ttl` | `12h` | GUI and user API session lifetime. |
| `--user-activation-token-ttl` | `12h` | Default activation-link lifetime. |
| `--user-activation-token-max-ttl` | `48h` | Maximum administrator-selected activation-link lifetime. |
| `--output-limit` | `1048576` | Maximum captured bytes for stdout or stderr before truncation. |
| `--require-command-approval` | `true` | Require approval when no explicit command policy evaluator is available. |
| `--debug` | `false` | Log HTTP requests, request bodies, and command execution details. |
| `--rebuild-interaction-history` | `false` | Rebuild the SQLite interaction index from JSON records before serving. |

Environment-variable names and JSON field mappings are listed in
[Configuration](docs/configuration.md). Run `proxyma --version` to inspect an
installed server binary without starting it.

## Agent Harness

The cross-platform `proxyma-agent` CLI gives existing agent runtimes one
deterministic interface for Proxyma authentication, target resolution, command
submission, polling, duplicate prevention, and durable client task state. Agents:

- route target-machine commands through Proxyma instead of connecting directly;
- run `proxyma-agent resume` before new remote work;
- supply only target identity, remote payload, and reasoning;
- interpret results and decide when the logical user goal is complete.

Start with the
[harness instruction pack](harness/instruction-pack/proxyma-harness.md),
and [CLI authentication guide](harness/instruction-pack/auth/proxyma-auth.md). The
[harness configuration schema](harness/instruction-pack/proxyma-agent.schema.json),
[task-state schema](harness/instruction-pack/task-state.schema.json),
[examples](harness/examples/), and
[validation instructions](harness/tests/README.md) support integrations. A
[Codex project skill](harness/.agents/skills/proxyma-harness/SKILL.md) and a
Claude Code project skill under `harness/.claude/skills/` are included. Harness v1 state and run
configuration are intentionally unsupported.

## Documentation

- [Deployment overview](docs/deployment.md)
- [`proxyma-agent` harness CLI](docs/proxyma-agent.md)
- [Configuration records and runtime settings](docs/configuration.md)
- [User and agent API usage](docs/api.md)
- [Build and version metadata](docs/build-version.md)
- [Typical command lifecycle](docs/proxyma-typical-usage-flow.md)
- [Interaction architecture](docs/interaction-architecture.md)
- [Interaction-provider integration](docs/interaction-integration.md)
- [Storage integration](docs/store-integration.md)

## License

Proxyma is free and open-source software distributed under the GNU Affero
General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE)
for the full terms and [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution
policy. The official container image carries the same license and links each
release to its corresponding source code.
