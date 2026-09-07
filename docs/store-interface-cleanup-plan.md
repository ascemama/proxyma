# Store Interface Cleanup Implementation Notes

Status: implemented.

This document records the source cleanup that made Proxyma storage replacement a
constructor wiring choice, not a task or interaction lifecycle change.

## Goal

Introduce typed task and interaction store interfaces so `internal/tasks.Manager` and
`internal/interaction.Service` no longer depend directly on `*store.FileStore`.

After this cleanup:

- Task lifecycle code depends on `tasks.Store`.
- Interaction lifecycle code depends on `interaction.Store`.
- JSON files are one backend implementation, not the domain-facing abstraction.
- Future database or object-storage backends can be added without changing taskflow,
  interaction response handling, scheduler code, or API handlers.

## Previous Coupling

`internal/tasks.Manager` previously owned a `*store.FileStore` and called:

- `ListJSONFiles` during `Recover`
- `ReadJSONFile` during `Recover`
- `WriteJSON` whenever a task changes

`internal/interaction.Service` had the same file-store coupling for interactions.

This leaks three file-specific concerns into domain code:

- Recovery is path-based.
- Persistence is JSON-value-based rather than record-type-based.
- The constructor requires a concrete file store, so another backend cannot satisfy the
  dependency.

## Target Interfaces

Add this interface in `internal/tasks`:

```go
type Store interface {
	LoadAll() ([]Task, error)
	Save(task Task) error
}
```

Add this interface in `internal/interaction`:

```go
type Store interface {
	LoadAll() ([]Interaction, error)
	Save(interaction Interaction) error
}
```

These interfaces intentionally do not include `Get`, `ListByStatus`, or partial update
methods. The existing services keep an in-memory map and already own lifecycle queries.
The store only needs to recover all durable records at startup and save a full record
after each lifecycle change.

## Implemented Phases

### Phase 1: Add Interfaces and JSON Adapters

- Added `tasks.Store` and `interaction.Store`.
- Created JSON-file adapters that wrap the current `store.FileStore`.
- Kept the existing file layout and JSON shape unchanged.
- Added adapter tests for load/save round trips.

Implemented package shape:

```text
internal/store/jsonfile/
|-- task_store.go
|-- interaction_store.go
`-- store_test.go
```

The adapters hide `ListJSONFiles`, `ReadJSONFile`, and `WriteJSON` from callers.

### Phase 2: Update Domain Constructors

Task manager constructors now accept the typed task store:

```go
func NewManager(taskStore Store, exec executor.Executor) *Manager
func NewManagerWithOptions(taskStore Store, exec executor.Executor, options ManagerOptions) *Manager
```

Interaction service construction now accepts the typed interaction store:

```go
func New(interactionStore Store, options Options) *Service
```

Fields were updated from `*store.FileStore` to the typed interfaces. Public behavior
stayed the same.

### Phase 3: Update Recovery and Save Paths

In `tasks.Manager.Recover`:

- Call `store.LoadAll`.
- Preserve the current behavior that waiting tasks remain waiting.
- Preserve the current behavior that non-final, non-waiting tasks become abandoned.
- Save any abandoned task through `store.Save`.

In `interaction.Service.Recover`:

- Call `store.LoadAll`.
- Clone recovered interactions before placing them in the service map.

In `save` methods:

- Clone the record into the in-memory map.
- Call the typed store's `Save` method with the complete record.

### Phase 4: Update Wiring

In `cmd/proxyma/main.go`:

- Construct JSON-file task and interaction stores through the new adapter package.
- Pass those typed stores into `tasks.NewManagerWithOptions` and `interaction.New`.

The CLI flags can stay unchanged in this cleanup:

- `--task-dir`
- `--interaction-dir`

Database and object-storage flags were not added in this phase. The purpose was to make
the interface clean first.

### Phase 5: Test the Contract

Added JSON-file adapter coverage for:

- Empty backend loads as no records.
- Saving one record makes it visible in `LoadAll`.
- Saving the same ID twice replaces the record.
- Multiple records round-trip independently.
- Malformed persisted data returns a clear error for JSON-backed stores.
- IDs cannot escape the configured file prefix.

Existing manager and interaction service tests now use small in-memory stores where that
makes the test simpler.

## Backward Compatibility

This cleanup did not change:

- JSON record field names.
- JSON directory defaults.
- Task or interaction IDs.
- API response payloads.
- Recovery semantics.
- Approval or interaction expiration behavior.

Existing task and interaction JSON files should continue to load without migration.

## Documentation Update

`docs/store-integration.md` now describes the typed interfaces as the current storage
boundary, not as a recommendation.

The documentation update:

- Moved `tasks.Store` and `interaction.Store` into the "Current Boundary" section.
- Moved `store.FileStore` and JSON-file adapters into a "Default JSON File Backend"
  section.
- Replaced the recommendation section with backend implementation guidance.
- Kept database and object-storage guidance, phrased against `LoadAll` and `Save`.

## Out of Scope

This cleanup does not add a new storage backend. It also does not add distributed locks,
leader election, schema migrations, multi-process write coordination, or task querying
directly from the backend. Those can be planned after the domain code depends on clean
typed store interfaces.
