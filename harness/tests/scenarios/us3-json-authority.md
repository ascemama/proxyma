# US3 Scenario: JSON Authority Over Markdown

## Purpose

Validate that JSON is the source of truth when Markdown summaries are missing or stale.

## Given

- A valid JSON task-state file exists.
- The matching Markdown summary is missing or differs from JSON.

## When

- The harness processes the task.

## Then

- The harness uses JSON as authoritative.
- The harness regenerates the Markdown summary from JSON.
- The harness does not use stale Markdown to overwrite JSON.

## Validation Layer

Integration simulated workflow.
