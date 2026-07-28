# US4 Scenario: Instruction Pack Rule Coverage

## Purpose

Validate that the harness-neutral instruction pack contains every required operating rule.

## Required Rules

- Resume first.
- Active polling.
- Proxyma-only target access.
- Run-configured Proxyma endpoint via `proxyma_base_url`.
- JSON authority.
- One logical task, one JSON state file.
- State completeness.
- Proxyma unreachable remains pending or waiting on Proxyma.
- Proxyma final failure states become blocked.
- Completion marks JSON and Markdown done.
- Adapter rules may not weaken the instruction pack.

## Validation Layer

Unit-level prompt-rule validation.
