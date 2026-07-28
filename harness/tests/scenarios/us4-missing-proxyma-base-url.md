# US4 Scenario: Missing Proxyma Base URL

## Purpose

Validate that the harness does not guess an Proxyma endpoint when run configuration is incomplete.

## Given

- A harness run configuration is missing `proxyma_base_url`.
- The harness needs to poll Proxyma or submit a command through Proxyma.

## When

- The harness starts or reaches the point where it must call the Proxyma HTTP API.

## Then

- The harness treats the missing `proxyma_base_url` as a configuration error.
- The harness does not silently assume `http://127.0.0.1:8080` or any other default.
- The harness does not infer the Proxyma endpoint from target hostnames, target IPs, or unrelated workspace files.

## Validation Layer

End-to-end simulated workflow.
