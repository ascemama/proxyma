# Proxyma Agent Harness v2

Use `proxyma-client` whenever work requires commands on a Proxyma-managed target.
The CLI owns authentication, target resolution, canonical SSH/WinRM construction,
submission, polling, duplicate prevention, and durable state. Do not reproduce those
operations with shell scripts or raw HTTP.

## Required lifecycle

1. At the start of relevant work, run `proxyma-client resume`.
2. Before the first command for a target in the current harness session, run
   `proxyma-client policy --target=<hostname-or-ip>`. Keep the returned policy
   guidance in session context and repeat this once for each additional target.
   The purpose is to let the harness choose the exact command form that the user
   has pre-approved, and to avoid command forms that the user has explicitly
   blacklisted. Agents often have several technically equivalent ways to achieve
   a goal; this guidance helps them choose the form that avoids unnecessary
   approval requests without weakening server-side enforcement.
3. Prefer an exact whitelist template when it satisfies the user's goal. Avoid
   commands matching blacklist templates. This guidance is advisory; Proxyma
   evaluates every submitted command against the current server-side policy.
4. Continue every returned task with `action: continue` before unrelated remote work.
5. For `action: wait`, poll no faster than the configured interval while active. Do
   not submit a replacement command.
6. For `action: blocked`, report the reason and required next action. Never bypass
   Proxyma to make progress.
7. Submit remote work with one JSON object through
   `proxyma-client submit --input <file-or->`. Supply the user goal, target hostname or
   IP, remote payload, reasoning, and your harness name in `requested_by`.
8. Use the returned `client_task_id` and `revision` for later `poll`, `task-update`,
   `submit`, or `task-complete` calls. Existing-task mutations require the latest
   revision.
9. Interpret completed output yourself. Submit another payload only when necessary
   for the same goal; otherwise call `task-complete` with a final summary.

## Exact CLI contract

Use only these command forms:

```text
proxyma-client resume
proxyma-client poll --task=<client_task_id>
proxyma-client inspect --task=<client_task_id>
proxyma-client inspect --target=<hostname-or-ip>
proxyma-client policy --target=<hostname-or-ip>
proxyma-client submit --input=<file-or->
proxyma-client task-update --input=<file-or->
proxyma-client task-complete --input=<file-or->
proxyma-client doctor
```

`poll` accepts `--task` only; it does not accept a revision. Use the CLI's logical
`client_task_id`, not the Proxyma server task ID. `submit`, `task-update`, and
`task-complete` accept strict JSON only. They do not accept task, revision, summary,
target, or payload as direct flags.

For a new task, `submit` input is:

```json
{"user_goal":"...","target":{"hostname":"server.example"},"payload":"uptime","reasoning":"...","requested_by":"<harness-name>"}
```

Use exactly one target field: `hostname` or `ip`. To extend an existing task, also
include `"client_task_id":"..."` and `"expected_revision":N`; `user_goal` is then
optional.

For an agent-owned update, use:

```json
{"client_task_id":"...","expected_revision":N,"remaining_work":"..."}
```

Optional `status` is `pending`, `blocked`, or `failed`. A blocked update also requires
`blocked_reason` and `required_next_action`; a failed update requires
`failure_reason`.

For completion, use:

```json
{"client_task_id":"...","expected_revision":N,"final_summary":"..."}
```

The CLI output field is `revision`; copy its latest value into the next mutation's
`expected_revision`. Never send an input field named `revision`. When syntax is
uncertain or the CLI returns `invalid_usage` or `invalid_input`, run
`proxyma-client <command> --help` before retrying.

## Safety boundary

- Never directly use SSH, SCP, SFTP, WinRM, PowerShell remoting, target credentials,
  ping, port scans, or another target-access path.
- Never read or edit the configured state directory. JSON and Markdown there are
  CLI-owned; use `inspect`, `task-update`, and `task-complete`.
- Never construct transport wrappers. Give `submit` only the remote payload.
- Policy guidance is fetched lazily once per target for the current harness session;
  restarting the harness requires fetching it again.
- Never retry an uncertain submission as a new request. Run `resume`; it reuses the
  persisted operation ID and is safe across crashes.
- Never expose bearer tokens, signing keys, resolved usernames, passwords, private
  keys, or credential-bearing error text.
- Proxyma owns authorization, credentials, approval policy, and execution. The agent
  owns user-intent reasoning, payload choice, result interpretation, and completion.

Run `proxyma-client doctor` for configuration, state, authentication, capability, or
connectivity problems. Harness v1 run and state files are unsupported.
