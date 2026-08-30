# Proxyma Agent Harness v2

Use `proxyma-agent` whenever work requires commands on a Proxyma-managed target.
The CLI owns authentication, target resolution, canonical SSH/WinRM construction,
submission, polling, duplicate prevention, and durable state. Do not reproduce those
operations with shell scripts or raw HTTP.

## Required lifecycle

1. At the start of relevant work, run `proxyma-agent resume`.
2. Continue every returned task with `action: continue` before unrelated remote work.
3. For `action: wait`, poll no faster than the configured interval while active. Do
   not submit a replacement command.
4. For `action: blocked`, report the reason and required next action. Never bypass
   Proxyma to make progress.
5. Submit remote work with one JSON object through
   `proxyma-agent submit --input <file-or->`. Supply the user goal, target hostname or
   IP, remote payload, reasoning, and your harness name in `requested_by`.
6. Use the returned `client_task_id` and `revision` for later `poll`, `task-update`,
   `submit`, or `task-complete` calls. Existing-task mutations require the latest
   revision.
7. Interpret completed output yourself. Submit another payload only when necessary
   for the same goal; otherwise call `task-complete` with a final summary.

## Safety boundary

- Never directly use SSH, SCP, SFTP, WinRM, PowerShell remoting, target credentials,
  ping, port scans, or another target-access path.
- Never read or edit the configured state directory. JSON and Markdown there are
  CLI-owned; use `inspect`, `task-update`, and `task-complete`.
- Never construct transport wrappers. Give `submit` only the remote payload.
- Never retry an uncertain submission as a new request. Run `resume`; it reuses the
  persisted operation ID and is safe across crashes.
- Never expose bearer tokens, signing keys, resolved usernames, passwords, private
  keys, or credential-bearing error text.
- Proxyma owns authorization, credentials, approval policy, and execution. The agent
  owns user-intent reasoning, payload choice, result interpretation, and completion.

Run `proxyma-agent doctor` for configuration, state, authentication, capability, or
connectivity problems. Harness v1 run and state files are unsupported.
