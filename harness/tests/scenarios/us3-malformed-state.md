# US3 Scenario: Malformed State File Handling

## Purpose

Validate that one malformed or incomplete JSON state file does not prevent processing other valid tasks.

## Given

- One task-state file cannot be parsed or is missing required fields.
- Other task-state files are valid.

## When

- The harness scans task-state files at run start.

## Then

- The harness records an error for the malformed file.
- The harness does not delete or overwrite the malformed file.
- The harness continues processing other valid task-state files.

## Validation Layer

Unit-level JSON validation and end-to-end simulated workflow.
