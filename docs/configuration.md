# Proxyma Configuration

Proxyma keeps operator-managed records in JSON files and validates their
relationships when the service starts. The default entity directories are
`var/proxyma/targets/`, `var/proxyma/policies/`, `var/proxyma/agents/`,
`var/proxyma/users/`, and `var/proxyma/interaction_instances/`; deployments may
override every path. Protected LLM profiles, including provider API keys, use
`var/proxyma/llm_profiles/`. Durable tasks and approval records use
`var/proxyma/tasks/` and `var/proxyma/interactions/` respectively.

Files under `docs/examples/` are source-controlled examples only. They are not
loaded as live configuration. The old root-level entity directories are no
longer read by default.

Use `proxymactl` to create, update, and delete users, agents, and targets. The
browser GUI can manage the same resources as well as policies and interaction
instances. IDs are UUID v4 values; use human-readable name and description
fields for display.

## Targets

Each target names exactly one policy, the agents authorized to submit commands,
and the users who own it. A target must provide a hostname, an IP address, or
both.

Password-authenticated SSH target:

```json
{
  "$schema": "../schemas/proxyma.target.schema.json",
  "id": "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
  "transport": "ssh",
  "os": "linux",
  "hostname": "app.example",
  "ip": "192.0.2.10",
  "username": "automation",
  "policy_id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "agent_ids": ["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "auth": {
    "type": "password",
    "password": "replace-with-a-managed-secret"
  },
  "description": "Application server"
}
```

Private-key SSH target:

```json
{
  "$schema": "../schemas/proxyma.target.schema.json",
  "id": "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
  "transport": "ssh",
  "os": "linux",
  "hostname": "db.example",
  "username": "automation",
  "policy_id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "agent_ids": ["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "auth": {
    "type": "private_key",
    "private_key_path": "secrets/private_keys/db-target-key"
  },
  "description": "Database server"
}
```

Private keys must be unencrypted, project-root-relative, and below
`secrets/private_keys/`. Absolute paths, traversal, and paths outside that
directory are rejected. This is SSH private-key authentication, not OpenSSH
certificate authentication.

Windows WinRM target:

```json
{
  "$schema": "../schemas/proxyma.target.schema.json",
  "id": "75be298f-66c5-4b55-b067-8da7b6c8d597",
  "transport": "winrm",
  "os": "windows",
  "hostname": "win.example",
  "username": "EXAMPLE\\automation",
  "policy_id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "agent_ids": ["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "auth": {
    "type": "password",
    "password": "replace-with-a-managed-secret"
  },
  "winrm": {
    "scheme": "https",
    "port": 5986,
    "tls_insecure": false
  },
  "description": "Windows application server"
}
```

WinRM targets must use Windows, password authentication, and NTLM. HTTPS
verifies the remote certificate through the Proxyma host's system trust store
unless `tls_insecure` is deliberately enabled. Domain Kerberos, CredSSP, Basic
authentication, client certificates, and PowerShell Core are not supported.

The complete contract is
[`schemas/proxyma.target.schema.json`](../schemas/proxyma.target.schema.json).

## Policies

Each target references exactly one command-policy ID. Policies are owned by the
user who creates them and contain blacklist (`deny`) and whitelist (`allow`)
typed command templates. Within the selected policy, deny wins over allow. If
no rule matches, an optional LLM profile can apply a user-authored prompt; an
invalid, unavailable, or uncertain result requires human approval. Without an
LLM fallback, an unmatched command requires human approval. Assigning a
different policy replaces the target's current policy.

```json
{
  "$schema": "../schemas/proxyma.policy.schema.json",
  "id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "owner_user_id": "14fb57cf-11a1-45ed-ad51-26d733a0b2db",
  "name": "Baseline Readonly",
  "description": "Allow diagnostics and deny destructive commands.",
  "enabled": true,
  "rules": [
    {
      "id": "deny-dangerous",
      "effect": "deny",
      "command_template": "shutdown",
      "reason": "Dangerous destructive command."
    },
    {
      "id": "allow-diagnostics",
      "effect": "allow",
      "command_template": "hostname",
      "reason": "Known read-only diagnostic command."
    }
  ]
}
```

See [`schemas/proxyma.policy.schema.json`](../schemas/proxyma.policy.schema.json)
and the checked-in example
[`docs/examples/policy-baseline-readonly.json`](examples/policy-baseline-readonly.json).

LLM provider profiles are managed from Settings in the WebGUI and are stored
with their API keys under the configured `llm_profile_dir` (by default
`var/proxyma/llm_profiles`). API keys are never returned by the API. Policies
refer to a profile ID and contain the prompt used for the fallback decision.
OpenAI, OpenRouter, Claude, and custom HTTPS endpoints are supported. Provider
requests must return a JSON object containing `decision` and a non-empty
`justification` of at most 1,000 characters. The decision must be `approved`,
`denied`, or `require approval`; every malformed or incomplete response is
treated as approval required. The justification is explanatory and does not
override the decision. It is persisted with the task, shown in approval
interactions and interaction history, and included in debug logs.

With `--debug`, Proxyma logs the LLM profile, provider, model, user-defined
prompt, HTTP status, duration, normalized decision, and a truncated provider
response to help troubleshoot integrations. Commands and API keys are not
logged. Because prompts and provider responses may contain sensitive text,
debug logs should be protected and rotated like other operational logs.

## Agents

An agent is a trusted API caller. It has an Agent JWT Signing Key used as
symmetric HS256 key material for short-lived JWTs, a list of owning users, and
an optional interaction instance. The signing key itself is never sent in the
`Authorization` header.

```json
{
  "$schema": "../schemas/proxyma.agent.schema.json",
  "id": "df9ec8bb-1a22-4181-a7c8-126ad553221d",
  "display_name": "Operations harness",
  "agent_jwt_signing_key": "replace-with-a-managed-signing-key",
  "enabled": true,
  "user_ids": ["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],
  "interaction_instance_id": "local"
}
```

Omitting `interaction_instance_id` selects the built-in `local` console
interaction. See
[`schemas/proxyma.agent.schema.json`](../schemas/proxyma.agent.schema.json).

## Users

Users authenticate to the GUI and user API. Active users have an Argon2id
password hash. Pending activation and password-reset records omit the hash
until the user chooses a password through the one-time link.

```json
{
  "$schema": "../schemas/proxyma.user.schema.json",
  "id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "username": "operator",
  "name": "Operations User",
  "admin": true,
  "status": "active",
  "passwd_hash": "$argon2id$v=19$m=19456,t=2,p=1$..."
}
```

For bootstrap, `proxymactl users create --password-stdin` hashes the initial
password and creates an active user. `proxymactl users reset-password
<user-id> --password-stdin` changes the password and revokes existing sessions.
See [`schemas/proxyma.user.schema.json`](../schemas/proxyma.user.schema.json).

## Interaction Instances

The local console interaction is built in as the reserved `local` instance. Do
not create a `var/proxyma/interaction_instances/local/` directory. Configure
ntfy instances as individual files under
`var/proxyma/interaction_instances/ntfy/`:

```json
{
  "$schema": "../../../schemas/proxyma.interaction.ntfy.schema.json",
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "creator_user_id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "display_name": "Operations phone",
  "enabled": true,
  "base_url": "https://ntfy.sh",
  "request_topic": "replace-with-request-topic",
  "response_topic": "replace-with-response-topic",
  "token": "",
  "poll_interval": "5s",
  "poll_timeout": "30s",
  "responded_by": "operations-phone"
}
```

An instance's creator and users who own referencing agents may view or edit it.
Deletion is blocked while an agent references the instance. See
[`schemas/proxyma.interaction.ntfy.schema.json`](../schemas/proxyma.interaction.ntfy.schema.json).

## Runtime Settings

Settings are merged in this order:

```text
hardcoded defaults < proxyma.config.json < PROXYMA_* environment variables < CLI arguments
```

The tracked
[`proxyma.config.example.json`](../proxyma.config.example.json) is exhaustive
and references
[`schemas/proxyma.config.schema.json`](../schemas/proxyma.config.schema.json)
for validation and editor completion. Copy it to the ignored
`proxyma.config.json` for machine-specific configuration.

The HTTP server also applies fixed resource safeguards that are not configurable:
10 seconds to read request headers, 30 seconds to read a request, 180 seconds
to write a response, 120 seconds for idle keep-alive connections, and a 64 KiB
maximum header size. Request bodies are capped at 1 MiB globally; individual
endpoints may impose smaller limits. The 180-second write timeout accommodates
the maximum 120-second LLM profile test timeout while still bounding stuck
responses.

The supported environment variables are:

```text
PROXYMA_LISTEN
PROXYMA_SCHEME
PROXYMA_TLS_MODE
PROXYMA_TLS_CERT_FILE
PROXYMA_TLS_KEY_FILE
PROXYMA_TLS_SERVER_NAME
PROXYMA_TARGET_DIR
PROXYMA_POLICY_DIR
PROXYMA_AGENT_DIR
PROXYMA_USER_DIR
PROXYMA_DATABASE_PATH
PROXYMA_INTERACTION_CONFIG_DIR
PROXYMA_USER_SESSION_DIR
PROXYMA_CREDENTIAL_TOKEN_DIR
PROXYMA_KNOWN_HOSTS_PATH
PROXYMA_COMMAND_TIMEOUT
PROXYMA_INTERACTION_TIMEOUT
PROXYMA_USER_SESSION_TTL
PROXYMA_USER_ACTIVATION_TOKEN_TTL
PROXYMA_USER_ACTIVATION_TOKEN_MAX_TTL
PROXYMA_OUTPUT_LIMIT
PROXYMA_REQUIRE_COMMAND_APPROVAL
PROXYMA_DEBUG
```

Duration values use Go syntax such as `30s`, `2m`, or `1h`. Boolean values use
Go boolean syntax. `--config` and `--no-config` are CLI-only controls. SQLite migrations
run automatically when Proxyma starts; commands and interactions are stored directly in
the canonical database rather than imported from JSON files.

The complete CLI argument table is in the
[project README](../README.md#runtime-configuration).
