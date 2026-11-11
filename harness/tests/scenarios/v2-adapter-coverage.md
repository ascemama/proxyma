# Codex and Claude Code adapters

Both adapters load the same neutral policy, start with `proxyma-client resume`, submit
payloads with their own `harness_name` value, preserve task identity and revision, and
never edit task-state files directly.
