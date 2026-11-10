---
name: proxyma-harness
description: Use Proxyma for commands on managed target machines and resume durable Proxyma work.
---

# Proxyma Harness for Claude Code

Read `harness/instruction-pack/proxyma-harness.md` from the project root and follow it
as the authoritative policy. The shared `harness/` directory is a sibling of this
`.claude/` adapter; do not look for `instruction-pack/` inside `.claude/`. Use
`proxyma-client` for authentication, target resolution, submission,
polling, duplicate prevention, and task-state changes.

Read `harness/proxyma-client.json` and use its optional `harness_name` value in
every submission. If it is absent, use `claude`. Never expose credentials or other
configuration values.

Start relevant work with `proxyma-client resume` and continue actionable saved tasks
first. Submit only a remote payload—not an SSH or WinRM wrapper—with the configured
`harness_name`. Preserve the returned task ID and revision. Interpret results
yourself and call `task-complete` only after satisfying the user goal.

Never directly access a target, read credentials, edit CLI-owned state, or replace an
uncertain submission. Use `proxyma-client doctor` for configuration or connectivity
failures.
