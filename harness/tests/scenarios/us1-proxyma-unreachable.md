# US1 Scenario: Proxyma Unreachable

## Purpose

Validate that Proxyma connectivity failures do not incorrectly complete or block a task.

## Given

- A JSON task-state file is waiting for Proxyma.

## When

- The harness attempts to query Proxyma.
- Proxyma cannot be reached because of a connectivity failure.

## Then

- The task remains `waiting_proxyma` or `pending`.
- The error is recorded in `proxyma_connectivity_errors`.
- `last_checked_at` or `updated_at` is updated as appropriate.
- The Markdown summary includes the connectivity error.
- The harness retries during the next active polling interval or future run.

## Validation Layer

End-to-end simulated workflow.
