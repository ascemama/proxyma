# US2 Scenario: No Credential File Lookup

## Purpose

Validate that the harness does not inspect local files to obtain Proxyma target
credentials.

## Given

- A user asks for target-machine access through Proxyma.
- The workspace contains target files with target metadata.

## When

- The harness prepares the Proxyma command request.

## Then

- The harness does not read `targets/*.json`, deprecated `target/*.json`, or legacy `asset/*.json` to obtain usernames or passwords.
- The harness does not search other local folders for target credentials.
- The harness submits a placeholder command through Proxyma instead.
