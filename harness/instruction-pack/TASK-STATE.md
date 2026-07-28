# Proxyma Task State Guide

JSON task-state files are the durable source of truth for Proxyma-assisted agent work. Markdown summaries are generated from JSON for human review and are never authoritative.

## File Locations

```text
harness/task-state/
|-- pending/
|-- done/
|-- blocked/
`-- summaries/
```

Open tasks live in `pending/` unless local policy chooses another open-task location. Completed tasks should live in `done/`. Blocked tasks should live in `blocked/`. Summaries live in `summaries/`.
Use the run configuration `state_dir` as the live task-state root. In this repository the default live root is `harness/task-state`; `harness/examples/task-state` contains examples only.

## Filename Convention

Generate new JSON filenames from the task creation timestamp and sanitized user goal:

```text
YYYYMMDD-HHMMSS-sanitized-user-goal.json
```

Use lowercase words, replace whitespace and punctuation with single hyphens, and trim the slug to a readable length. The Markdown summary uses the same basename with `.md`.

Before creating a new task state file, scan open JSON files for the same or similar user goal. If an open task already represents the same logical work, update that file instead of creating a duplicate.

## Required JSON Fields

Every task state must include:

- `schema_version`
- `user_goal`
- `status`
- `created_at`
- `updated_at`
- `remaining_work`

When Proxyma commands exist, include ordered `proxyma_commands` entries with:

- `proxyma_task_id`
- `target`
- `command`
- `reasoning`
- `submitted_at`
- `status`

The `target` object must include at least one of:

- `hostname`
- `ip`

The `command` value must preserve credential placeholders such as `{user}` and
`{password}` exactly as submitted to Proxyma. Do not store resolved target usernames or
passwords in JSON task state or generated Markdown summaries.

When Proxyma results exist, include ordered `proxyma_results` entries with:

- `proxyma_task_id`
- `status`
- `checked_at`
- `stdout`, `stderr`, `exit_status`, and `truncated` when Proxyma provides them

Record Proxyma connectivity failures in `proxyma_connectivity_errors`. Do not store connectivity failures as Proxyma command results because the Proxyma task outcome is unknown.

## Status Transitions

| Status | Meaning | Required action |
|--------|---------|-----------------|
| `pending` | Work is open and not currently waiting for Proxyma. | Continue the saved `remaining_work` when the harness runs. |
| `waiting_proxyma` | Work is waiting for one or more Proxyma task results. | Query Proxyma at run start and during active polling. |
| `done` | Work is complete. | Record `final_summary` and regenerate Markdown. |
| `blocked` | Work cannot continue without agent/operator action. | Record `blocked_reason` and `required_next_action`. |
| `failed` | Unrecoverable client-side processing failure unrelated to Proxyma final task status. | Record `final_summary` describing the client-side failure. |

Proxyma statuses `failed`, `timed_out`, `interrupted`, `abandoned`, `blocked`, and unknown task responses transition the client task to `blocked`, not `failed`. Proxyma status `waiting_user_response` is non-final and should keep the client task waiting.

## Multiple Proxyma Commands

A single client task may require more than one Proxyma command. Append each created Proxyma command to `proxyma_commands` and append each observed result to `proxyma_results`. Keep the logical task in one JSON file until the user goal is done, blocked, or failed.

## Markdown Summary Generation

Regenerate the Markdown summary after every JSON state change. The summary should include:

- Task title and status
- Last updated time
- User goal
- Proxyma commands and latest known statuses
- Proxyma result summary when available
- Connectivity errors when present
- Blocked reason and required next action when blocked
- Remaining work
- Final summary when done or failed

If JSON and Markdown disagree, use JSON and regenerate Markdown.
