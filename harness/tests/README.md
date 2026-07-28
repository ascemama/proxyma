# Proxyma Harness Asset Validation

This directory contains validation scenarios and checks for the client-side Proxyma harness instruction assets.

## Validation Layers

Unit-level validation:

- JSON files parse correctly.
- The live run configuration file `harness/proxyma-harness-run.json` exists and includes `proxyma_base_url` and `agent_config`.
- The harness run schema requires `proxyma_base_url` and `agent_config`, keeping `proxyma_base_url` as a URI-shaped base URL.
- Auth instruction files and Proxyma JWT scripts are present.
- Task-state examples include required fields.
- Status-specific rules are present, including `blocked_reason` and `required_next_action` for blocked tasks.
- Required instruction-pack phrases are present.

Integration-level validation:

- The Codex adapter preserves the harness-neutral instruction-pack rules.
- Future adapter scenarios preserve Proxyma-only access, JSON authority, resume-first processing, active polling, and blocked/done handling.

End-to-end validation:

- Scenario files describe simulated harness workflows for pending Proxyma work, completed Proxyma results, Proxyma unreachable behavior, final Proxyma failures, Proxyma-only target access, duplicate prevention, restart/resume, multi-command tasks, and JSON authority over Markdown.
- Scenario files also cover endpoint configuration, including configured Proxyma base URL use, Proxyma agent authentication, and missing configuration failures.

## Run Validation

From the repository root:

```powershell
.\harness\tests\validate-harness-assets.ps1
```

Expected result:

```text
Proxyma harness asset validation passed.
Checked schemas, task-state examples, scenario files, instruction-pack rules, and Codex adapter coverage.
```

## Latest Recorded Results

- US1 scenarios: passed via `powershell -ExecutionPolicy Bypass -File .\harness\tests\validate-harness-assets.ps1`.
- US2 scenarios: passed via `powershell -ExecutionPolicy Bypass -File .\harness\tests\validate-harness-assets.ps1`.
- US3 scenarios: passed via `powershell -ExecutionPolicy Bypass -File .\harness\tests\validate-harness-assets.ps1`.
- US4 scenarios: passed via `powershell -ExecutionPolicy Bypass -File .\harness\tests\validate-harness-assets.ps1`.
- Full validation: passed.

Direct script execution with `.\harness\tests\validate-harness-assets.ps1` may be blocked by local PowerShell execution policy. Use the process-local bypass command above when needed.
