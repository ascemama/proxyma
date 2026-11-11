-- Rename the pre-public harness identity field while preserving existing local data.
ALTER TABLE commands RENAME COLUMN requested_by TO harness_name;
ALTER TABLE interactions RENAME COLUMN requested_by TO harness_name;

-- Existing durable records store a JSON copy of the task/interaction shape.
-- JSON1 is part of the SQLite build used by Proxyma; only rows containing the
-- old key are rewritten.
UPDATE commands
SET record_json = json_set(
    json_remove(record_json, '$.requested_by'),
    '$.harness_name',
    json_extract(record_json, '$.requested_by')
)
WHERE json_type(record_json, '$.requested_by') IS NOT NULL;

UPDATE interactions
SET record_json = json_set(
    json_remove(record_json, '$.requested_by'),
    '$.harness_name',
    json_extract(record_json, '$.requested_by')
)
WHERE json_type(record_json, '$.requested_by') IS NOT NULL;

-- Rebuild FTS tables so their public column names and indexed values match the
-- renamed durable columns.
ALTER TABLE commands_fts RENAME TO commands_fts_requested_by;
CREATE VIRTUAL TABLE commands_fts USING fts5(
    task_id UNINDEXED,
    command,
    reasoning,
    harness_name,
    target_hostname,
    target_ip,
    result
);
INSERT INTO commands_fts(task_id, command, reasoning, harness_name, target_hostname, target_ip, result)
SELECT task_id, command, reasoning, requested_by, target_hostname, target_ip, result
FROM commands_fts_requested_by;
DROP TABLE commands_fts_requested_by;

ALTER TABLE interactions_fts RENAME TO interactions_fts_requested_by;
CREATE VIRTUAL TABLE interactions_fts USING fts5(
    interaction_id UNINDEXED,
    task_id UNINDEXED,
    subject,
    message,
    answer,
    harness_name
);
INSERT INTO interactions_fts(interaction_id, task_id, subject, message, answer, harness_name)
SELECT interaction_id, task_id, subject, message, answer, requested_by
FROM interactions_fts_requested_by;
DROP TABLE interactions_fts_requested_by;
