# US4 Scenario: Future Harness Adaptation

## Purpose

Validate that future harness adapters can reuse the Proxyma instruction pack without changing core rules.

## Given

- A future adapter is created for Claude Code, Claude cowork, Gemini CLI, or another comparable harness.

## When

- Harness-specific tool names, file-access syntax, or invocation details differ from Codex.

## Then

- The adapter may replace tool and invocation details.
- The adapter must preserve Proxyma-only target access.
- The adapter must preserve resume-first processing.
- The adapter must preserve one JSON state file per task.
- The adapter must preserve JSON authority over Markdown.
- The adapter must preserve active polling and blocked/done handling.

## Validation Layer

Integration-level adapter coverage validation.
