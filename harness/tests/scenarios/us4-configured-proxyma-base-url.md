# US4 Scenario: Configured Proxyma Base URL

## Purpose

Validate that the harness uses the run-configured Proxyma server location instead of guessing where Proxyma is running.

## Given

- A harness run configuration includes `proxyma_base_url` set to `http://127.0.0.1:8080`.
- The harness has open task-state files or new work that requires Proxyma.

## When

- The harness polls an existing Proxyma task or submits a new Proxyma command.

## Then

- The harness uses `http://127.0.0.1:8080/v1/agent/tasks/{task_id}` for polling.
- The harness uses `http://127.0.0.1:8080/v1/agent/commands` for command submission.
- The harness treats `proxyma_base_url` as the Proxyma server root URL rather than as target-machine data.
- The target hostname or IP inside the SSH command remains separate from the Proxyma server location.

## Validation Layer

End-to-end simulated workflow.
