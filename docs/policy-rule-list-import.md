# Policy Rule-List Import

Proxyma policy management keeps the policy entity separate from reusable rule
lists. A rule-list JSON file is imported into an existing policy; it does not
contain a policy ID, owner, target assignment, or credentials.

The V1 format is:

```json
{
  "$schema": "../../schemas/proxyma.policy-rule-list.schema.json",
  "version": 1,
  "blacklist": [
    {"command_template": "rm -rf {path}", "reason": "Destructive filesystem operation"}
  ],
  "whitelist": [
    {"command_template": "ping {ipv4}", "reason": "Network reachability check"}
  ]
}
```

Supported placeholders are `{ipv4}`, `{ipv6}`, `{hostname}`, `{port}`,
`{path}`, `{relative_path}`, `{service}`, `{username}`, and `{integer}`.
Placeholders must occupy complete command arguments. Shell operators and regular
expressions are not accepted.

Import is append-only. Exact duplicates are skipped and reported. A template
that already exists with the opposite effect is a conflict; the complete import
is rejected without changing the policy. Target assignments are not changed by
an import.

The prepared example is
[`docs/examples/policy-rule-list-safe-diagnostics.json`](examples/policy-rule-list-safe-diagnostics.json).
