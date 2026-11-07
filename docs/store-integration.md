# Storage Integration

This document explains Proxyma's storage interface and how to add a new storage backend,
such as a database, S3-compatible object storage, Azure Blob Storage, or another durable
store.

## Current Boundary

Proxyma separates task and interaction lifecycle code from the concrete persistence
backend. The domain packages define typed store interfaces for the records they own.

Task storage is defined in `internal/tasks/store.go`:

```go
type Store interface {
	LoadAll() ([]Task, error)
	Save(task Task) error
}
```

Interaction storage is defined in `internal/interaction/store.go`:

```go
type Store interface {
	LoadAll() ([]Interaction, error)
	Save(interaction Interaction) error
}
```

These interfaces are intentionally small. `internal/tasks.Manager` and
`internal/interaction.Service` keep in-memory maps for lookup and lifecycle decisions.
The backend only needs to load all durable records during recovery and save one complete
record after each lifecycle change.

This keeps backend concerns out of taskflow, API handlers, schedulers, and interaction
response handling.

## Default SQLite Backend

The default backend is `internal/store/sqlite`. One `Database` handle is shared by
three separate repositories:

```go
database, _ := sqlite.Open(cfg.DatabasePath)
commandStore := database.NewCommandStore()
interactionStore := database.NewInteractionStoreWithRegistry(interactionRegistry)
historyStore := database.NewHistoryStore()
```

`CommandStore` implements `tasks.Store`, `InteractionStore` implements
`interaction.Store`, and `HistoryStore` implements `history.QueryStore`. The command
and interaction tables are canonical; the history repository queries those tables
directly and does not maintain a projection table. See
`docs/interaction-history-search-architecture.md` for the schema and query model.

JSON filesystem helpers remain available for configuration resources such as users,
agents, targets, policies, sessions, LLM profiles, and interaction-instance provider
definitions. They are not the runtime source of truth for commands or interactions.

## Backend Responsibilities

Any storage backend must provide these semantics:

- Persist the complete task or interaction record after every lifecycle change.
- Load all existing records during `LoadAll`.
- Treat the Proxyma-generated `TaskID` or `InteractionID` as the durable primary key.
- Preserve JSON-compatible fields and UTC timestamps enough for API responses and
  recovery.
- Return clear errors when records cannot be loaded, decoded, encoded, or saved.
- Avoid persisting fields that are intentionally excluded from JSON, such as execution
  plans and sensitive values in `tasks.Task`.
- Keep task records and interaction records separate at the interface level, even if the
  backend uses one physical database or bucket.

The store should not:

- Start tasks, apply interactions, expire interactions, or perform lifecycle decisions.
- Deliver human interaction messages.
- Log secrets or internal execution plans.
- Depend on HTTP request handlers or the scheduler.

## Adding a Backend

Create a backend package under `internal/store` or a backend-specific package. Examples:

```text
internal/store/sqlite/
internal/store/postgres/
internal/store/s3/
```

Prefer separate task and interaction repository types:

```go
type TaskStore struct { /* db, bucket, client, prefix */ }
type InteractionStore struct { /* db, bucket, client, prefix */ }
```

Both can share lower-level connection, migration, or object-client code. The important
part is that they implement `tasks.Store` and `interaction.Store`.

Backend implementation steps:

1. Implement `LoadAll` and `Save` for task records.
2. Implement `LoadAll` and `Save` for interaction records.
3. Add backend contract tests.
4. Add configuration for selecting the backend.
5. Update `cmd/proxyma/main.go` to construct the selected stores and pass them into
   `tasks.NewManagerWithOptions` and `interaction.New`.

Do not make task or interaction packages parse backend flags. They should receive ready
store implementations.

## Database Backend

For SQL or document databases:

- Use `task_id` and `interaction_id` as primary keys.
- Store timestamps in UTC.
- Store the full public JSON record in one JSON column initially, or map fields into
  typed columns when querying by status becomes necessary.
- Use transactions or atomic upserts for `Save`.
- Add numbered migrations for schema changes.
- Use prepared statements and parameter binding for every runtime value.
- Add indexes for fields used by GUI filters and cursor pagination.

The current manager keeps an in-memory map and only needs `LoadAll` plus `Save`, so the
SQLite repositories use complete-record JSON columns alongside typed query columns.

## Object Storage Backend

For S3, Azure Blob Storage, GCS, or filesystem-like object storage:

- Use stable object keys such as `tasks/<task_id>.json` and
  `interactions/<interaction_id>.json`.
- Write complete records, not patches.
- Ensure listing returns only valid record objects.
- Treat malformed objects as recovery errors unless there is a deliberate quarantine
  policy.
- Prefer conditional writes or object version checks if multiple Proxyma processes may run
  against the same backend.

Object storage is close to the JSON file model, but it should still expose typed
`LoadAll` and `Save` methods rather than leaking object keys into the domain packages.

## Wiring

The current CLI exposes one canonical database path:

```text
--database-path=var/proxyma/database.db
```

A new backend should add backend-specific configuration in `internal/config`. Example
future shapes:

```text
--store-backend=sqlite
--store-dsn=var/proxyma/Proxyma.db

--store-backend=s3
--store-bucket=proxyma-state
--store-prefix=dev/
```

Backend selection should happen once in `cmd/proxyma/main.go`, then the selected
`tasks.Store` and `interaction.Store` should be passed into the existing constructors.

## Tests

Add or reuse backend contract tests for each typed store:

- Empty backend loads as no records.
- Saving one record makes it visible after `LoadAll`.
- Saving the same ID twice replaces the previous record.
- Multiple records round-trip independently.
- Corrupt or undecodable records return a clear error.
- Missing storage starts as an empty store when that is a valid backend state.
- IDs are used as keys and path or object traversal is impossible for file-like stores.

Domain tests for `tasks.Manager` and `interaction.Service` should use in-memory stores
unless they specifically verify storage behavior. SQLite behavior belongs in
`internal/store/sqlite` tests.

## Future Improvements

The storage boundary is now clean enough for new backends. The next useful improvements
depend on the selected backend:

- Add backend selection flags only when a second backend is implemented.
- Add optimistic concurrency if multiple Proxyma processes will write to the same backend.
- Add schema migrations for SQL-style backends.
- SQLite is the canonical command and interaction store, and its history queries read
  directly from the `commands` and `interactions` tables. See
  `docs/interaction-history-search-architecture.md` and
  `docs/database-operations.md` for the schema, migrations, backup, and reset flow.
- Add quarantine or repair tooling if object stores may contain malformed records.
