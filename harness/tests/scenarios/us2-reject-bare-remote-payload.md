# US2 Scenario: Reject Bare Remote Payload

## Purpose

Validate that the harness sends the full access command to Proxyma instead of only the
inner remote payload.

## Given

- A user asks for `ls /home` on a target machine.
- Proxyma-mediated access is available.

## When

- The harness constructs the Proxyma request.

## Then

- The harness does not submit bare `ls /home`.
- The harness submits the full command, such as `ssh {user}@host ls /home`.
- The Proxyma request includes the transport prefix and destination host.
