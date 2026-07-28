# US3 Scenario: Duplicate Task Prevention

## Purpose

Validate that the harness performs a best-effort duplicate check before creating a new task state file.

## Given

- An open JSON task-state file exists for a user goal.
- A new prompt asks for the same or substantially similar goal.

## When

- The harness prepares to create a new task state file.

## Then

- The harness scans open task-state files.
- The harness reuses and updates the existing logical task when it represents the same work.
- The harness creates a new file only when the open tasks do not match the new user goal.

## Validation Layer

Integration simulated workflow.
