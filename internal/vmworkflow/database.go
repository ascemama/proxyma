package vmworkflow

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_vulnerability_management.sql
var migrationFS embed.FS

type Database struct {
	DB   *sql.DB
	Path string
}

func OpenDatabase(path string) (*Database, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open workflow database: %w", err)
	}
	d := &Database{DB: db, Path: path}
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure workflow database: %w", err)
		}
	}
	if err := d.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

func (d *Database) Close() error { return d.DB.Close() }

func (d *Database) migrate() error {
	if _, err := d.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, description TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create schema migrations: %w", err)
	}
	var version int
	if err := d.DB.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read workflow schema version: %w", err)
	}
	if version > 1 {
		return fmt.Errorf("workflow database schema %d is newer than this vm-workflow binary", version)
	}
	if version == 0 {
		contents, err := migrationFS.ReadFile("migrations/001_vulnerability_management.sql")
		if err != nil {
			return fmt.Errorf("read workflow migration: %w", err)
		}
		tx, err := d.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply workflow migration: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, description, applied_at) VALUES (?, ?, ?)`, 1, "initial vulnerability-management schema", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record workflow migration: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit workflow migration: %w", err)
		}
	}
	return nil
}

func (d *Database) Integrity(ctx context.Context) (string, error) {
	var result string
	if err := d.DB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return "", err
	}
	return result, nil
}
