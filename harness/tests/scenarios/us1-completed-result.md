# US1 Scenario: Pending Proxyma Task Completed

## Purpose

Validate that a completed Proxyma result is recorded and the agent continues the original work from saved state.

## Given

- A pending JSON task-state file contains Proxyma task ID `proxyma-task-1001`.
- The saved `remaining_work` explains what to do after Proxyma returns output.

## When

- The harness starts a run.
- Proxyma status for `proxyma-task-1001` is `done` and includes command output.

## Then

- The Proxyma output is appended to `proxyma_results`.
- The command reference status is updated.
- The same JSON task-state file records the result.
- The harness uses `remaining_work` to continue the original task.
- The task is marked `done` only when the original user goal is complete.

## Validation Layer

End-to-end simulated workflow.
