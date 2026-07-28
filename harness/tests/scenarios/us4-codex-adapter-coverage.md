# US4 Scenario: Codex Adapter Coverage

## Purpose

Validate that the Codex skill adapter preserves every harness-neutral instruction-pack rule.

## Required Adapter Coverage

- Skill metadata and trigger guidance.
- Resume-first task-state processing.
- Proxyma status polling before new work.
- Active polling while working.
- Reading `proxyma_base_url` before Proxyma HTTP calls.
- Proxyma-only target access.
- Direct SSH and direct target contact rejection.
- JSON source-of-truth updates.
- Markdown summary regeneration.
- Blocked and done task handling.
- Statement that the instruction pack is authoritative.

## Validation Layer

Integration-level adapter coverage validation.
