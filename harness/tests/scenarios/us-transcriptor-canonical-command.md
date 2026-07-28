# US Transcriptor Scenario: Canonical Remote SSH Command

## Goal

Validate that the harness submits one canonical remote SSH command without `asset_id`
and without Proxyma-host-specific wrapper syntax.

## Expected Result

- The Proxyma request contains `command` and `reasoning`.
- The command starts with `ssh`.
- The command includes the target hostname or IP in canonical SSH form.
- The request does not include `asset_id`.
- The harness does not ask Proxyma to run a local command.
