// Package sqlite provides the canonical SQLite repositories used by Proxyma.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"proxyma/internal/interaction/instances"

	_ "modernc.org/sqlite"
)

// Database owns the shared SQLite handle used by the command, interaction,
// and history repositories.
type Database struct {
	db     *sql.DB
	logger Logger
	debug  bool
}

// Logger is the small logging surface needed by the SQLite lifecycle.
type Logger interface {
	Printf(format string, args ...any)
}

// Options controls optional database lifecycle logging.
type Options struct {
	Logger Logger
	Debug  bool
}

var memoryDatabaseID uint64

// Open opens or creates a Proxyma database and applies all migrations.
func Open(path string) (*Database, error) {
	return OpenWithOptions(path, Options{})
}

// OpenWithOptions opens or creates a Proxyma database with optional lifecycle logging.
func OpenWithOptions(path string, options Options) (*Database, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	_, statErr := os.Stat(path)
	newDatabase := os.IsNotExist(statErr)
	if statErr != nil && !newDatabase {
		return nil, fmt.Errorf("inspect database path: %w", statErr)
	}
	if options.Debug {
		logf(options.Logger, "sqlite database opening path=%s new=%t", path, newDatabase)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database := &Database{db: db, logger: options.Logger, debug: options.Debug}
	if err := database.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := database.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if newDatabase {
		database.debugf("sqlite database created path=%s", path)
	}
	database.debugf("sqlite database ready path=%s", path)
	return database, nil
}

// OpenMemory opens an isolated in-memory database for tests.
func OpenMemory() (*Database, error) {
	id := atomic.AddUint64(&memoryDatabaseID, 1)
	db, err := sql.Open("sqlite", fmt.Sprintf("file:proxyma-database-%d?mode=memory&cache=shared", id))
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory database: %w", err)
	}
	database := &Database{db: db}
	if err := database.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := database.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return database, nil
}

func (d *Database) configure(ctx context.Context) error {
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := d.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure sqlite (%s): %w", pragma, err)
		}
	}
	return nil
}

// Close closes the shared database handle.
func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	d.debugf("sqlite database closing")
	return d.db.Close()
}

func (d *Database) debugf(format string, args ...any) {
	if d == nil || !d.debug {
		return
	}
	logf(d.logger, format, args...)
}

func logf(logger Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

func (d *Database) sqlDB() *sql.DB { return d.db }

// NewCommandStore creates the canonical task repository.
func (d *Database) NewCommandStore() *CommandStore { return &CommandStore{database: d} }

// NewInteractionStore creates the canonical interaction repository.
func (d *Database) NewInteractionStore() *InteractionStore { return &InteractionStore{database: d} }

// NewInteractionStoreWithRegistry creates an interaction repository that
// snapshots provider and creator metadata from the JSON-backed registry.
func (d *Database) NewInteractionStoreWithRegistry(registry instances.Registry) *InteractionStore {
	return &InteractionStore{database: d, instances: registry}
}

// NewHistoryStore creates the history query repository.
func (d *Database) NewHistoryStore() *HistoryStore { return &HistoryStore{database: d} }
