# Unified history search architecture

Proxyma uses SQLite as the canonical store for commands and asynchronous human
interactions. JSON remains configuration storage for users, agents, targets,
policies, LLM profiles, sessions, and interaction-instance provider settings.
This avoids scanning thousands of files while keeping the domain storage
interfaces independent from the database implementation.

## Interfaces

The lifecycle boundaries remain small:

```go
type tasks.Store interface {
    LoadAll() ([]tasks.Task, error)
    Save(tasks.Task) error
}

type interaction.Store interface {
    LoadAll() ([]interaction.Interaction, error)
    Save(interaction.Interaction) error
}

type history.QueryStore interface {
    ListHistory(context.Context, history.HistoryQuery) (history.HistoryPage, error)
}
```

The SQLite package has separate `CommandStore`, `InteractionStore`, and
`HistoryStore` types over one database handle. A future backend can implement
the same lifecycle and query interfaces without changing task or interaction
services.

## Database and migrations

The default database is `var/proxyma/database.db`. Startup creates its parent
directory, enables foreign keys, WAL mode, and a five-second busy timeout, then
applies embedded sequential migrations. `schema_migrations` records each
successful version transactionally. A newer unsupported schema stops startup.

The initial schema contains:

- `commands`: typed fields for status, agent, target, dates, command text, and
  result classification plus `record_json` for the complete evolving task;
- `interactions`: typed lifecycle, response, delivery, provider, and creator
  fields plus `record_json` for complete recovery state;
- a foreign key from `interactions.task_id` to `commands.task_id`;
- idempotency and GUI filtering indexes;
- `commands_fts` and `interactions_fts` FTS5 tables.

There is no `history_entries` projection table. History queries use a fixed
UNION over `commands` and `interactions`, so updates are visible immediately.

## Visibility

The API resolves the current user’s scope from the current JSON registries on
each history request:

```go
type VisibilityScope struct {
    Admin     bool
    AgentIDs  []string
    TargetIDs []string
}
```

Non-admin command rows are visible when their stored agent or target ID is in
the scope. Interaction rows are visible through their related command. Admins
can query all rows and use the interaction-instance creator filter. Ownership
changes therefore take effect without rebuilding or rewriting history rows.

## Query safety

Every runtime value is bound through `database/sql` prepared statements. Dynamic
history predicates are assembled only from fixed internal fragments; `IN` lists
generate placeholders, sort order and column names are whitelisted, LIKE/glob
patterns are escaped and bound, and FTS text is passed as a bound quoted value.
Regex matching is compiled by Go after the SQL candidate set is loaded. Values
are never interpolated into SQL.

## API filters

`GET /v1/user/history` supports cursor pagination, command or interaction entry
types, status, kind, agent, target, interaction instance, date ranges, FTS
search, and literal/glob/regex command matching. Cursors encode the descending
`(created_at, history_id)` position.

See [database operations](database-operations.md) for backup, restore, and safe
from-scratch reset procedures.
