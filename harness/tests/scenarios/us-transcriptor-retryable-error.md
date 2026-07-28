# US Transcriptor Scenario: Retryable Command Transcriptor Error

## Goal

Validate that when Proxyma rejects an unsupported command format, the harness-visible task
state preserves the retryable transcriptor error and guidance for submitting a supported
canonical command shape.

## Expected Result

- The Proxyma error identifies the command transcriptor as the rejection point.
- The error is marked retryable.
- The error includes a canonical-shape hint for a supported SSH command.
