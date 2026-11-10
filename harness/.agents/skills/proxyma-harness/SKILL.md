---
name: proxyma-harness
description: Use Proxyma for commands on managed target machines and resume durable Proxyma work.
---

# Proxyma Harness for Codex

From the project root, read the authoritative policy using the first applicable
exact path below:

1. `harness/instruction-pack/proxyma-harness.md` when `harness/` is a sibling of
   this `.agents/` adapter.
2. `instruction-pack/proxyma-harness.md` when this `harness/` directory is the
   project root.

Resolve the selected path relative to the project root, not relative to this
`SKILL.md` file; do not search for the file by basename alone. If neither exact
path can be read, stop and report the problem before running any Proxyma commands.
Do not look for `instruction-pack/` inside `.agents`. Use
`proxyma-client` for authentication, target resolution,
submission, polling, duplicate prevention, and task-state changes.

Read `harness/proxyma-client.json` and use its optional `harness_name` value in
every submission. If it is absent, use `codex`. Never expose credentials or other
configuration values.

Start relevant work with `proxyma-client resume` and continue actionable saved tasks
first. Submit only a remote payload—not an SSH or WinRM wrapper—with the configured
`harness_name`. Preserve the returned task ID and revision. Interpret results
yourself and call `task-complete` only after satisfying the user goal.

Never directly access a target, read credentials, edit CLI-owned state, or replace an
uncertain submission. Use `proxyma-client doctor` for configuration or connectivity
failures.
