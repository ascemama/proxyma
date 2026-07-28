# US2 Scenario: No Local Remote-Access Fallback

## Purpose

Validate that the harness does not execute target-access commands locally when Proxyma is
available.

## Given

- A user asks for a command that requires target-machine access.
- Proxyma-mediated access is available.

## When

- The harness decides how to execute the request.

## Then

- The harness does not run local `ssh`.
- The harness does not run local `scp`.
- The harness does not run local `winrs` or PowerShell `Invoke-Command`.
- The harness does not connect directly to WinRM.
- The harness does not use a local shell fallback to reach the target.
- The harness submits the full access command to Proxyma instead.
