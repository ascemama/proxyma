# Proxyma Harness API Reference

This document is the harness-facing API reference for Proxyma. Use it together with
`proxyma-harness.md`. The harness must use the configured `proxyma_base_url` as the service
root URL and derive the endpoint paths from it.
When this file is present, do not probe `/swagger.json`, `/openapi.json`, or other
undocumented endpoints to discover Proxyma syntax. Use this file as the request and
response contract.

Copyable JSON examples are stored under `harness/instruction-pack/api-examples/`.
Use those files for exact request and response shapes:

- `commands.request.json`
- `targets.resolve.response.json`
- `commands.created.response.json`
- `commands.transcriptor-error.response.json`
- `tasks.running.response.json`
- `tasks.completed.response.json`
- `tasks.failed.response.json`
- `health.response.json`

If `proxyma_base_url` is `http://127.0.0.1:8080`, then:

- `POST /v1/agent/commands` becomes `http://127.0.0.1:8080/v1/agent/commands`
- `GET /v1/agent/targets/resolve?ip=20.218.123.238` becomes `http://127.0.0.1:8080/v1/agent/targets/resolve?ip=20.218.123.238`
- `GET /v1/agent/tasks/{task_id}` becomes `http://127.0.0.1:8080/v1/agent/tasks/{task_id}`
- `GET /v1/agent/interactions/{interaction_id}` becomes `http://127.0.0.1:8080/v1/agent/interactions/{interaction_id}`
- `GET /v1/health` becomes `http://127.0.0.1:8080/v1/health`

Trim any trailing slash from `proxyma_base_url` before appending `/v1/...`.

## Authentication

Every Proxyma agent/harness request documented here requires:

```http
Authorization: Bearer <jwt>
```

Generate the JWT from the configured `agent_config` using
`harness/instruction-pack/auth/proxyma-auth.md`, `new-proxyma-jwt.ps1`, or
`new-proxyma-jwt.sh`. Proxyma returns `401` for authentication failures:

- `auth_required`: no bearer token was sent.
- `invalid_token`: the token is malformed, signed with the wrong Agent JWT Signing Key, or belongs to an unknown or disabled agent.
- `token_expired`: the token expired. This response has `retryable: true`; regenerate the token and retry once.

## GET /v1/agent/targets/resolve

Use this endpoint before submitting a target-machine command. Proxyma resolves the
given hostname or IP against configured targets authorized for the authenticated
agent and returns safe routing metadata.

Method:

- `GET`

Path:

- `/v1/agent/targets/resolve`

Query parameters:

- `hostname`: optional target hostname.
- `ip`: optional target IP address.
- At least one parameter is required. When both are supplied, they must identify the same target.
- Parameter values must be URL encoded and neither parameter may be repeated.

Example: `GET /v1/agent/targets/resolve?ip=20.218.123.238`.

Success response:

- Status: `200 OK`

```json
{
  "client_operation_id": "c7b2de27-a00f-4c6c-bb90-b4cc80d1628f",
  "target": {
    "transport": "ssh",
    "os": "linux",
  "hostname": "demo-linux-target",
    "ip": "20.218.123.238"
  },
  "canonical_command": {
    "prefix": "ssh {user}@20.218.123.238 ",
    "payload_kind": "posix_shell"
  }
}
```

`payload_kind` is one of:

- `posix_shell` for Linux SSH payloads.
- `windows_shell` for Windows SSH payloads.
- `powershell` for WinRM payloads.

The response must not be treated as credential material. It intentionally omits
usernames, passwords, auth type, WinRM port, TLS settings, owner IDs, and policy IDs.

Failure responses:

- `401` for missing, invalid, or expired Proxyma bearer token
- `400` for missing, unsupported, or repeated query parameters, duplicate target matches, or hostname/IP conflicts
- `404` when no authorized target matches the requested target hostname or IP. Proxyma
  intentionally returns the same response when the target exists but this agent is not
  authorized for it.

Harness behavior:

- Call this endpoint before constructing a new target-machine command.
- Build the command by appending the intended remote payload to `canonical_command.prefix`.
- Choose payload syntax from `payload_kind`.
- Do not directly probe the target with SSH, WinRM, ping, port scans, `Enter-PSSession`, `Invoke-Command`, or similar checks.

## POST /v1/agent/commands

Use this endpoint to submit one Proxyma command for asynchronous execution.

Proxyma requires an explicit `target` object on this endpoint. Proxyma uses that target
hostname or IP to select one configured target, then separately parses the SSH
destination inside `command` and rejects the request if the command points somewhere
else.
By default Proxyma requires local operator approval before execution. This endpoint still
returns immediately after task creation; approval happens asynchronously through task
status and interaction state.

Method:

- `POST`

Path:

- `/v1/agent/commands`

Request body:

```json
{
  "target": {
    "ip": "20.218.123.238"
  },
  "command": "ssh {user}@20.218.123.238 ls /home",
  "reasoning": "List the contents of /home on the target host",
  "requested_by": "agent-demo"
}
```

Copyable example file: `harness/instruction-pack/api-examples/commands.request.json`.

Rules:

- `client_operation_id` is an optional UUID. It is strongly recommended for all
  automated clients.
- Repeating the same operation ID and request for the same authenticated agent returns
  the original task. Reusing it for different request content returns `409` with code
  `idempotency_conflict`.
- `command` is required and must be a canonical Proxyma-supported remote SSH or WinRM command.
- `target` is required and must contain `hostname`, `ip`, or both.
- `reasoning` is required.
- `requested_by` is optional.
- `metadata` is optional.
- Put the same target hostname or IP directly inside the SSH or WinRM target portion of `command`.
- If both `target.hostname` and `target.ip` are present, they must identify the same
  loaded Proxyma target.
- Proxyma rejects requests when the explicit `target` and command destination conflict.
- Do not include resolved usernames or passwords.
- Do not include `asset_id`.
- Use `GET /v1/agent/targets/resolve` first to learn the configured transport and OS. Do not perform direct target probing.

If the user gives only an IP and Proxyma resolves it to `payload_kind: "posix_shell"`,
then a request to run `ls /home` becomes:

```json
{
  "target": {
    "ip": "20.218.123.238"
  },
  "command": "ssh {user}@20.218.123.238 \"ls /home\"",
  "reasoning": "List the contents of /home on the target host",
  "requested_by": "agent-demo"
}
```

Literal command example: `ssh {user}@20.218.123.238 "ls /home"`.
Literal request-body shape: `{"target":{"ip":"20.218.123.238"},"command":"ssh {user}@20.218.123.238 \"ls /home\"","reasoning":"List the contents of /home on the target host","requested_by":"agent-demo"}`.

For a WinRM target, submit the PowerShell payload after the canonical target:

```json
{
  "target": {"hostname": "win-srv-01"},
  "command": "winrm {user}@win-srv-01 Get-Service",
  "reasoning": "Inspect Windows services",
  "requested_by": "agent-demo"
}
```

Do not use local `Invoke-Command`, `winrs`, or direct WinRM access. Proxyma owns the
NTLM credentials and endpoint configuration.

Success response:

- Status: `202 Accepted`

```json
{
  "task_id": "generated-task-id",
  "status": "created",
  "client_operation_id": "c7b2de27-a00f-4c6c-bb90-b4cc80d1628f"
}
```

Copyable example file: `harness/instruction-pack/api-examples/commands.created.response.json`.

Failure responses:

- `401` for missing, invalid, or expired Proxyma bearer token
- `403` when the authenticated Proxyma agent is not listed in the target's `agent_ids`
- `400` for invalid JSON, missing required fields, target/command mismatch, or unsupported command format
- `404` when no configured target matches the submitted target hostname or IP
- `409` when a client operation ID is reused with different request content

Copyable retryable transcriptor error example:
`harness/instruction-pack/api-examples/commands.transcriptor-error.response.json`.

Harness behavior:

- Record the returned `task_id` in the JSON task-state file immediately.
- Resolve the target through Proxyma before command construction, then always send the explicit `target` object and keep it consistent with the destination embedded in `command`.
- If Proxyma returns `400`, record the failure details and handle it according to the
  current harness rule set.
- If Proxyma returns `404`, treat it as a target-match failure rather than a transport or OS failure.

## GET /v1/agent/tasks/{task_id}

Use this endpoint to poll Proxyma task status for a previously submitted command.

Method:

- `GET`

Path:

- `/v1/agent/tasks/{task_id}`

Path parameter:

- `task_id`: the Proxyma task identifier previously returned by `POST /v1/agent/commands`

Only the Proxyma agent that created the task can read its status and command result.

Success response:

- Status: `200 OK`

Example:

```json
{
  "task_id": "generated-task-id",
  "client_operation_id": "c7b2de27-a00f-4c6c-bb90-b4cc80d1628f",
  "agent_id": "df9ec8bb-1a22-4181-a7c8-126ad553221d",
  "target": {
    "hostname": "local-dev",
    "ip": "127.0.0.1"
  },
  "command": "ssh {user}@127.0.0.1 hostname",
  "reasoning": "Identify the target host for diagnostics",
  "requested_by": "agent-demo",
  "transcription": {
    "transcribed": true,
    "command_family": "ssh_remote_command",
    "strategy": "go_ssh_client"
  },
  "status": "completed",
  "created_at": "2026-05-17T00:00:00Z",
  "started_at": "2026-05-17T00:00:01Z",
  "completed_at": "2026-05-17T00:00:02Z",
  "result": {
    "stdout": "target-host\n",
    "stderr": "",
    "exit_status": 0,
    "truncated": false
  },
  "history": [
    {
      "status": "created",
      "at": "2026-05-17T00:00:00Z"
    },
    {
      "status": "running",
      "at": "2026-05-17T00:00:01Z"
    },
    {
      "status": "completed",
      "at": "2026-05-17T00:00:02Z"
    }
  ]
}
```

When Proxyma is waiting for user input, the task status is `waiting_user_response` and
includes safe pending-interaction metadata:

```json
{
  "task_id": "generated-task-id",
  "command": "ssh {user}@127.0.0.1 hostname",
  "reasoning": "Identify the target host for diagnostics",
  "status": "waiting_user_response",
  "created_at": "2026-05-17T00:00:00Z",
  "pending_interaction": {
    "interaction_id": "generated-interaction-id",
    "kind": "approval_request",
    "status": "pending",
    "created_at": "2026-05-17T00:00:01Z",
    "expires_at": "2026-05-18T00:00:01Z"
  },
  "approval": {
    "required": true,
    "status": "pending",
    "interaction_id": "generated-interaction-id"
  },
  "history": [
    {
      "status": "created",
      "at": "2026-05-17T00:00:00Z"
    },
    {
      "status": "waiting_user_response",
      "at": "2026-05-17T00:00:01Z"
    }
  ]
}
```

Copyable example files:

- `harness/instruction-pack/api-examples/tasks.running.response.json`
- `harness/instruction-pack/api-examples/tasks.completed.response.json`
- `harness/instruction-pack/api-examples/tasks.failed.response.json`

Status values:

- `created`
- `waiting_user_response`
- `running`
- `completed`
- `failed`
- `timed_out`
- `interrupted`
- `abandoned`
- `blocked`

Failure responses:

- `401` for missing, invalid, or expired Proxyma bearer token
- `403` when the authenticated Proxyma agent did not create the task
- `404` when the task does not exist

Harness behavior:

- `created`, `waiting_user_response`, or `running`: keep the client task pending or waiting on Proxyma.
- `completed`: record the result and continue saved work.
- `failed`, `timed_out`, `interrupted`, `abandoned`, or `blocked`: mark the client task blocked
  according to the harness rules.
- `404`: treat as an Proxyma task lookup failure and record it explicitly.

## GET /v1/agent/interactions/{interaction_id}

Use this endpoint to inspect a pending, answered, expired, or canceled human interaction.
Only the Proxyma agent that owns the linked task can inspect the interaction.

Method:

- `GET`

Path:

- `/v1/agent/interactions/{interaction_id}`

Success response:

- Status: `200 OK`

```json
{
  "interaction_id": "generated-interaction-id",
  "task_id": "generated-task-id",
  "kind": "approval_request",
  "expected_response": "approval",
  "subject": "Approve command",
  "message": "Run the submitted command?",
  "status": "pending",
  "created_at": "2026-05-17T00:00:01Z",
  "expires_at": "2026-05-18T00:00:01Z"
}
```

When Proxyma is configured with the local interaction provider, approval prompts are shown
on the Proxyma process console and accept `y`, `yes`, `n`, or `no`. The provider serializes
prompts so only one command approval is active on stdin at a time.

Agents can inspect interaction status but cannot answer interactions. Only Proxyma's
configured interaction provider can submit an approval, rejection, or question response.
After submitting a command, agents must use GET requests to observe state and wait for the
provider-authenticated human response.

## GET /v1/health

Use this endpoint to confirm that the Proxyma service is reachable.

Method:

- `GET`

Path:

- `/v1/health`

Success response:

- Status: `200 OK`

```json
{
  "status": "ok",
  "capabilities": ["agent_command_idempotency_v1"]
}
```

Copyable example file: `harness/instruction-pack/api-examples/health.response.json`.

Harness behavior:

- Send a valid Proxyma bearer token even for health checks.
- This endpoint is optional for preflight or troubleshooting.
- Do not use `GET /v1/health` as a substitute for polling `GET /v1/agent/tasks/{task_id}`.
- A healthy response shows the Proxyma service is reachable, not that a submitted task has completed.
- `proxyma-agent` requires `agent_command_idempotency_v1` before state-changing work.
