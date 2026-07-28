# Proxyma API Usage

Proxyma exposes separate user and agent API surfaces. Browser users
authenticate with a password-backed session and CSRF token. Agents authenticate
with a short-lived JWT and can resolve targets, submit commands, and inspect
their own tasks and interactions.

For the complete agent request and response contract, use the authoritative
[harness API reference](../harness/instruction-pack/proxyma-api-reference.md)
and its [copyable examples](../harness/instruction-pack/api-examples/).

## User Login and Sessions

Log in with:

```http
POST /v1/user/login
Content-Type: application/json

{"username":"operator","password":"..."}
```

Proxyma verifies the stored Argon2id hash, persists an opaque session, returns a
CSRF token, and sets an HTTP-only cookie. HTTPS uses
`__Host-proxyma_session` with `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`,
no `Domain`, and an expiry matching the session TTL. Explicit HTTP development
mode uses the non-Secure `proxyma_session_dev` cookie.

Successful login and session responses include the safe user profile, CSRF
token, and `build.version` and `build.commit` metadata.

| Operation | Request |
| --- | --- |
| Inspect or refresh the session | `GET /v1/user/session` with the session cookie |
| Log out | `POST /v1/user/logout` with the cookie and `X-CSRF-Token` |
| Change the current password | `POST /v1/user/change-password` with the cookie and `X-CSRF-Token` |
| Update user settings | `PUT /v1/user/settings` with the cookie and `X-CSRF-Token` |

State-changing authenticated user requests require the current CSRF token.

## Activation and Password Reset

Pending users and users with an administrator-generated password-reset link use
the same endpoint:

```http
GET /v1/user/activation
Authorization: Activation <token>
```

The response identifies the purpose as `activation` or `password_reset`.
Complete the operation with:

```http
POST /v1/user/activation
Authorization: Activation <token>
Content-Type: application/json

{"new_password":"..."}
```

Proxyma consumes the link and changes the user status to `active`. Activation
and reset secrets are stored only as SHA-256 hashes in the configured credential
token directory. Each persisted record includes its purpose.

## Agent Authentication

Every agent endpoint, including `GET /v1/health`, requires:

```http
Authorization: Bearer <jwt>
```

The JWT is signed with the configured Agent JWT Signing Key and identifies that
agent. The signing key is symmetric HS256 key material and is never sent
directly in the `Authorization` header. Use the
[agent authentication guide](../harness/instruction-pack/auth/proxyma-auth.md)
and the supplied PowerShell or shell token scripts. Harnesses use 10-minute
HS256 tokens and regenerate once when Proxyma returns `token_expired`.

## Resolve a Target

An agent that knows a hostname or IP resolves the target before composing a
command:

```http
GET /v1/agent/targets/resolve?hostname=app.example
```

or:

```http
GET /v1/agent/targets/resolve?ip=192.0.2.10
```

The safe response includes the target transport, operating-system environment,
canonical command prefix, and payload kind. It does not expose the target
username, password, or private key. The canonical forms are:

| Target | Command prefix | Payload kind |
| --- | --- | --- |
| Linux over SSH | `ssh {user}@host ` | `posix_shell` |
| Windows over SSH | `ssh {user}@host ` | `windows_shell` |
| Windows over WinRM | `winrm {user}@host ` | `powershell` |

## Submit and Poll a Command

Submit a complete canonical access command:

```http
POST /v1/agent/commands
Content-Type: application/json
Authorization: Bearer <jwt>

{
  "target": {
    "hostname": "app.example"
  },
  "command": "ssh {user}@app.example hostname",
  "reasoning": "Identify the target host for diagnostics",
  "requested_by": "operations-agent"
}
```

Proxyma checks that the authenticated agent is listed in the target's
`agent_ids`, verifies that the command destination matches the explicit target,
and stores only the placeholder command. It resolves `{user}` and, for
password-authenticated targets when needed, `{password}` only in the internal
execution plan. A private-key target rejects commands that require
`{password}`.

The submission returns a durable task ID immediately. Policy then allows,
denies, or pauses the task for human approval. Poll with the same agent:

```http
GET /v1/agent/tasks/{task_id}
Authorization: Bearer <jwt>
```

Continue until the task reaches a final state. Results include stdout, stderr,
exit status, errors, and truncation information. An authenticated agent can
read only its own tasks and task-linked interactions.

## WinRM Execution

WinRM targets use Windows PowerShell. Proxyma invokes
`powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand`; the agent
submits the PowerShell payload after the canonical `winrm {user}@host` prefix.
Agents must not run local `Invoke-Command`, `Enter-PSSession`, `winrs`, or
direct WinRM fallbacks.

Prepare the Windows target with PowerShell remoting enabled, the listener port
allowed through Windows Firewall, and the configured account granted remoting
access. HTTPS/5986 also requires a listener certificate trusted by the Proxyma
host. `tls_insecure` skips remote certificate verification but does not change
NTLM authentication.

When diagnosing a WinRM target, enable Proxyma `--debug` logging and use the
GUI's **Test credentials** action. Debug output includes the effective
scheme/port, TLS verification setting, NTLM username form, timeout, exit status,
and a categorized error without logging the username or password. Remember
that `Enter-PSSession` may negotiate Kerberos while Proxyma currently uses
NTLM.
