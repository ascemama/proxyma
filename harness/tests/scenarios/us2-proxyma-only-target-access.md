# US2 Scenario: Proxyma-Only Target Access

## Purpose

Validate that target-machine commands are routed through Proxyma.

## Given

- A user goal requires command execution on a target machine.
- Proxyma-mediated access is available.

## When

- The harness decides how to perform the command.

## Then

- The harness builds an Proxyma command request with target hostname or IP, `command`, and `reasoning`.
- The command starts with `ssh` and includes the full access command, not only the inner remote payload.
- The harness submits the request to Proxyma.
- The returned Proxyma task ID is recorded in the JSON task-state file.
- The harness does not directly SSH, directly SCP, run a local shell fallback, or otherwise directly contact the target.

## Validation Layer

End-to-end simulated workflow.
