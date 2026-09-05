# `proxymactl` Command Reference

`proxymactl` manages Proxyma's on-disk records and transport configuration. It
does not call the running HTTP service. Resource create and update commands
accept `--file <path>` or `--file -`; create input omits `id` because the CLI
generates a UUID v4.

Global configuration flags must appear before the resource and action:

```powershell
.\bin\proxymactl.exe --user-dir .\users users create --file .\new-user.json
```

| Command | Purpose |
| --- | --- |
| `users create --file <path> [--password-stdin]` | Create a user; `--password-stdin` hashes the supplied password with Argon2id and activates the user. |
| `users update <user-id> --file <path>` | Replace a user record while preserving its ID. |
| `users reset-password <user-id> --password-stdin` | Replace the password, activate the user, and revoke existing sessions. |
| `users delete <user-id>` | Delete a user record. |
| `agents create --file <path>` | Create an agent with a generated UUID. |
| `agents update <agent-id> --file <path>` | Replace an agent record while preserving its ID. |
| `agents delete <agent-id>` | Delete an agent record. |
| `targets create --file <path>` | Create a target with a generated UUID. |
| `targets update <target-id> --file <path>` | Replace a target record while preserving its ID. |
| `targets delete <target-id>` | Delete a target record. |
| `certificates validate --cert-file <path> --key-file <path>` | Validate that a PEM certificate and unencrypted private key match and are currently usable. |
| `certificates install --cert-file <path> --key-file <path> --dest-cert-file <path> --dest-key-file <path>` | Validate and atomically install a certificate pair at managed destinations. |
| `config update-transport --file <path> [--scheme <https\|http>] [--tls-mode <self_signed\|provided>] [--tls-cert-file <path> --tls-key-file <path>] [--tls-server-name <name>]` | Update server transport fields in a Proxyma JSON configuration file. |

Record formats, examples, relationships, and schema links are documented in
[Configuration](configuration.md).
