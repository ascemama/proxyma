# Proxyma Configuration

Proxyma keeps operator-managed records in JSON files and validates their
relationships when the service starts. The default directories are `targets/`,
`policies/`, `agents/`, `users/`, and `interactions/`; deployments may override
every path.

Use `proxymactl` to create, update, and delete users, agents, and targets. The
browser GUI can manage the same resources as well as policies and interaction
instances. IDs are UUID v4 values; use human-readable name and description
fields for display.

## Targets

Each target names the policies applied to its commands, the agents authorized
to submit commands, and the users who own it. A target must provide a hostname,
an IP address, or both.

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
  "policy_ids": ["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
  "policy_ids": ["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
  "policy_ids": ["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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

Targets reference one or more command-policy IDs. Policy evaluation can allow a
command, require human approval, or deny it. Deny wins over other matches;
otherwise priority and rule order determine the result.

```json
{
  "$schema": "../schemas/proxyma.policy.schema.json",
  "id": "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
  "name": "Baseline Readonly",
  "description": "Allow diagnostics, deny destructive commands, approve everything else.",
  "priority": 100,
  "enabled": true,
  "rules": [
    {
      "id": "deny-dangerous",
      "effect": "deny",
      "remote_command_regex": "(?i)\\b(rm\\s+-rf|shutdown|reboot|mkfs|dd\\b)",
      "reason": "Dangerous destructive command."
    },
    {
      "id": "allow-diagnostics",
      "effect": "allow",
      "remote_command_prefix_any": ["hostname", "uptime", "df -h", "free -m"],
      "reason": "Known read-only diagnostic command."
    },
    {
      "id": "approve-default",
      "effect": "approval",
      "always": true,
      "reason": "Command requires human approval."
    }
  ]
}
```

See [`schemas/proxyma.policy.schema.json`](../schemas/proxyma.policy.schema.json)
and the checked-in
[`policies/baseline-readonly.json`](../policies/baseline-readonly.json).

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
not create an `interactions/local/` directory. Configure ntfy instances as
individual files under `interactions/ntfy/`:

```json
{
  "$schema": "../../schemas/proxyma.interaction.ntfy.schema.json",
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
PROXYMA_TASK_DIR
PROXYMA_INTERACTION_DIR
PROXYMA_INTERACTION_HISTORY_DB
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
Go boolean syntax. `--config`, `--no-config`, and
`--rebuild-interaction-history` are CLI-only controls.

The complete CLI argument table is in the
[project README](../README.md#runtime-configuration).
