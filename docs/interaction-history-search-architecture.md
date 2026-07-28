# Interaction History Search Architecture

## Problem

Proxyma persists each durable human interaction as one JSON file under
`var/proxyma/interactions`. That remains a good lifecycle and recovery format, but it is
not a good production query model for the GUI. A production instance can have 10,000+
interaction messages, and the GUI must not scan every JSON file for every page or
filter change.

The GUI needs a dedicated history page where a user can browse and search all
interaction messages visible through their assigned agents.

## Current State

The durable interaction store remains intentionally small:

```go
type Store interface {
	LoadAll() ([]Interaction, error)
	Save(interaction Interaction) error
}
```

`interaction.Service` uses that store for recovery and lifecycle updates. Task data
needed by search, such as `agent_id`, target, command, and task creation time, lives on
task records.

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
`interaction_history_visible_users`. Admin users can query all rows and may filter by
interaction instance creator for audit purposes.

Target ownership does not grant interaction-history visibility in this version. The
history page answers "show me the interaction history for my agents".

## Implemented Backend Shape

The query boundary is separate from the lifecycle store:

```go
type QueryStore interface {
	ListInteractions(ctx context.Context, query InteractionQuery) (InteractionPage, error)
}

type Indexer interface {
	UpsertInteraction(ctx context.Context, record IndexedInteraction) error
	DeleteAll(ctx context.Context) error
}
```

The SQLite implementation lives under `internal/store/sqlitehistory`. JSON task and
interaction stores remain the source of truth for lifecycle recovery.

Runtime incremental indexing is handled by wrapping the JSON interaction store. The
wrapper saves the JSON record first, then best-effort upserts the SQLite history row if
the linked task exists.

Existing JSON data can be imported by starting Proxyma with:

```text
--rebuild-interaction-history
```

The rebuild path clears the SQLite index, loads JSON interactions and tasks, joins them,
and repopulates the indexed history rows.

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
create table interaction_history (
	interaction_id text primary key,
	task_id text not null,
	agent_id text not null,
	agent_name text,
	interaction_instance_id text not null,
	interaction_instance_creator_user_id text,
	provider text,
	kind text not null,
	expected_response text not null,
	status text not null,
	subject text not null,
	message text not null,
	requested_by text,
	approved integer,
	answer text,
	responded_by text,
	cancel_reason text,
	target_hostname text,
	target_ip text,
	command text not null,
	created_at text not null,
	expires_at text,
	responded_at text,
	task_created_at text not null,
	record_json text not null
);
```

Visibility table:

```sql
create table interaction_history_visible_users (
	interaction_id text not null,
	user_id text not null,
	primary key (interaction_id, user_id)
);
```

Indexes support agent, status, provider instance, kind, and created-time pagination.
SQLite FTS5 indexes `subject`, `message`, `command`, `requested_by`,
`target_hostname`, and `target_ip`.

## API

The GUI endpoint is:

```text
GET /v1/user/interaction-history
```

Supported query parameters:

```text
limit=50
cursor=<opaque cursor>
agent_id=<agent-id>
interaction_instance_id=<instance-id>
interaction_instance_creator_user_id=<admin-only-user-id>
status=pending,answered,expired,canceled
kind=approval_request,question
target=<hostname-or-ip-fragment>
created_from=<rfc3339>
created_to=<rfc3339>
q=<full-text-query>
command_pattern=<literal-glob-or-regex>
command_pattern_mode=literal|glob|regex
```

The response contains `items` and `next_cursor`. Cursor pagination uses
`(created_at, interaction_id)` in descending order.

Regex command searches are bounded. The API rejects a regex search unless it includes an
`agent_id` or a full `created_from` / `created_to` interval.

## GUI

The interaction history page is separate from interaction provider-instance management.
It follows the selected split-detail layout:

- Left filter rail: search, agent, status, kind, provider instance, creator for admins,
  target, date interval, command pattern, and pattern mode.
- Center result table: time, status, agent, kind, command, and target.
- Right detail panel: subject, status, agent, target, provider, command, message,
  approval/result fields, timestamps, task ID, and interaction ID.

The page uses the existing Proxyma panel, table, badge, button, and light/dark theme tokens.

## Performance Expectations

Non-admin queries always include the indexed user visibility join. Common filters such
as "my answered approval requests for agent X in this date range" avoid scanning JSON
files and should return within a few seconds for tens of thousands of rows.

FTS handles broad text search. Regex remains an advanced second-stage filter after SQL
has narrowed the candidate set.
