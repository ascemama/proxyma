# US1 Scenario: Proxyma Final Failure

## Purpose

Validate that Proxyma terminal failure states block the client task and record the next action.

## Given

- A JSON task-state file is waiting for Proxyma.

## When

- Proxyma returns `failed`, `timed_out`, `interrupted`, or an unknown task status.

## Then

- The client task status becomes `blocked`.
- `blocked_reason` is recorded.
- `required_next_action` is recorded.
- The Markdown summary is regenerated from JSON.

## Validation Layer

End-to-end simulated workflow.
