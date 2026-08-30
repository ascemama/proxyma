# Client Task State v2

`proxyma-agent` is the only writer of harness task state. Each logical task has one
authoritative JSON file at `task-state/tasks/<client_task_id>.json` and one derived
Markdown summary. Agents must use `inspect`, `task-update`, `submit`, and
`task-complete` rather than reading or editing these files.

The JSON schema is `task-state.schema.json`. A monotonic revision protects every
agent-requested mutation. Operations preserve the exact Proxyma request and operation
ID before network submission, allowing `resume` to retry a crash or uncertain response
without creating a duplicate Proxyma command.

Task statuses are `pending`, `waiting_proxyma`, `completed`, `blocked`, and `failed`.
Raw Proxyma status remains on each operation and is never substituted for logical task
state. JSON is authoritative; summaries can always be regenerated.

Harness v1 directory-based state and schemas are unsupported. `proxyma-agent doctor`
reports legacy state so it can be archived or removed explicitly.
