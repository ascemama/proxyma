# Proxyma Agent Authentication

`proxyma-agent` owns bearer-token creation and refresh. It reads `agent_id` (or `id`
from a Proxyma server agent record) and the HS256 signing key from the `agent_config`
referenced by `proxyma-agent.json`.
`PROXYMA_AGENT_JWT_SIGNING_KEY` overrides the file value for deployments that inject
the secret through the environment.

The CLI creates a short-lived token with `kid` and `sub` equal to the agent ID, retries
one `token_expired` response with a fresh token, and never prints the token or signing
key. Harness adapters and models must not create JWTs or construct Authorization
headers.

The scripts in this directory remain reference utilities for human API debugging only;
they are not part of the v2 model-facing harness workflow.
