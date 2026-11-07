package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWithOptionsLogsLifecycleInDebugMode(t *testing.T) {
	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	path := filepath.Join(t.TempDir(), "database.db")

	database, err := OpenWithOptions(path, Options{Logger: logger, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	output := logs.String()
	for _, want := range []string{
		"sqlite database opening",
		"new=true",
		"sqlite migration applied version=000001",
		"sqlite migration applied version=000002",
		"sqlite database created",
		"sqlite database ready",
		"sqlite database closing",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log does not contain %q:\n%s", want, output)
		}
	}
}

func TestOpenMigratesAndReopensDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "database.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sqlDB().ExecContext(context.Background(), `SELECT 1 FROM commands_fts LIMIT 1`); err != nil {
		t.Fatalf("FTS table unavailable: %v", err)
	}
	var version int
	if err := database.sqlDB().QueryRowContext(context.Background(), `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d", version)
	}
	if _, err := database.sqlDB().ExecContext(context.Background(), `SELECT 1 FROM history_entries`); err == nil {
		t.Fatal("legacy history_entries table exists")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.sqlDB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration count after reopen = %d", count)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (999, 'future', 'now')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("expected newer schema error")
	}
}

func TestIntegrityAndResetRecreateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := database.sqlDB().QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity check = %q", integrity)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	moved := path + ".before-reset"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + "-wal"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(path + "-shm"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	recreated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer recreated.Close()
	var count int
	if err := recreated.sqlDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("recreated migration count = %d", count)
	}
}
