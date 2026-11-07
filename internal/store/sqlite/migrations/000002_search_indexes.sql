-- FTS5 search indexes are maintained transactionally by the repositories.
CREATE VIRTUAL TABLE commands_fts USING fts5(
    task_id UNINDEXED,
    command,
    reasoning,
    requested_by,
    target_hostname,
    target_ip,
    result
);

CREATE VIRTUAL TABLE interactions_fts USING fts5(
    interaction_id UNINDEXED,
    task_id UNINDEXED,
    subject,
    message,
    answer,
    requested_by
);

INSERT INTO commands_fts(task_id, command, reasoning, requested_by, target_hostname, target_ip, result)
SELECT task_id, command, reasoning, requested_by, target_hostname, target_ip, result_class
FROM commands;

INSERT INTO interactions_fts(interaction_id, task_id, subject, message, answer, requested_by)
SELECT interaction_id, COALESCE(task_id, ''), subject, message, answer, requested_by
FROM interactions;
