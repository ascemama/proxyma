# US1 Scenario: Pending Proxyma Task Still Working

## Purpose

Validate that the harness checks pending Proxyma work before new work and does not duplicate an Proxyma command when Proxyma reports the task is still working.

## Given

- `harness/examples/task-state/pending/20260515-103000-check-target-disk-usage-through-proxyma.json` exists.
- The task has status `waiting_proxyma`.
- The task contains Proxyma task ID `proxyma-task-1001`.

## When

- The harness starts a run.
- Proxyma status for `proxyma-task-1001` is `working`.

## Then

- The harness queries Proxyma before processing new user instructions.
- The same JSON file is updated with `last_checked_at`.
- The task remains `waiting_proxyma`.
- No duplicate Proxyma command is submitted.
- The matching Markdown summary is regenerated from JSON.

## Validation Layer

End-to-end simulated workflow.
