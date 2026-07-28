# US Placeholder Scenario: No Credential Leakage

## Purpose

Validate that placeholder-based Proxyma work does not place resolved credentials into
harness-visible records.

## Given

- Proxyma accepts a command containing `{user}` or `{password}` placeholders.
- Proxyma resolves those placeholders internally from the matched target selected by hostname or IP.

## When

- The harness writes or updates task-state JSON.
- The harness regenerates the Markdown summary.
- The harness records blocked, failed, timed-out, or completed Proxyma status.

## Then

- The task-state JSON keeps the placeholder command.
- The Markdown summary keeps the placeholder command.
- Neither file contains the resolved target username.
- Neither file contains the resolved target password.
- The harness records Proxyma status and remaining work without credential values.
