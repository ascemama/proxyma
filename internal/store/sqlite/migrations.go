package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	path    string
}

func (d *Database) migrate(ctx context.Context) error {
	// schema_migrations is the durable record of successfully committed
	// numbered migrations; it makes startup upgrades deterministic and auditable.
	if _, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	var current int
	if err := d.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	d.debugf("sqlite schema version current=%d available=%d migrations=%d", current, latestMigrationVersion(migrations), len(migrations))
	if len(migrations) > 0 && current > migrations[len(migrations)-1].version {
		return fmt.Errorf("database schema version %d is newer than this Proxyma version", current)
	}
	for _, migration := range migrations {
		if migration.version <= current {
			continue
		}
		contents, err := migrationFiles.ReadFile(migration.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", migration.path, err)
		}
		tx, err := d.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %06d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %06d (%s): %w", migration.version, migration.name, err)
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare migration record %06d: %w", migration.version, err)
		}
		_, execErr := stmt.ExecContext(ctx, migration.version, migration.name, time.Now().UTC().Format(time.RFC3339Nano))
		closeErr := stmt.Close()
		if execErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %06d: %w", migration.version, execErr)
		}
		if closeErr != nil {
			_ = tx.Rollback()
			return fmt.Errorf("close migration statement %06d: %w", migration.version, closeErr)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %06d: %w", migration.version, err)
		}
		d.debugf("sqlite migration applied version=%06d name=%s", migration.version, migration.name)
		current = migration.version
	}
	if current == latestMigrationVersion(migrations) {
		d.debugf("sqlite migrations complete schema_version=%d", current)
	}
	return nil
}

func latestMigrationVersion(migrations []migration) int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		parts := strings.SplitN(base, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		migrations = append(migrations, migration{version: version, name: parts[1], path: "migrations/" + entry.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].version == migrations[i].version {
			return nil, fmt.Errorf("duplicate migration version %d", migrations[i].version)
		}
	}
	return migrations, nil
}
