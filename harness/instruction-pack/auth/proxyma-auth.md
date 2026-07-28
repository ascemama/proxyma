# Proxyma Agent Authentication

Every Proxyma API request must include a short-lived bearer token:

```http
Authorization: Bearer <jwt>
```

The harness reads its agent configuration from the `agent_config` path in
`harness/proxyma-harness-run.json`. The config contains `agent_id` and `shared_secret`.
Generate a new HS256 JWT before the first Proxyma call, reuse it until it is near expiry,
and regenerate it when the token is missing, expired, or Proxyma returns `401` with code
`token_expired`.

JWT requirements:

- Header includes `alg` = `HS256`, `typ` = `JWT`, and `kid` = agent ID.
- Payload includes `sub` = agent ID, `iat`, and `exp`.
- `exp` must be no more than 10 minutes after `iat`.

Use `new-proxyma-jwt.ps1` on PowerShell hosts and `new-proxyma-jwt.sh` on Bash hosts.

For powershell use the following command line :  powershell.exe -ExecutionPolicy Bypass -File .\harness\instruction-pack\auth\new-proxyma-jwt.ps1
