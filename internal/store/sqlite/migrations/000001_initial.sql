-- Canonical durable command records. Typed columns serve recovery and GUI filters;
-- record_json preserves the complete evolving task shape.
CREATE TABLE commands (
    task_id TEXT PRIMARY KEY,
    client_operation_id TEXT NOT NULL DEFAULT '',
    client_request_hash TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL,
    agent_name TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL DEFAULT '',
    target_hostname TEXT NOT NULL DEFAULT '',
    target_ip TEXT NOT NULL DEFAULT '',
    target_transport TEXT NOT NULL DEFAULT '',
    target_os TEXT NOT NULL DEFAULT '',
    target_policy_id TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL,
    reasoning TEXT NOT NULL,
    requested_by TEXT NOT NULL DEFAULT '',
    interaction_instance_id TEXT NOT NULL,
    status TEXT NOT NULL,
    policy_justification TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    result_class TEXT NOT NULL DEFAULT '',
    record_json TEXT NOT NULL
);

-- Only non-empty client operation IDs participate in idempotency uniqueness.
CREATE UNIQUE INDEX commands_operation_unique_idx
    ON commands(agent_id, client_operation_id)
    WHERE client_operation_id <> '';

-- Common history sort and filter paths.
CREATE INDEX commands_created_idx ON commands(created_at DESC, task_id DESC);
CREATE INDEX commands_status_created_idx ON commands(status, created_at DESC, task_id DESC);
CREATE INDEX commands_agent_created_idx ON commands(agent_id, created_at DESC, task_id DESC);
CREATE INDEX commands_target_created_idx ON commands(target_id, created_at DESC, task_id DESC);
CREATE INDEX commands_instance_created_idx ON commands(interaction_instance_id, created_at DESC, task_id DESC);

-- Canonical durable human-interaction records. The task foreign key links an
-- interaction to its command without duplicating command lifecycle ownership.
CREATE TABLE interactions (
    interaction_id TEXT PRIMARY KEY,
    task_id TEXT,
    interaction_instance_id TEXT NOT NULL,
    interaction_instance_creator_user_id TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    expected_response TEXT NOT NULL,
    subject TEXT NOT NULL,
    message TEXT NOT NULL,
    requested_by TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT,
    responded_at TEXT,
    approved INTEGER,
    answer TEXT NOT NULL DEFAULT '',
    responded_by TEXT NOT NULL DEFAULT '',
    cancel_reason TEXT NOT NULL DEFAULT '',
    delivery_reference TEXT NOT NULL DEFAULT '',
    delivered_at TEXT,
    record_json TEXT NOT NULL,
    FOREIGN KEY (task_id) REFERENCES commands(task_id) ON DELETE SET NULL
);

-- Common history sort and filter paths.
CREATE INDEX interactions_created_idx ON interactions(created_at DESC, interaction_id DESC);
CREATE INDEX interactions_status_created_idx ON interactions(status, created_at DESC, interaction_id DESC);
CREATE INDEX interactions_kind_created_idx ON interactions(kind, created_at DESC, interaction_id DESC);
CREATE INDEX interactions_instance_created_idx ON interactions(interaction_instance_id, created_at DESC, interaction_id DESC);
CREATE INDEX interactions_task_idx ON interactions(task_id);
