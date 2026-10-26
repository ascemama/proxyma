# Unified History Search Architecture

## Problem

Proxyma persists each durable task and human interaction as JSON records. Those files
remain the lifecycle and recovery source of truth, but they are not a good production
query model for the GUI. A production instance can have thousands of commands and
interaction messages, and the GUI must not scan every JSON file for every page or filter
change.

The GUI uses one History page where a user can browse and search command tasks and
human interactions visible through their assigned agents and targets.

## Current State

The durable interaction store remains intentionally small:

```go
type Store interface {
	LoadAll() ([]Interaction, error)
	Save(interaction Interaction) error
}
```

`interaction.Service` and `tasks.Manager` use their JSON stores for recovery and
lifecycle updates. The query index contains safe projections of both record types.

Interaction instance configuration now uses `creator_user_id`. That creator is metadata
for the NTFY/local provider instance. It is not the authorization index for message
history.

## Visibility Model

Interaction history visibility is based on agent ownership:

1. Join `interaction.Interaction.TaskID` to the durable task record.
2. Read `task.AgentID`.
3. Look up the current agent definition.
4. Insert one visibility row for each `agent.UserIDs` entry.

Non-admin GUI users can only query rows where their user ID appears in
`history_entries_visible_users`. Admin users can query all rows and may filter by
interaction instance creator for audit purposes.

Command history preserves the previous command-history visibility behavior: agent users
and users owning the matched target can see the command. Admin users can query all rows.

Visibility is materialized with its source. Command rows have an `agent` source and,
when the task target matches a managed target, a `target` source. Interaction rows have
only the `agent` source. The history API checks these rows with an `EXISTS` predicate so
a user visible through both sources still receives one result.

## Implemented Backend Shape

The query boundary is separate from the lifecycle store:

```go
type QueryStore interface {
	ListHistory(ctx context.Context, query HistoryQuery) (HistoryPage, error)
}

type Indexer interface {
	UpsertCommand(ctx context.Context, record IndexedCommand) error
	UpsertInteraction(ctx context.Context, record IndexedInteraction) error
	DeleteAll(ctx context.Context) error
}

type VisibilitySynchronizer interface {
	SyncAgentVisibility(ctx context.Context, agentID string, userIDs []string) error
	SyncTargetVisibility(ctx context.Context, targetID string, userIDs []string) error
}
```

The SQLite implementation lives under `internal/store/sqlitehistory`. JSON task and
interaction stores remain the source of truth for lifecycle recovery.

Runtime incremental indexing is handled by wrapping both JSON stores. Each wrapper saves
the JSON record first, then best-effort upserts its safe SQLite history projection. The
interaction indexer joins the interaction to its task; the task indexer indexes one row
per task and updates that row as the task progresses.

Existing JSON data can be imported by starting Proxyma with:

```text
--rebuild-interaction-history
```

The rebuild path clears the SQLite index, loads all task records, indexes commands,
loads interactions, joins them to tasks, and repopulates the unified history rows.
Historical commands are imported by running this flag once after upgrading.

After the one-time migration rebuild, agent and target ownership changes do not rebuild
the JSON stores. The management API transactionally replaces only the visibility rows
for the changed agent or target. The indexing wrappers also receive the refreshed
registries so new task and interaction saves use the same permissions.

## SQLite Schema

The configured database path defaults to:

```text
var/proxyma/interaction-history.db
```

It can be overridden with:

```text
--interaction-history-db
PROXYMA_INTERACTION_HISTORY_DB
paths.interaction_history_db
```

Main table:

```sql
create table history_entries (
	history_id text primary key,
	entry_type text not null check (entry_type in ('command', 'interaction')),
	interaction_id text,
	task_id text,
	agent_id text,
	agent_name text,
	interaction_instance_id text,
	interaction_instance_creator_user_id text,
	provider text,
	kind text,
	expected_response text,
	status text not null,
	subject text,
	message text,
	requested_by text,
	approved integer,
	answer text,
	responded_by text,
	cancel_reason text,
	target_hostname text,
	target_ip text,
	target_id text,
	command text not null default '',
	result text,
	policy_justification text,
	created_at text not null,
	completed_at text,
	expires_at text,
	responded_at text,
	task_created_at text,
	record_json text
);
```

Visibility table:

```sql
create table history_entries_visible_users (
	history_id text not null,
	user_id text not null,
	source_type text not null check (source_type in ('agent', 'target')),
	source_id text not null,
	primary key (history_id, user_id, source_type, source_id)
);
```

`history_id` is prefixed with `command:` or `interaction:`. Indexes support entry type,
agent, target, status, provider instance, kind, visible user, and created-time pagination.
SQLite FTS5 indexes `subject`, `message`, `command`, `result`, `requested_by`,
`target_hostname`, and `target_ip`.

When an existing database contains `interaction_history`, startup migrates its rows and
visibility entries transactionally into `history_entries` before using the new schema.
Existing unified databases are migrated to the source-aware visibility schema. Run
`--rebuild-interaction-history` once after upgrading so historical rows receive their
managed target IDs and correct agent/target visibility sources.

## API

The GUI endpoint is:

```text
GET /v1/user/history
```

Supported query parameters:

```text
limit=50
cursor=<opaque cursor>
entry_type=command|interaction
agent_id=<agent-id>
interaction_instance_id=<instance-id>
interaction_instance_creator_user_id=<admin-only-user-id>
status=<command-or-interaction-status>
kind=approval_request,question
target=<hostname-or-ip-fragment>
created_from=<rfc3339>
created_to=<rfc3339>
q=<full-text-query>
command_pattern=<literal-glob-or-regex>
command_pattern_mode=literal|glob|regex
```

The response contains `items` and `next_cursor`. Each item contains `entry_type`, a
stable `history_id`, shared task/agent/target fields, and type-specific command or
interaction fields. Cursor pagination uses `(created_at, history_id)` in descending
order.

Regex command searches are bounded. The API rejects a regex search unless it includes an
`agent_id` or a full `created_from` / `created_to` interval.

## GUI

The History page is separate from interaction provider-instance management. It follows
the selected split-detail layout:

- Left filter rail: search, agent, status, kind, provider instance, creator for admins,
  target, date interval, command pattern, and pattern mode.
- Center result table: time, type, status, agent, kind, command, and target.
- Right detail panel: type-aware command or interaction details, safe result summary,
  approval/result fields, timestamps, task ID, and optional interaction ID.

The page uses the existing Proxyma panel, table, badge, button, and light/dark theme tokens.

## Performance Expectations

Non-admin queries always include an indexed user visibility predicate. Common filters such
as "my completed commands and answered approval requests for agent X in this date
range" avoid scanning JSON files and should return within a few seconds for tens of
thousands of rows.

FTS handles broad text search. Regex remains an advanced second-stage filter after SQL
has narrowed the candidate set.
