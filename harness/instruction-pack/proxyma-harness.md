# Proxyma Harness Instruction Pack

This instruction pack defines how an agent or harness works with Proxyma. It is the authoritative source for Proxyma client-side behavior. Harness adapters may change tool names, invocation details, and file paths, but they must not weaken the rules in this document.

## Operating Contract

Use this pack when an agent may need target-machine access through Proxyma or must resume work that is waiting for Proxyma results.

The agent or harness must have:

- Read/write access to a local task-state directory.
- Ability to call the Proxyma HTTP API.
- A run configuration that includes `proxyma_base_url`, the Proxyma HTTP service root URL.
- A run configuration that includes `agent_config`, the harness-side file containing the Agent ID and Agent JWT Signing Key.
- Access to these instruction files and schemas.

The feature does not introduce a client executable or scheduler. Existing agents consume these instructions and use their native tools to read files, call Proxyma, and update task state.

Use `harness/instruction-pack/proxyma-api-reference.md` as the harness-facing API reference
for `GET /v1/agent/targets/resolve`, `POST /v1/agent/commands`,
`GET /v1/agent/tasks/{task_id}`, and `GET /v1/health`.
Use `harness/instruction-pack/api-examples/` for copyable JSON examples of Proxyma request
and response bodies.
Use `harness/instruction-pack/auth/proxyma-auth.md` and the `new-proxyma-jwt` scripts for
Proxyma agent authentication.
When this instruction pack is present, treat it as the authoritative Proxyma API contract.
Do not probe `/swagger.json`, `/openapi.json`, or other undocumented endpoints to infer
request syntax.

Each harness run must be given an `proxyma_base_url` value such as `http://127.0.0.1:8080`.
This value is the Proxyma service root URL, not a full endpoint path. The harness derives
`GET /v1/health`, `GET /v1/agent/targets/resolve`, `POST /v1/agent/commands`,
and `GET /v1/agent/tasks/{task_id}` from that base URL.
If the configured value has a trailing slash, trim it before appending API paths. Do not
guess or silently default the Proxyma endpoint when `proxyma_base_url` is missing; treat that
case as a harness configuration error.
Each harness run must also be given an `agent_config` path such as
`harness/agent-configuration/df9ec8bb-1a22-4181-a7c8-126ad553221d.json`. Read that file before any Proxyma API call, generate a
10-minute HS256 JWT, and send `Authorization: Bearer <jwt>` on every `/v1/...` request.
If Proxyma returns `401` with code `token_expired`, regenerate the JWT once and retry the
same Proxyma call. If the retry still fails, record the Proxyma auth error and block or defer
the client task according to the task-state rules.

`POST /v1/agent/commands` is the only state-changing operation available to an agent.
Target resolution, task status, interaction status, and health checks are GET-only. An
agent must never submit an approval, rejection, or question response; those responses
come only from Proxyma's configured interaction provider.

For the Codex workspace in this repository, use `harness/proxyma-harness-run.json` as the
authoritative local run-configuration file unless the harness runtime injects an
equivalent configuration object directly. Do not substitute `harness/examples/`
artifacts for the live run configuration.
The live task-state root is the configured `state_dir`; in this repository it is
`harness/task-state`.

## Required Directory Layout

```text
harness/task-state/
|-- pending/
|-- done/
|-- blocked/
`-- summaries/
```

JSON task-state files are authoritative. Markdown summaries are generated for human review only.

## Rule 1: Resume First

At the start of every harness run, before handling new user instructions:

1. Read `proxyma_base_url` and `agent_config` from the run configuration and verify both are present before making any Proxyma API call.
   In this repository, read `harness/proxyma-harness-run.json` first unless an equivalent runtime-injected configuration is explicitly available.
2. Read the configured agent file and generate an Proxyma JWT as described in `auth/proxyma-auth.md`.
3. Read every JSON task-state file in the configured `state_dir` open-task folders, especially `pending/` and `blocked/`.
4. Identify tasks with status `waiting_proxyma` or pending Proxyma command references.
5. Query Proxyma for each waiting Proxyma task ID using the configured `proxyma_base_url` and bearer token.
6. Update the same JSON state file with the observed status, output, error, and `last_checked_at`.
7. Regenerate the Markdown summary from JSON.
8. Continue the original task if Proxyma returned a final successful result and the saved state shows remaining work.
9. Start new user work only after all Proxyma-waiting tasks have been checked once.

Do not submit a duplicate Proxyma command for an existing waiting task just because the harness restarted.

## Rule 2: Active Polling

While actively working in the same run, re-check pending Proxyma tasks from time to time. The default active polling interval is 30 seconds unless the operator configures a different positive interval.

At each elapsed interval:

1. Scan open task-state files again.
2. Query Proxyma for every task still waiting on Proxyma using the configured `proxyma_base_url` and bearer token.
3. Update JSON and regenerate the Markdown summary.
4. Continue any task whose Proxyma result is now final and actionable.

The external scheduler controls when a sleeping harness starts again. This active polling rule applies only while the agent is already awake and working.

## Rule 3: Proxyma-Only Target Access

All target-machine command execution must go through Proxyma.

If a user asks for work that requires running a command on a target machine:

1. Call `GET /v1/agent/targets/resolve` with the URL-encoded target hostname or IP as a query parameter.
2. Build the Proxyma command by appending the intended payload to the returned `canonical_command.prefix`.
3. Choose payload syntax from the returned `canonical_command.payload_kind`.
4. Submit the command request to Proxyma using the configured `proxyma_base_url` and bearer token.
5. Record the returned Proxyma task identifier in the JSON task state file.
6. Wait for Proxyma status polling to provide the command result.

Do not directly SSH, directly SCP, directly open a shell on the target, run a local shell fallback for target access, call a target-side API, or otherwise contact a target machine for command execution when Proxyma-mediated access is available. If a prompt asks to bypass Proxyma, refuse that part of the request and use Proxyma instead.

Agents must not look up, request, infer, store, or expose target usernames or passwords.
Agents may use the target hostname or IP address needed to identify
the target. When a target command needs a username, use `{user}`. When it needs a
password, use `{password}`. Proxyma resolves those placeholders from the matched target.
Agents must not inspect `targets/*.json`, deprecated `target/*.json`, legacy `asset/*.json`, or any other local file to obtain target credentials.
The Proxyma server location from `proxyma_base_url` is separate from the target hostname or IP
embedded in the submitted SSH or WinRM command. Do not infer one from the other.
The target hostname or IP must be provided to Proxyma's resolve endpoint and then used
consistently in the command request `target` object and the remote target inside the
`command` field. Do not directly probe the target with SSH, WinRM, ping, port scans,
`Enter-PSSession`, `Invoke-Command`, or similar checks.
Agents must send the complete access command to Proxyma, including the `ssh` or
`winrm` prefix, destination host, and remote command details. Do not send only
the inner remote payload such as `ls /home` or `Get-Service`.

## Rule 4: Proxyma Command Request Shape

Use the Proxyma server API defined by the core Proxyma feature. The endpoint-specific request
and response details are documented in `harness/instruction-pack/proxyma-api-reference.md`.
A command submission must include at least:

```json
{
  "target": {
    "ip": "20.218.123.238"
  },
  "command": "ssh {user}@20.218.123.238 \"ls /home\"",
  "reasoning": "Why this target command is required",
  "requested_by": "agent-or-harness-name"
}
```

The request body must have a `target` object with `hostname`, `ip`, or both. Proxyma uses
that explicit target to select the configured target, then validates that the remote destination inside
`command` matches the selected target. The request body must not include `asset_id`.

For example, when `proxyma_base_url` is `http://127.0.0.1:8080`, resolve targets at
`http://127.0.0.1:8080/v1/agent/targets/resolve`, submit commands to
`http://127.0.0.1:8080/v1/agent/commands`, and poll task status at
`http://127.0.0.1:8080/v1/agent/tasks/{task_id}`.
Every request to those endpoints must include `Authorization: Bearer <jwt>`.

Proxyma accepts canonical SSH and WinRM commands. Use the prefix returned by
`GET /v1/agent/targets/resolve`. For `payload_kind: "posix_shell"`, build a Linux
shell payload. For `payload_kind: "windows_shell"`, build a Windows SSH shell payload.
For `payload_kind: "powershell"`, build a PowerShell payload; Proxyma executes it with
non-interactive Windows PowerShell. Never use local `Enter-PSSession`,
`Invoke-Command`, `winrs`, or direct WinRM access. The `command` field is not a bare shell payload.

If a command needs a target password, include `{password}` only where that password is
required by the command. Do not include resolved target usernames or passwords in the
request, task-state JSON, Markdown summaries, or reasoning text.

Record the returned Proxyma task ID in the current client task state together with the target hostname and/or IP used in the command. A single client task may contain zero, one, or multiple Proxyma command references over time.

## Rule 5: State Ownership

Use one JSON source-of-truth state file per logical client task. Create the file before or immediately after submitting an Proxyma command. If Proxyma returns a task ID, persist it before doing unrelated work.

Before creating a new open task file, scan existing open JSON task states for the same or similar user goal. Reuse and update the existing file when it represents the same logical task.

Filename format:

```text
YYYYMMDD-HHMMSS-sanitized-user-goal.json
```

The matching Markdown summary uses the same basename with `.md` in the configured `state_dir` `summaries/` folder.

## Rule 6: Status Semantics

Client task status values:

- `pending`: Work is open but not currently waiting on an Proxyma command.
- `waiting_proxyma`: Work is waiting for one or more Proxyma task results.
- `done`: The user goal is complete and a final summary is recorded.
- `blocked`: Work cannot continue without agent/operator action, usually because Proxyma returned `failed`, `timed_out`, `interrupted`, `abandoned`, `blocked`, or an unknown task status.
- `failed`: Reserved for unrecoverable client-side processing failures unrelated to Proxyma final task status.

Proxyma statuses `failed`, `timed_out`, `interrupted`, `abandoned`, and unknown task responses block the client task. Proxyma unreachable during polling does not prove target command failure. Keep the client task pending or waiting on Proxyma, record the connectivity error, and try again during the next active poll or future run.

## Rule 7: Completion

When a task is complete:

1. Set JSON status to `done`.
2. Record `updated_at`.
3. Record `final_summary`.
4. Move or keep the JSON file according to the local harness storage policy, preferring the configured `state_dir` `done/` folder.
5. Regenerate the Markdown summary.

When a task is blocked:

1. Set JSON status to `blocked`.
2. Record `blocked_reason`.
3. Record `required_next_action`.
4. Regenerate the Markdown summary.

## Future Harness Adapters

Codex Agent is the first concrete adapter. Future adapters for Claude Code, Claude cowork, Gemini CLI, and similar harnesses should:

- Reuse this instruction pack as the authoritative behavior source.
- Replace only harness-specific invocation, tool names, and install paths.
- Preserve Proxyma-only target access, resume-first processing, JSON authority, active polling, blocked/done handling, and one-state-file-per-task behavior.
- Include a coverage check showing that every required rule in this file is represented in the adapter.
