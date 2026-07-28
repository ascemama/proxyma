# US Placeholder Scenario: Credential-Free Command Generation

## Purpose

Validate that harnesses build Proxyma command requests with credential placeholders instead
of resolved target credentials.

## Given

- A user asks the agent to run a target-machine command through Proxyma.
- The harness knows the target hostname or IP address.
- The harness does not know the target username or password.

## When

- The harness prepares the Proxyma command request.

## Then

- The request includes the target hostname or IP.
- The command starts with `ssh`.
- The command uses `{user}` wherever the target username is needed.
- The command uses `{password}` wherever the target password is needed.
- The command includes the full destination and remote command or copy path details.
- The request does not contain a resolved username or password.
- The task-state JSON records the placeholder command exactly as submitted.
