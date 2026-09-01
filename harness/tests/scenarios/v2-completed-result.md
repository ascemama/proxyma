# Continue a completed result

When Proxyma reports `completed`, the CLI records the raw result, changes the logical
task to `pending`, and returns `action: continue`. The agent interprets the result and
either submits another payload or calls `task-complete`.
