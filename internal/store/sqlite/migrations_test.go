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
	if len(migrations) != 2 || migrations[0].version != 1 || migrations[1].version != 2 {
		t.Fatalf("migrations = %#v", migrations)
	}
}
