# Codex and Claude Code adapters

Both adapters load the same neutral policy, start with `proxyma-agent resume`, submit
payloads with their own `requested_by` value, preserve task identity and revision, and
never edit task-state files directly.
