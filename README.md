<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Proxyma](#Proxyma)
  - [Core v1 Scope](#core-v1-scope)
  - [Repository Layout](#repository-layout)
  - [Install](#install)
  - [Deployment](#deployment)
    - [Build Version](#build-version)
  - [License](#license)
  - [Configure Targets](#configure-targets)
  - [Configure Policies](#configure-policies)
  - [Configure Agents](#configure-agents)
  - [Configure Users](#configure-users)
  - [proxymactl Command Reference](#proxymactl-command-reference)
  - [Run](#run)
  - [Frontend GUI](#frontend-gui)
    - [Start for Local Development](#start-for-local-development)
    - [Stop the Frontend](#stop-the-frontend)
    - [Frontend Checks](#frontend-checks)
    - [Troubleshooting](#troubleshooting)
  - [Runtime Configuration](#runtime-configuration)
  - [User Login API](#user-login-api)
  - [Submit a Command](#submit-a-command)
  - [Client Harness Instruction Assets](#client-harness-instruction-assets)
    - [Install for an Agent or Harness](#install-for-an-agent-or-harness)
    - [Harness Run Configuration](#harness-run-configuration)
    - [Task-State Directory](#task-state-directory)
    - [Proxyma-Only Target Access](#Proxyma-only-target-access)
    - [Validate Harness Assets](#validate-harness-assets)
  - [Planning Artifacts](#planning-artifacts)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Proxyma

Proxyma is a Privileged Access Management tool for AI agents. A standard PAM tool monitors and controls user access to target systems; Proxyma does the same for agent-driven access.

In the first core phase, Proxyma runs as a Go HTTP service. Agents submit JSON command requests, Proxyma matches the submitted hostname or IP to a configured target, resolves supported credential placeholders from that matched target for execution, persists the placeholder command lifecycle and result, and lets agents poll task status later.

## Core v1 Scope

- HTTP service for agent command submission and task status polling.
- JSON command request format.
- JSON target definitions, one file per target in `targets/`.
- JSON command policy definitions, one file per policy in `policies/`.
- JSON agent definitions, one file per trusted Proxyma caller in `agents/`.
- JSON user definitions, one file per Proxyma owner in `users/`.
- Local command approval before execution by default, with a flag to disable it for tests or trusted local runs.
- Credential placeholder substitution for `{user}` and, for password-authenticated targets, `{password}` from target records.
- Persistent task records that survive Proxyma restart.
- Structured command results with stdout, stderr, exit status, and truncation indication.
- Localhost-only service exposure by default.
- Service health check at `GET /v1/health`.

Future phases are expected to add richer authorization workflows, notifications, richer audit export, backup integration, and SDK support.

## Repository Layout

```text
cmd/proxyma/                 Go binary entry point
frontend/                 Optional React/Vite GUI; Proxyma core works without it
internal/                 Proxyma implementation packages
targets/                  Target JSON definitions
policies/                 Command policy JSON definitions referenced by targets
agents/                   Agent shared-secret JSON definitions
users/                    User JSON definitions referenced by agents and targets
var/proxyma/tasks/           Local task record storage
var/proxyma/user_sessions/   Local GUI/API user session storage
var/proxyma/credential_tokens/ Local one-time activation and password-reset records
harness/instruction-pack/ Agent/harness Proxyma operating instructions and schemas
harness/adapters/codex/   Codex Agent skill adapter for Proxyma harness behavior
harness/task-state/       Local harness/client task state for active runs
harness/examples/         Example client task-state JSON files and summaries
harness/tests/            Validation scenarios and harness checks
specs/001-Proxyma-core-command/
  plan.md                 Current implementation plan
  contracts/              JSON API and schema contracts
```

## Install

Install Go 1.26.3 or the project-pinned Go version, then clone the repository and run:

```powershell
go mod tidy
go test ./...
go build -o .\bin\proxyma.exe .\cmd\proxyma
```

On Linux or macOS, build with:

```bash
go mod tidy
go test ./...
go build -o ./bin/proxyma ./cmd/proxyma
```

## Deployment

For step-by-step deployment instructions, including local development, Linux
binary installation, and Docker container installation, see
[docs/deployment.md](docs/deployment.md).

The overview links to the complete lifecycle guides for
[local development](docs/deployment-local.md),
[precompiled Linux binaries](docs/deployment-linux-precompiled.md),
[source-built Linux binaries](docs/deployment-linux-binary.md), and
[Docker](docs/deployment-docker.md). Each guide covers first installation,
updates, rollback, troubleshooting, backup, and complete removal.

The recommended host-level installation downloads a verified precompiled
release from GitHub and does not require Go, Node.js, npm, Git, or a source
checkout:

```bash
curl -fL -o install-proxyma.sh \
  https://github.com/ascemama/proxyma/releases/latest/download/install-proxyma.sh
sudo bash ./install-proxyma.sh
```

Use the source-built Linux guide when you specifically want to compile and
install a selected source revision.

The recommended container installation pulls the public multi-platform image
from GitHub Container Registry:

```bash
sudo bash ./scripts/install-docker.sh
```

Both production installers default to native HTTPS on `0.0.0.0:8080` with an
automatically managed self-signed certificate. Use `--tls-cert` and `--tls-key`
to install or renew a trusted certificate, `--self-signed` to switch back, or
`--http` only for development/debugging. Use `--bind-address` with `--port` for
Linux, or with `--host-port` for Docker.

Precompiled binary deployments can pin an exact release with `--version`.
Container deployments can pin an exact image tag or digest with `--image`.
Local contributor builds remain supported through the
`PROXYMA_IMAGE=proxyma:local` Compose override.

### Build Version

The authenticated sidebar identifies the exact Proxyma server build. Git tags
are authoritative: a build made at `v1.2.3` displays `1.2.3`, while the fourth
commit after `v1.2.3-rc.1` displays a value such as
`1.2.3-rc.1+4.g2396d74`. Dirty builds append `.dirty`.

Ordinary `go build` commands use the VCS metadata embedded by Go and display
`dev+g<commit>` when no release tag was injected. Release scripts derive the
nearest reachable tag and commit automatically. Reproducible build jobs can
override them with `PROXYMA_VERSION` and `PROXYMA_COMMIT`; both values are
embedded in the Proxyma server binary. Container builds use the equivalent
`BUILD_VERSION` and `BUILD_COMMIT` build arguments.

Run `proxyma --version` to inspect the identity embedded in an installed or
downloaded server binary without starting it.

## License

Proxyma is free and open-source software distributed under the GNU Affero
General Public License version 3 only (`AGPL-3.0-only`). See [LICENSE](LICENSE)
for the full terms and [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution
policy. The official container image carries the same license and
links each release to its corresponding source code.

## Configure Targets

Create one JSON file per target in `targets/`. Proxyma can still read other directories, including older `target/` or `asset/` layouts, when explicitly configured with `--target-dir`, `PROXYMA_TARGET_DIR`, or `paths.target_dir`. Each target must list the policy IDs that apply to commands for that target, the agent IDs allowed to submit commands to it, and the user IDs that own it.
Target IDs are UUID v4 values generated by `proxymactl`; do not choose human-readable IDs for new records.

Password-authenticated target:

```json
{
  "$schema": "../schemas/proxyma.target.schema.json",
  "id": "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
  "transport": "ssh",
  "os": "linux",
  "ip": "127.0.0.1",
  "username": "demo",
  "policy_ids": ["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
  "agent_ids": ["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "auth": {
    "type": "password",
    "password": "demo-password"
  },
  "description": "Local development target"
}
```

Private-key-authenticated target:

```json
{
  "$schema": "../schemas/proxyma.target.schema.json",
  "id": "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
  "transport": "ssh",
  "os": "linux",
  "ip": "127.0.0.1",
  "username": "demo",
  "policy_ids": ["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
  "agent_ids": ["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "auth": {
    "type": "private_key",
    "private_key_path": "secrets/private_keys/demo-target-key"
  },
  "description": "Local development target using SSH private-key auth"
}
```

Private-key targets support unencrypted SSH private keys only. `private_key_path` must be project-root-relative and must point under `secrets/private_keys/`; Proxyma rejects absolute paths, path traversal, and paths outside that directory. This is SSH private-key authentication, not OpenSSH certificate-file authentication with a separate `*-cert.pub` certificate file.

## Configure Policies

Create one JSON file per command policy in `policies/`. Targets reference policies by `id`; policy files do not reference targets. Policy IDs are UUID v4 values; use `name` for the human-readable label.

```json
{
  "$schema": "../schemas/proxyma.policy.schema.json",
  "id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "name": "Baseline Readonly",
  "description": "Allow simple diagnostic commands, deny destructive commands, approve everything else.",
  "priority": 100,
  "enabled": true,
  "rules": [
    {
      "id": "deny-dangerous",
      "effect": "deny",
      "remote_command_regex": "(?i)\\b(rm\\s+-rf|shutdown|reboot|mkfs|dd\\b)",
      "reason": "Dangerous destructive command."
    },
    {
      "id": "allow-diagnostics",
      "effect": "allow",
      "remote_command_prefix_any": ["hostname", "uptime", "df -h", "free -m"],
      "reason": "Known readonly diagnostic command."
    },
    {
      "id": "approve-default",
      "effect": "approval",
      "always": true,
      "reason": "Command requires human approval."
    }
  ]
}
```

Policy decisions are `allow`, `approval`, or `deny`. Deny wins over other matches; otherwise Proxyma uses policy priority and rule order. A target with no policy IDs is rejected by target validation and denied at runtime if it somehow reaches taskflow.

## Configure Agents

Create one JSON file per trusted Proxyma caller in `agents/`. Each agent must list the user IDs that own it. Agent IDs are UUID v4 values generated by `proxymactl`.

```json
{
  "$schema": "../schemas/proxyma.agent.schema.json",
  "id": "df9ec8bb-1a22-4181-a7c8-126ad553221d",
  "display_name": "Codex Local Harness",
  "shared_secret": "dev-only-codex-harness-secret",
  "enabled": true,
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]
}
```

## Configure Users

Create one JSON file per Proxyma user in `users/`. Every record has an explicit `status`: `active` users require an Argon2id `passwd_hash`, while `pending_activation` and `password_reset_pending` users omit the hash until they choose a password. The GUI creates pending users and returns a one-time activation link. Administrators reset another user's password by generating a one-time reset link, which immediately removes the old credential and revokes existing sessions. `proxymactl users create --password-stdin` remains available for bootstrap and creates an active user.

```json
{
  "$schema": "../schemas/proxyma.user.schema.json",
  "id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "username": "antoine",
  "name": "Antoine",
  "admin": true,
  "status": "active",
  "passwd_hash": "$argon2id$v=19$m=19456,t=2,p=1$WGeElL1uhb1hV8gVYptHmg$rvzcxwJQV6VK9gqH/GnHKWGB3t+40DX8XUYLkr8iiGE"
}
```

## proxymactl Command Reference

Use `proxymactl` to create, update, and delete Proxyma configuration records programmatically. The CLI edits JSON records on disk; it does not call the running Proxyma HTTP service.

Create and update commands take JSON from `--file <path>` or `--file -` for stdin. Create and update input JSON must not include `id`; Proxyma generates UUID v4 IDs on create and keeps IDs immutable on update. Successful commands print a JSON mutation result containing the resource, action, generated or changed ID, and output path.

Global path overrides must appear before the resource command:

```powershell
.\bin\proxymactl.exe --user-dir .\users --agent-dir .\agents --target-dir .\targets users create --file .\new-user.json
```

Supported global overrides are the same configuration flags used by Proxyma, including `--config`, `--no-config`, `--target-dir`, `--policy-dir`, `--agent-dir`, and `--user-dir`.

| Command | Purpose |
| --- | --- |
| `users create --file <path> [--password-stdin]` | Create a user. With `--password-stdin`, `proxymactl` reads the initial password from stdin, hashes it with Argon2id, and creates an active user. Without it, the JSON must include `passwd_hash`. |
| `users update <user-id> --file <path>` | Replace an existing user record while keeping the immutable user ID. |
| `users reset-password <user-id> --password-stdin` | Read a new password from stdin, hash it, update the user, set status to `active`, and revoke that user's existing sessions. |
| `users delete <user-id>` | Delete a user record. |
| `agents create --file <path>` | Create an agent record with a generated UUID. |
| `agents update <agent-id> --file <path>` | Replace an existing agent record while keeping the immutable agent ID. |
| `agents delete <agent-id>` | Delete an agent record. |
| `targets create --file <path>` | Create a target record with a generated UUID. |
| `targets update <target-id> --file <path>` | Replace an existing target record while keeping the immutable target ID. |
| `targets delete <target-id>` | Delete a target record. |

Create an active bootstrap user with a password read from stdin:

```powershell
$password = Read-Host "Initial Proxyma password" -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($password)
try {
  [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) | .\bin\proxymactl.exe users create --file .\new-user.json --password-stdin
} finally {
  [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
}
```

Change an existing user's password from the command line:

```powershell
$password = Read-Host "New Proxyma password" -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($password)
try {
  [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr) | .\bin\proxymactl.exe users reset-password <user-id> --password-stdin
} finally {
  [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
}
```

Manage users:

```powershell
.\bin\proxymactl.exe users create --file .\new-user.json
.\bin\proxymactl.exe users update <user-id> --file .\updated-user.json
.\bin\proxymactl.exe users delete <user-id>
```

Manage agents:

```powershell
.\bin\proxymactl.exe agents create --file .\new-agent.json
.\bin\proxymactl.exe agents update <agent-id> --file .\updated-agent.json
.\bin\proxymactl.exe agents delete <agent-id>
```

Manage targets:

```powershell
.\bin\proxymactl.exe targets create --file .\new-target.json
.\bin\proxymactl.exe targets update <target-id> --file .\updated-target.json
.\bin\proxymactl.exe targets delete <target-id>
```

## Run

Windows:

```powershell
.\bin\proxyma.exe
```

Linux or macOS:

```bash
./bin/proxyma
```

By default, Proxyma evaluates the target's policies before execution. Commands can run directly, be denied, or request human approval through the configured interaction provider. Use `--require-command-approval=false` only for automated tests or trusted local development runs without an explicit policy evaluator.

## Frontend GUI

The GUI is optional and lives under `frontend/`. Proxyma still works as a Go HTTP service without the GUI. The frontend is a pure client-side React/Vite app and communicates with Proxyma only through `/v1/...` API calls.

### Start for Local Development

Start Proxyma from the repository root. HTTP is an explicit development mode;
the scheme automatically selects the non-Secure development cookie:

```powershell
go build -o .\bin\proxyma.exe .\cmd\proxyma
.\bin\proxyma.exe --scheme http
```

In a second terminal, start the frontend:

```powershell
cd .\frontend
npm.cmd install
npm.cmd run dev -- --port 5173
```

Open `http://127.0.0.1:5173/`. Vite proxies `/v1/...` requests to Proxyma at `http://127.0.0.1:8080`.

For the checked-in Playwright fixture data, Proxyma can be started with:

```powershell
.\bin\proxyma.exe `
  --no-config `
  --listen 127.0.0.1:8080 `
  --scheme http `
  --target-dir .\frontend\e2e\fixtures\targets `
  --policy-dir .\frontend\e2e\fixtures\policies `
  --agent-dir .\frontend\e2e\fixtures\agents `
  --user-dir .\frontend\e2e\fixtures\users `
  --task-dir .\frontend\e2e\fixtures\tasks `
  --interaction-dir .\frontend\e2e\fixtures\interactions `
  --user-session-dir .\frontend\e2e\fixtures\user_sessions `
  --require-command-approval=false
```

Fixture login:

```text
username: antoine
password: correct-password
```

### Stop the Frontend

If Proxyma and Vite are running in foreground terminals, stop each with `Ctrl+C`.

If they were started in the background, find the processes and stop them:

```powershell
Get-Process | Where-Object { $_.ProcessName -like '*Proxyma*' -or $_.ProcessName -eq 'node' -or $_.ProcessName -eq 'cmd' }
Stop-Process -Id <pid>
```

If a port is still busy:

```powershell
Get-NetTCPConnection -State Listen -LocalPort 5173,8080 -ErrorAction SilentlyContinue
```

### Frontend Checks

From `frontend/`:

```powershell
npm.cmd run build
npm.cmd run test:e2e
```

The E2E suite starts its own fixture Proxyma backend and frontend server. It covers login, dashboard loading, and desktop visual screenshots for the login and dashboard pages.

Regenerate the README table of contents after changing headings:

```powershell
npm.cmd run docs:toc
```

For Go tests on Windows, if Go cannot execute binaries from the default temp/cache directories, use repository-local ignored directories:

```powershell
New-Item -ItemType Directory -Force -Path .\.gotmp, .\.gocache | Out-Null
$env:GOTMPDIR = (Resolve-Path .\.gotmp).Path
$env:GOCACHE = (Resolve-Path .\.gocache).Path
go test ./...
```

### Troubleshooting

- `npm : File ... npm.ps1 cannot be loaded`: use `npm.cmd` instead of `npm` in PowerShell.
- Older `proxyma.config.json` files may still contain `security.allow_insecure_user_cookies`. The field is obsolete and ignored; remove it when convenient because cookie security now comes only from `server.scheme`.
- Browser shows login but API calls fail with `401`: start Proxyma with `--scheme http` when using the local HTTP Vite proxy.
- Browser cannot reach `http://127.0.0.1:5173/`: confirm Vite is running and port `5173` is listening.
- Dashboard is empty: confirm Proxyma has users, agents, targets, and task records visible to the logged-in user.
- Vite reports `EPERM` under `frontend\node_modules\.vite-temp`: stop Vite, delete `frontend\node_modules\.vite-temp`, then restart `npm.cmd run dev -- --port 5173`.
- `go test ./...` reports `Access is denied` under `AppData\Local\Temp` or `AppData\Local\go-build`: set `GOTMPDIR` and `GOCACHE` to `.gotmp` and `.gocache` as shown above.
- Port `8080` or `5173` is already in use: find the owning process with `Get-NetTCPConnection`, then stop that process or choose a different Vite port.

## Runtime Configuration

Proxyma reads runtime settings from hardcoded defaults, then `proxyma.config.json` in the current working directory when present, then `PROXYMA_*` environment variables, then explicit command-line arguments. Later sources override earlier sources:

```text
hardcoded defaults < proxyma.config.json < PROXYMA_* environment variables < CLI args
```

Copy the tracked `proxyma.config.example.json` file to `proxyma.config.json` to
create a local runtime configuration. The example is exhaustive and references
`schemas/proxyma.config.schema.json` for editor validation, autocomplete, and
future GUI generation. The runtime `proxyma.config.json` file remains ignored by
Git so machine-specific settings are not committed accidentally.

Interaction provider instances are configured separately under `interactions/`,
and agents select an instance with `interaction_instance_id`. Omitted agent
values use the built-in `local` console interaction.

Proxyma accepts these command-line arguments as highest-precedence overrides:

| Argument | Default | Meaning |
| --- | --- | --- |
| `--config` | `proxyma.config.json` | Explicit Proxyma JSON config file path. Missing explicit files are errors. |
| `--no-config` | `false` | Ignore the default `proxyma.config.json` file. |
| `--listen` | `127.0.0.1:8080` | HTTP or HTTPS listen address for the Proxyma service. |
| `--scheme` | `https` | Transport scheme: `https` or explicit development mode `http`. |
| `--tls-mode` | `self_signed` | HTTPS certificate mode: `self_signed` or `provided`. |
| `--tls-cert-file` | `tls/self-signed.crt` | Active certificate or PEM full-chain path. |
| `--tls-key-file` | `tls/self-signed.key` | Matching unencrypted PEM private-key path. |
| `--tls-server-name` | empty | Optional DNS name or IP included in generated-certificate SANs. |
| `--target-dir` | `targets` | Directory containing target JSON files with host/IP and Proxyma-owned credentials. |
| `--policy-dir` | `policies` | Directory containing command policy JSON files referenced by targets. |
| `--agent-dir` | `agents` | Directory containing trusted Proxyma agent JSON files and shared secrets. |
| `--user-dir` | `users` | Directory containing Proxyma user JSON files referenced by agents and targets. |
| `--task-dir` | `var/proxyma/tasks` | Directory where Proxyma persists task JSON records. |
| `--interaction-dir` | `var/proxyma/interactions` | Directory where Proxyma persists human interaction JSON records. |
| `--interaction-config-dir` | `interactions` | Directory containing interaction instance JSON configuration. |
| `--user-session-dir` | `var/proxyma/user_sessions` | Directory where Proxyma persists GUI/API user session JSON records. |
| `--credential-token-dir` | `var/proxyma/credential_tokens` | Directory where Proxyma persists hashed, one-time activation and password-reset records. |
| `--known-hosts-path` | `var/proxyma/.ssh/known_hosts` | Proxyma-owned OpenSSH `known_hosts` file used for SSH host-key verification. Parent directories and the file are created automatically. |
| `--command-timeout` | `5m` | Default timeout for command execution. Uses Go duration syntax such as `30s`, `2m`, or `1h`. |
| `--interaction-timeout` | `24h` | Default timeout for pending human interactions before they expire and block the task. Uses Go duration syntax. |
| `--user-session-ttl` | `12h` | Lifetime for GUI/API user cookie sessions. Uses Go duration syntax. |
| `--user-activation-token-ttl` | `12h` | Default lifetime for user activation links. |
| `--user-activation-token-max-ttl` | `48h` | Maximum activation-link lifetime an administrator may choose. |
| `--output-limit` | `1048576` | Maximum captured bytes for stdout or stderr before truncation. |
| `--require-command-approval` | `true` | Whether command tasks require local approval before execution. Set to `false` for automated tests or trusted local development runs. |
| `--debug` | `false` | Enables HTTP request, command preparation, and command execution debug logging. |

Environment variables use explicit `PROXYMA_*` names, including
`PROXYMA_SCHEME`, `PROXYMA_TLS_MODE`, `PROXYMA_TLS_CERT_FILE`,
`PROXYMA_TLS_KEY_FILE`, `PROXYMA_TLS_SERVER_NAME`,
`PROXYMA_CREDENTIAL_TOKEN_DIR`, `PROXYMA_USER_ACTIVATION_TOKEN_TTL`, and
`PROXYMA_USER_ACTIVATION_TOKEN_MAX_TTL`; the remaining names follow the fields
listed above.

Create one JSON file per non-local interaction instance under `interactions/<provider>/`. Local console interaction is built in as the singleton `local` instance and must not be configured under `interactions/local/`. For ntfy:

```json
{
  "$schema": "../../schemas/proxyma.interaction.ntfy.schema.json",
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "creator_user_id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "display_name": "Ops phone",
  "enabled": true,
  "base_url": "https://ntfy.sh",
  "request_topic": "abc",
  "response_topic": "abc-responses",
  "token": "",
  "poll_interval": "5s",
  "poll_timeout": "30s",
  "responded_by": "ops-phone"
}
```

`creator_user_id` records the GUI user that created the interaction instance. It is not
the full access list: GUI users can also view and edit an interaction instance when they
own an agent that references it. Deletion is still blocked while any agent references
the instance.

Agents that should use that ntfy instance set the same UUID:

```json
{
  "interaction_instance_id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
}
```

## User Login API

GUI/user API authentication is separate from harness authentication. Users call `POST /v1/user/login` with `{"username":"antoine","password":"..."}`. Proxyma verifies the stored Argon2id password hash, creates a persisted opaque session, returns a CSRF token, and sets an HTTP-only cookie. Production cookies use `__Host-proxyma_session` with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`, and an expiry matching the session TTL.

Pending users activate through `GET` and `POST /v1/user/activation` with `Authorization: Activation <token>`. The same endpoint completes administrator-initiated password resets, with the GET response identifying the purpose as `activation` or `password_reset`. POST accepts `{"new_password":"..."}`, consumes the link, and changes the user status to `active`. Activation and reset secrets are stored only as SHA-256 hashes under `var/proxyma/credential_tokens`; each record includes a required `purpose` of `activation` or `password_reset`.

Successful login and session responses also include
`build.version` and `build.commit`, identifying the server that is serving the
authenticated GUI. Use `GET /v1/user/session` with the cookie to fetch the
current safe user profile and rotate the CSRF token. Use
`POST /v1/user/logout` with the cookie and `X-CSRF-Token` header to revoke the
session and clear the cookie. HTTPS uses the Secure
`__Host-proxyma_session` cookie; explicit `--scheme http` development mode
uses the non-Secure `proxyma_session_dev` cookie.

## Submit a Command

```json
{
  "target": {
    "ip": "127.0.0.1"
  },
  "command": "ssh {user}@127.0.0.1 hostname",
  "reasoning": "Identify the target host for diagnostics",
  "requested_by": "agent-demo"
}
```

Submit it to `POST /v1/agent/commands`. Proxyma first checks that the authenticated agent is listed in the target's `agent_ids`, then resolves `{user}` from the matched target for execution and chooses password or private-key SSH authentication from the matched target definition. For password-authenticated targets, Proxyma also resolves `{password}` when the remote command needs it. For private-key-authenticated targets, commands containing `{password}` are rejected because there is no target password to resolve. Task records and status responses keep the placeholder command and policy decision. Proxyma returns a task ID immediately, then taskflow applies the target policy decision before execution. Poll `GET /v1/agent/tasks/{task_id}` with the same Proxyma agent until the task reaches a final state.

Harness clients that only know a hostname or IP should first call
`GET /v1/agent/targets/resolve?hostname=...` or `GET /v1/agent/targets/resolve?ip=...`.
Proxyma returns the safe target `transport`, `os`,
canonical command prefix, and payload kind so the harness does not guess whether the
target needs Linux SSH, Windows SSH, or WinRM PowerShell syntax.

Every target must declare `"transport": "ssh"` or `"transport": "winrm"` and
`"os": "linux"` or `"os": "windows"`. Transport describes how Proxyma connects;
OS describes the target command environment, so Windows-over-SSH is represented as
`"transport": "ssh", "os": "windows"`. WinRM targets must use `"os": "windows"`,
password authentication with NTLM, and a `winrm` object
containing `scheme` (`http` or `https`), optional `port` (defaults to 5985/5986),
and optional `tls_insecure`. HTTPS verifies with the Proxyma host's system trust store
by default. Submit Windows PowerShell as `winrm {user}@host PowerShell-payload`.
Proxyma invokes `powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand`;
agents must not use local `Invoke-Command`, `winrs`, or direct WinRM access. HTTPS
is recommended because HTTP does not provide TLS protection for the remote session.

Prepare the Windows target from an elevated PowerShell session with
`Enable-PSRemoting -Force`, ensure the selected WinRM listener port is permitted by
Windows Firewall, and grant the configured account remoting access. HTTPS/5986 also
requires a WinRM HTTPS listener bound to a certificate whose hostname and issuer are
trusted by the Proxyma host. `tls_insecure` skips certificate verification but does not
change NTLM authentication. Domain Kerberos, CredSSP, Basic authentication, client
certificates, and PowerShell Core (`pwsh`) are not supported in this version.

For WinRM troubleshooting, start Proxyma with `--debug` and retry **Test credentials**.
The log reports the effective scheme/port, TLS verification setting, NTLM username
form, timeout, exit status, and a categorized error without logging the username or
password. `Enter-PSSession` without `-UseSSL` normally tests HTTP/5985, while a new
Proxyma WinRM target defaults to HTTPS/5986. `Enter-PSSession` may also negotiate
Kerberos; Proxyma's WinRM client currently uses NTLM, so verify that Negotiate/NTLM is
enabled and use `DOMAIN\user`, `user@domain`, or the appropriate local account form.

To test the exact Proxyma client path without the web API, use the standalone diagnostic:

```powershell
$env:PROXYMA_WINRM_PASSWORD = Read-Host "WinRM password"
go run ./cmd/winrmdiag --host Thinkpad-99 --scheme http --port 5985 --username 'Thinkpad-99\username'
Remove-Item Env:PROXYMA_WINRM_PASSWORD
```

It first runs the same `Write-Output PROXYMA_WINRM_OK` credential probe as the GUI,
then opens a second WinRM command and runs `whoami.exe`. Both calls use Proxyma's
production NTLM runner and emit the same redacted debug diagnostics. For HTTPS,
pass `--scheme https --port 5986`; add `--tls-insecure` only when intentionally
testing an untrusted listener certificate.

Every Proxyma agent API call, including `GET /v1/health`, must include `Authorization: Bearer <jwt>`.
Generate the JWT with the harness auth scripts under `harness/instruction-pack/auth/`.

## Client Harness Instruction Assets

Proxyma also includes reusable client-side instruction assets for agents or harnesses that sleep and resume while waiting for Proxyma command results. These assets do not create a separate client executable. Existing agents, including Codex Agent first, consume the instructions and use their native file and HTTP capabilities.

Use these files:

```text
harness/
|-- proxyma-harness-run.json
|-- instruction-pack/
|   |-- proxyma-harness.md
|   |-- TASK-STATE.md
|   |-- TASK-SUMMARY-TEMPLATE.md
|   |-- task-state.schema.json
|   `-- proxyma-harness-run.schema.json
|-- adapters/
|   `-- codex/
|       `-- SKILL.md
|-- task-state/
|   |-- pending/
|   |-- done/
|   |-- blocked/
|   `-- summaries/
|-- examples/
|   |-- proxyma-harness-run.json
|   `-- task-state/
`-- tests/
    |-- README.md
    |-- validate-harness-assets.ps1
    `-- scenarios/
```

### Install for an Agent or Harness

For a repository that will run an Proxyma-aware agent, copy or reference:

- `harness/proxyma-harness-run.json` as the live run-configuration file for the local workspace.
- `harness/instruction-pack/` as the harness-neutral Proxyma operating contract.
- `harness/adapters/codex/SKILL.md` when using Codex Agent.
- `harness/examples/proxyma-harness-run.json` as a run-configuration example.
- `harness/examples/task-state/` as example state files and summaries.
- `harness/tests/` when you want local validation scenarios and checks.

The Codex skill must be available to the Codex Agent in the target workspace. Other harnesses, such as Claude Code, Claude cowork, or Gemini CLI, should reuse `harness/instruction-pack/proxyma-harness.md` and replace only harness-specific tool or invocation details.

### Harness Run Configuration

Each harness run must be configured with the Proxyma server base URL as well as the task-state directory and polling interval. The run configuration uses `proxyma_base_url` as the source of truth for Proxyma HTTP calls. In this repository, the concrete local file is `harness/proxyma-harness-run.json`.

Example:

```json
{
  "run_id": "demo-run-20260517-01",
  "started_at": "2026-05-17T08:00:00Z",
  "proxyma_base_url": "http://127.0.0.1:8080",
  "agent_config": "harness/agent-configuration/df9ec8bb-1a22-4181-a7c8-126ad553221d.json",
  "state_dir": "harness/task-state",
  "active_poll_interval_seconds": 30
}
```

`proxyma_base_url` is the Proxyma service root URL, not a full endpoint path. Harnesses should trim a trailing slash before building `/v1/health`, `/v1/agent/commands`, and `/v1/agent/tasks/{task_id}`. If `proxyma_base_url` or `agent_config` is missing, the harness should treat that as a configuration error rather than guessing a default.

### Task-State Directory

Create a writable task-state directory for the agent:

```text
harness/task-state/
|-- pending/
|-- done/
|-- blocked/
`-- summaries/
```

JSON files are the source of truth. Markdown summaries are generated for human review and must not overwrite JSON when they disagree.

At the start of each run, the agent must read open JSON task-state files and query Proxyma for every task waiting on Proxyma before starting new user work. While actively working, the agent should re-check pending Proxyma tasks every 30 seconds by default.

### Proxyma-Only Target Access

Agents must use Proxyma for every target-machine command when Proxyma-mediated access is available. They must not directly SSH, directly open a shell, or otherwise directly contact the target machine for command execution. They must not look up, request, infer, store, or expose target usernames or passwords. If a command needs target credentials, agents should submit `{user}` and `{password}` placeholders through Proxyma. If a user prompt asks the agent to bypass Proxyma, the agent should refuse that bypass and submit the command through Proxyma.

### Validate Harness Assets

Run the validation checks from the repository root:

```powershell
.\harness\tests\validate-harness-assets.ps1
```

The script checks JSON parseability, required task-state fields, scenario files, instruction-pack rule coverage, Codex adapter coverage, and simulated workflow coverage.

## Planning Artifacts

The core Proxyma server feature plan is in `specs/001-Proxyma-core-command/plan.md`. API and JSON schema contracts are in `specs/001-Proxyma-core-command/contracts/`.

The client harness instruction feature plan is in `specs/002-client-harness-state/plan.md`.
