package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationFailureRollsBackAndLeavesVersionUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL); CREATE TABLE commands(task_id TEXT PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected migration failure")
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var count int
	if err := check.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed migration recorded %d versions", count)
	}
}

func TestLoadMigrationsAreSequential(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 3 || migrations[0].version != 1 || migrations[1].version != 2 || migrations[2].version != 3 {
		t.Fatalf("migrations = %#v", migrations)
	}
}

func TestRenameHarnessIdentityMigrationPreservesRecordsAndSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	for _, version := range []int{0} {
		contents, readErr := migrationFiles.ReadFile(migrations[version].path)
		if readErr != nil {
			raw.Close()
			t.Fatal(readErr)
		}
		if _, execErr := raw.Exec(string(contents)); execErr != nil {
			raw.Close()
			t.Fatal(execErr)
		}
	}
	if _, err := raw.Exec(`
		INSERT INTO commands(
			task_id, agent_id, command, reasoning, requested_by, interaction_instance_id,
			status, created_at, record_json
		) VALUES ('task-old', 'agent-old', 'hostname', 'migration test', 'hermes-nightly', 'local',
			'completed', '2026-01-01T00:00:00Z',
			'{"task_id":"task-old","requested_by":"hermes-nightly"}');
		INSERT INTO interactions(
			interaction_id, task_id, interaction_instance_id, kind, expected_response,
			subject, message, requested_by, status, created_at, record_json
		) VALUES ('interaction-old', 'task-old', 'local', 'question', 'text',
			'Migration question', 'Migration message', 'hermes-nightly', 'pending',
			'2026-01-01T00:00:00Z',
			'{"interaction_id":"interaction-old","requested_by":"hermes-nightly"}');
	`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	contents, err := migrationFiles.ReadFile(migrations[1].path)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.Exec(string(contents)); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL);
		INSERT INTO schema_migrations(version, name, applied_at) VALUES (1, 'initial', 'now'), (2, 'search_indexes', 'now');
	`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var commandName, commandRecord, interactionName, interactionRecord string
	if err := database.sqlDB().QueryRow(`SELECT harness_name, record_json FROM commands WHERE task_id = 'task-old'`).Scan(&commandName, &commandRecord); err != nil {
		t.Fatal(err)
	}
	if err := database.sqlDB().QueryRow(`SELECT harness_name, record_json FROM interactions WHERE interaction_id = 'interaction-old'`).Scan(&interactionName, &interactionRecord); err != nil {
		t.Fatal(err)
	}
	if commandName != "hermes-nightly" || commandRecord != `{"task_id":"task-old","harness_name":"hermes-nightly"}` {
		t.Fatalf("migrated command = %q %s", commandName, commandRecord)
	}
	if interactionName != "hermes-nightly" || interactionRecord != `{"interaction_id":"interaction-old","harness_name":"hermes-nightly"}` {
		t.Fatalf("migrated interaction = %q %s", interactionName, interactionRecord)
	}

	var commandSearchName, interactionSearchName string
	if err := database.sqlDB().QueryRow(`SELECT harness_name FROM commands_fts WHERE commands_fts MATCH 'hermes'`).Scan(&commandSearchName); err != nil {
		t.Fatal(err)
	}
	if err := database.sqlDB().QueryRow(`SELECT harness_name FROM interactions_fts WHERE interactions_fts MATCH 'hermes'`).Scan(&interactionSearchName); err != nil {
		t.Fatal(err)
	}
	if commandSearchName != "hermes-nightly" || interactionSearchName != "hermes-nightly" {
		t.Fatalf("migrated search values = %q %q", commandSearchName, interactionSearchName)
	}
}
