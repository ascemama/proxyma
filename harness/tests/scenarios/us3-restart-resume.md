# US3 Scenario: Restart and Resume with Multiple Proxyma Commands

## Purpose

Validate that one logical task can survive restart and contain multiple Proxyma command references.

## Given

- A task-state file exists for checking target disk usage.
- The file already contains Proxyma task ID `proxyma-task-1130`.
- The first Proxyma result shows more investigation is needed.

## When

- The harness resumes after restart.
- The harness appends a second Proxyma command reference `proxyma-task-1131` for the same user goal.
- Proxyma returns final results for both command references.

## Then

- The same JSON file contains both Proxyma command references in `proxyma_commands`.
- The same JSON file contains both Proxyma results in `proxyma_results`.
- The task is marked `done` only after the original user goal is complete.
- The Markdown summary is regenerated from the final JSON state.

## Validation Layer

End-to-end simulated workflow.
