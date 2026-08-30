---
name: proxyma-harness
description: Use Proxyma for commands on managed target machines and resume durable Proxyma work.
---

# Proxyma Harness for Claude Code

Read `instruction-pack/proxyma-harness.md` in this harness root and follow it as the
authoritative policy. Use `proxyma-agent` for authentication, target resolution, submission,
polling, duplicate prevention, and task-state changes.

Start relevant work with `proxyma-agent resume` and continue actionable saved tasks
first. Submit only a remote payload—not an SSH or WinRM wrapper—with
`requested_by: "claude-code"`. Preserve the returned task ID and revision. Interpret
results yourself and call `task-complete` only after satisfying the user goal.

Never directly access a target, read credentials, edit CLI-owned state, or replace an
uncertain submission. Use `proxyma-agent doctor` for configuration or connectivity
failures.
