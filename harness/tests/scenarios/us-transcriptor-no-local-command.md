# US Transcriptor Scenario: Reject Local Command Requests

## Goal

Validate that the harness never asks Proxyma to run a local shell command when the work
requires target-machine access.

## Expected Result

- The harness submits a canonical SSH command instead of a local shell payload.
- The harness does not strip transport and send only the inner remote payload.
- The harness does not use `cmd /c`, `sh -c`, `powershell`, or similar local wrappers as the Proxyma command.
