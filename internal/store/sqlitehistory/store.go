// Package sqlitehistory stores Proxyma's unified command and interaction history index in SQLite.
package sqlitehistory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"proxyma/internal/interaction/history"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 3

var memoryStoreID uint64

const schema = `
create table if not exists history_entries (
	history_id text primary key,
	entry_type text not null check (entry_type in ('command', 'interaction')),
	interaction_id text,
	task_id text,
	agent_id text,
	agent_name text,
	interaction_instance_id text,
	interaction_instance_creator_user_id text,
	provider text,
	kind text,
	expected_response text,
	status text not null,
	subject text,
	message text,
	requested_by text,
	approved integer,
	answer text,
	responded_by text,
	cancel_reason text,
	target_hostname text,
	target_ip text,
	target_id text,
	command text not null default '',
	result text,
	policy_justification text,
	created_at text not null,
	completed_at text,
	expires_at text,
	responded_at text,
	task_created_at text,
	record_json text
);

create table if not exists history_entries_visible_users (
	history_id text not null,
	user_id text not null,
	source_type text not null check (source_type in ('agent', 'target')),
	source_id text not null,
	primary key (history_id, user_id, source_type, source_id),
	foreign key (history_id) references history_entries(history_id) on delete cascade
);

create index if not exists history_entries_visible_user_created
	on history_entries_visible_users(user_id, history_id);

create index if not exists history_entries_visible_source
	on history_entries_visible_users(source_type, source_id, history_id);

create index if not exists history_entries_type_created
	on history_entries(entry_type, created_at desc, history_id desc);

create index if not exists history_entries_agent_created
	on history_entries(agent_id, created_at desc, history_id desc);

create index if not exists history_entries_target_created
	on history_entries(target_id, created_at desc, history_id desc);

create index if not exists history_entries_status_created
	on history_entries(status, created_at desc, history_id desc);

create index if not exists history_entries_instance_created
	on history_entries(interaction_instance_id, created_at desc, history_id desc);

create index if not exists history_entries_kind_created
	on history_entries(kind, created_at desc, history_id desc);

create index if not exists history_entries_created
	on history_entries(created_at desc, history_id desc);

create virtual table if not exists history_entries_fts using fts5(
	history_id unindexed,
	entry_type unindexed,
	subject,
	message,
	command,
	result,
	requested_by,
	target_hostname,
	target_ip
);
`

// Store implements history.QueryStore and history.Indexer over SQLite.
type Store struct {
	db *sql.DB
}

// Open opens or creates the SQLite history database and migrates the legacy
// interaction-only schema when necessary.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("history db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// OpenMemory returns an in-memory SQLite history store for tests.
func OpenMemory() (*Store, error) {
	id := atomic.AddUint64(&memoryStoreID, 1)
	db, err := sql.Open("sqlite", fmt.Sprintf("file:Proxyma-history-%d?mode=memory&cache=shared", id))
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history database is not configured")
	}
	if _, err := s.db.ExecContext(ctx, `pragma foreign_keys = on`); err != nil {
		return err
	}
	legacy, err := tableExists(ctx, s.db, "interaction_history")
	if err != nil {
		return err
	}
	current, err := tableExists(ctx, s.db, "history_entries")
	if err != nil {
		return err
	}
	if legacy && !current {
		return s.migrateLegacy(ctx)
	}
	if current {
		version, err := schemaVersion(ctx, s.db)
		if err != nil {
			return err
		}
		if version > 0 && version < currentSchemaVersion {
			if err := s.migrateV2ToV3(ctx); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("pragma user_version = %d", currentSchemaVersion))
	return err
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, `pragma user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *Store) migrateLegacy(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into history_entries (
	history_id, entry_type, interaction_id, task_id, agent_id, agent_name,
	interaction_instance_id, interaction_instance_creator_user_id, provider, kind,
	expected_response, status, subject, message, requested_by, approved, answer,
	responded_by, cancel_reason, target_hostname, target_ip, command, created_at,
	expires_at, responded_at, task_created_at, record_json
)
select
	'interaction:' || interaction_id, 'interaction', interaction_id, task_id, agent_id,
	agent_name, interaction_instance_id, interaction_instance_creator_user_id,
	provider, kind, expected_response, status, subject, message, requested_by,
	approved, answer, responded_by, cancel_reason, target_hostname, target_ip,
	command, created_at, expires_at, responded_at, task_created_at, record_json
from interaction_history`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into history_entries_visible_users(history_id, user_id, source_type, source_id)
select 'interaction:' || v.interaction_id, v.user_id, 'agent', coalesce(h.agent_id, '')
from interaction_history_visible_users v
join history_entries h on h.history_id = 'interaction:' || v.interaction_id`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into history_entries_fts(history_id, entry_type, subject, message, command, requested_by, target_hostname, target_ip)
select 'interaction:' || interaction_id, 'interaction', subject, message, command, requested_by, target_hostname, target_ip
from interaction_history_fts`); err != nil {
		return err
	}
	for _, query := range []string{`drop table interaction_history_fts`, `drop table interaction_history_visible_users`, `drop table interaction_history`} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("pragma user_version = %d", currentSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateV2ToV3(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `alter table history_entries add column target_id text`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `alter table history_entries_visible_users rename to history_entries_visible_users_v2`); err != nil {
		return err
	}
	// Renaming the v2 table preserves its index names. Drop those indexes
	// before creating the equivalent indexes on the v3 visibility table.
	for _, query := range []string{
		`drop index if exists history_entries_visible_user_created`,
		`drop index if exists history_entries_visible_source`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
create table history_entries_visible_users (
	history_id text not null,
	user_id text not null,
	source_type text not null check (source_type in ('agent', 'target')),
	source_id text not null,
	primary key (history_id, user_id, source_type, source_id),
	foreign key (history_id) references history_entries(history_id) on delete cascade
)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into history_entries_visible_users(history_id, user_id, source_type, source_id)
select v.history_id, v.user_id, 'agent', coalesce(h.agent_id, '')
from history_entries_visible_users_v2 v
join history_entries h on h.history_id = v.history_id`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
create index history_entries_visible_user_created
	on history_entries_visible_users(user_id, history_id);
create index history_entries_visible_source
	on history_entries_visible_users(source_type, source_id, history_id)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `drop table history_entries_visible_users_v2`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("pragma user_version = %d", currentSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteAll clears the query index before a rebuild.
func (s *Store) DeleteAll(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	for _, query := range []string{
		`delete from history_entries_fts`,
		`delete from history_entries_visible_users`,
		`delete from history_entries`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpsertCommand indexes one durable task using its safe history projection.
func (s *Store) UpsertCommand(ctx context.Context, record history.IndexedCommand) error {
	return s.upsert(ctx, record.Item, record.VisibilitySources(), "")
}

// UpsertInteraction indexes one durable interaction.
func (s *Store) UpsertInteraction(ctx context.Context, record history.IndexedInteraction) error {
	return s.upsert(ctx, record.Item, record.VisibilitySources(), record.RecordJSON)
}

// SyncAgentVisibility replaces the agent-derived visibility rows for all
// history entries belonging to agentID.
func (s *Store) SyncAgentVisibility(ctx context.Context, agentID string, userIDs []string) error {
	return s.syncVisibility(ctx, "agent", agentID, userIDs, `select history_id from history_entries where agent_id = ?`)
}

// SyncTargetVisibility replaces the target-derived visibility rows for all
// command history entries linked to targetID.
func (s *Store) SyncTargetVisibility(ctx context.Context, targetID string, userIDs []string) error {
	return s.syncVisibility(ctx, "target", targetID, userIDs, `select history_id from history_entries where entry_type = 'command' and target_id = ?`)
}

func (s *Store) syncVisibility(ctx context.Context, sourceType, sourceID string, userIDs []string, historyQuery string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history database is not configured")
	}
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" {
		return fmt.Errorf("visibility source is required")
	}
	users := uniqueStrings(userIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, historyQuery, sourceID)
	if err != nil {
		return err
	}
	var historyIDs []string
	for rows.Next() {
		var historyID string
		if err := rows.Scan(&historyID); err != nil {
			rows.Close()
			return err
		}
		historyIDs = append(historyIDs, historyID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, historyID := range historyIDs {
		if _, err := tx.ExecContext(ctx, `delete from history_entries_visible_users where history_id = ? and source_type = ? and source_id = ?`, historyID, sourceType, sourceID); err != nil {
			return err
		}
		for _, userID := range users {
			if _, err := tx.ExecContext(ctx, `insert into history_entries_visible_users(history_id, user_id, source_type, source_id) values (?, ?, ?, ?)`, historyID, userID, sourceType, sourceID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) upsert(ctx context.Context, item history.HistoryItem, visibilitySources []history.VisibilitySource, recordJSON string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history database is not configured")
	}
	if strings.TrimSpace(item.HistoryID) == "" && strings.TrimSpace(item.InteractionID) != "" {
		item.HistoryID = history.EntryTypeInteraction + ":" + strings.TrimSpace(item.InteractionID)
	}
	if strings.TrimSpace(item.EntryType) == "" && strings.TrimSpace(item.InteractionID) != "" {
		item.EntryType = history.EntryTypeInteraction
	}
	if strings.TrimSpace(item.HistoryID) == "" {
		return fmt.Errorf("history_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	approved := sql.NullInt64{}
	if item.Approved != nil {
		approved.Valid = true
		if *item.Approved {
			approved.Int64 = 1
		}
	}
	_, err = tx.ExecContext(ctx, `
insert into history_entries (
	history_id, entry_type, interaction_id, task_id, agent_id, agent_name,
	interaction_instance_id, interaction_instance_creator_user_id, provider, kind,
	expected_response, status, subject, message, requested_by, approved, answer,
		responded_by, cancel_reason, target_hostname, target_ip, target_id, command, result,
	policy_justification, created_at, completed_at, expires_at, responded_at,
	task_created_at, record_json
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(history_id) do update set
	entry_type = excluded.entry_type,
	interaction_id = excluded.interaction_id,
	task_id = excluded.task_id,
	agent_id = excluded.agent_id,
	agent_name = excluded.agent_name,
	interaction_instance_id = excluded.interaction_instance_id,
	interaction_instance_creator_user_id = excluded.interaction_instance_creator_user_id,
	provider = excluded.provider,
	kind = excluded.kind,
	expected_response = excluded.expected_response,
	status = excluded.status,
	subject = excluded.subject,
	message = excluded.message,
	requested_by = excluded.requested_by,
	approved = excluded.approved,
	answer = excluded.answer,
	responded_by = excluded.responded_by,
	cancel_reason = excluded.cancel_reason,
	target_hostname = excluded.target_hostname,
	target_ip = excluded.target_ip,
	target_id = excluded.target_id,
	command = excluded.command,
	result = excluded.result,
	policy_justification = excluded.policy_justification,
	created_at = excluded.created_at,
	completed_at = excluded.completed_at,
	expires_at = excluded.expires_at,
	responded_at = excluded.responded_at,
	task_created_at = excluded.task_created_at,
	record_json = excluded.record_json`,
		item.HistoryID,
		item.EntryType,
		nullString(item.InteractionID),
		nullString(item.TaskID),
		nullString(item.AgentID),
		nullString(item.AgentName),
		nullString(item.InteractionInstanceID),
		nullString(item.InteractionInstanceCreatorUserID),
		nullString(item.Provider),
		nullString(item.Kind),
		nullString(item.ExpectedResponse),
		item.Status,
		nullString(item.Subject),
		nullString(item.Message),
		nullString(item.RequestedBy),
		approved,
		nullString(item.Answer),
		nullString(item.RespondedBy),
		nullString(item.CancelReason),
		nullString(item.TargetHostname),
		nullString(item.TargetIP),
		nullString(item.TargetID),
		item.Command,
		nullString(item.Result),
		nullString(item.PolicyJustification),
		formatTime(item.CreatedAt),
		formatOptionalTime(item.CompletedAt),
		formatOptionalTime(item.ExpiresAt),
		formatOptionalTime(item.RespondedAt),
		formatOptionalTime(nonZeroTime(item.TaskCreatedAt)),
		nullString(recordJSON),
	)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from history_entries_visible_users where history_id = ?`, item.HistoryID); err != nil {
		return err
	}
	for _, source := range visibilitySources {
		if strings.TrimSpace(source.Type) == "" || strings.TrimSpace(source.ID) == "" {
			continue
		}
		for _, userID := range source.UserIDs {
			if strings.TrimSpace(userID) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `insert or ignore into history_entries_visible_users(history_id, user_id, source_type, source_id) values (?, ?, ?, ?)`, item.HistoryID, userID, source.Type, source.ID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `delete from history_entries_fts where history_id = ?`, item.HistoryID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into history_entries_fts(history_id, entry_type, subject, message, command, result, requested_by, target_hostname, target_ip)
values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.HistoryID,
		item.EntryType,
		item.Subject,
		item.Message,
		item.Command,
		item.Result,
		item.RequestedBy,
		item.TargetHostname,
		item.TargetIP,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListHistory returns a filtered cursor page of commands and interactions.
func (s *Store) ListHistory(ctx context.Context, query history.HistoryQuery) (history.HistoryPage, error) {
	if s == nil || s.db == nil {
		return history.HistoryPage{}, fmt.Errorf("history database is not configured")
	}
	limit := history.NormalizeLimit(query.Limit)
	mode := strings.TrimSpace(query.CommandPatternMode)
	if mode == "" {
		mode = history.CommandPatternLiteral
	}
	var commandRegex *regexp.Regexp
	sqlLimit := limit + 1
	if strings.TrimSpace(query.CommandPattern) != "" && mode == history.CommandPatternGlob {
		sqlLimit = 501
	}
	if strings.TrimSpace(query.CommandPattern) != "" && mode == history.CommandPatternRegex {
		compiled, err := regexp.Compile(query.CommandPattern)
		if err != nil {
			return history.HistoryPage{}, fmt.Errorf("invalid command regex")
		}
		commandRegex = compiled
		sqlLimit = 501
	}
	sqlText, args, err := buildListSQL(query, sqlLimit)
	if err != nil {
		return history.HistoryPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return history.HistoryPage{}, err
	}
	defer rows.Close()
	items := make([]history.HistoryItem, 0, limit+1)
	var lastScanned *history.HistoryItem
	scanned := 0
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return history.HistoryPage{}, err
		}
		scanned++
		lastScanned = &item
		if commandRegex != nil && !commandRegex.MatchString(item.Command) {
			continue
		}
		if strings.TrimSpace(query.CommandPattern) != "" && mode == history.CommandPatternGlob && !globMatch(query.CommandPattern, item.Command) {
			continue
		}
		items = append(items, item)
		if len(items) > limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return history.HistoryPage{}, err
	}
	nextCursor := ""
	if len(items) > limit {
		nextCursor = history.EncodeCursor(items[limit-1].CreatedAt, items[limit-1].HistoryID)
		items = items[:limit]
	} else if strings.TrimSpace(query.CommandPattern) != "" && (mode == history.CommandPatternGlob || mode == history.CommandPatternRegex) && scanned >= sqlLimit && lastScanned != nil {
		nextCursor = history.EncodeCursor(lastScanned.CreatedAt, lastScanned.HistoryID)
	}
	return history.HistoryPage{Items: items, NextCursor: nextCursor}, nil
}

// ListInteractions is kept for package-level compatibility with older store tests.
func (s *Store) ListInteractions(ctx context.Context, query history.InteractionQuery) (history.InteractionPage, error) {
	return s.ListHistory(ctx, query)
}

func buildListSQL(query history.HistoryQuery, limit int) (string, []any, error) {
	var builder strings.Builder
	args := []any{}
	builder.WriteString(`select h.history_id, h.entry_type, h.interaction_id, h.task_id, h.agent_id, h.agent_name,
	h.interaction_instance_id, h.interaction_instance_creator_user_id, h.provider, h.kind,
	h.expected_response, h.status, h.subject, h.message, h.requested_by, h.approved,
	h.answer, h.responded_by, h.cancel_reason, h.target_hostname, h.target_ip, h.command,
	h.result, h.policy_justification, h.created_at, h.completed_at, h.expires_at,
	h.responded_at, h.task_created_at
from history_entries h`)
	where := []string{}
	if !query.CurrentUserIsAdmin {
		where = append(where, "exists (select 1 from history_entries_visible_users vu where vu.history_id = h.history_id and vu.user_id = ?)")
		args = append(args, query.CurrentUserID)
	}
	addEquals := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		where = append(where, column+" = ?")
		args = append(args, strings.TrimSpace(value))
	}
	addEquals("h.entry_type", query.EntryType)
	addEquals("h.agent_id", query.AgentID)
	addEquals("h.interaction_instance_id", query.InteractionInstanceID)
	if query.CurrentUserIsAdmin {
		addEquals("h.interaction_instance_creator_user_id", query.InteractionInstanceCreatorUserID)
	}
	addIn := func(column string, values []string) {
		values = nonEmpty(values)
		if len(values) == 0 {
			return
		}
		where = append(where, column+" in ("+placeholders(len(values))+")")
		for _, value := range values {
			args = append(args, value)
		}
	}
	addIn("h.status", query.Status)
	addIn("h.kind", query.Kind)
	if query.CreatedFrom != nil {
		where = append(where, "h.created_at >= ?")
		args = append(args, formatTime(*query.CreatedFrom))
	}
	if query.CreatedTo != nil {
		where = append(where, "h.created_at <= ?")
		args = append(args, formatTime(*query.CreatedTo))
	}
	if strings.TrimSpace(query.Target) != "" {
		where = append(where, "(h.target_hostname like ? escape '\\' or h.target_ip like ? escape '\\')")
		pattern := "%" + escapeLike(strings.TrimSpace(query.Target)) + "%"
		args = append(args, pattern, pattern)
	}
	mode := strings.TrimSpace(query.CommandPatternMode)
	if mode == "" {
		mode = history.CommandPatternLiteral
	}
	if strings.TrimSpace(query.CommandPattern) != "" && mode == history.CommandPatternLiteral {
		where = append(where, "h.command like ? escape '\\'")
		args = append(args, "%"+escapeLike(strings.TrimSpace(query.CommandPattern))+"%")
	}
	if strings.TrimSpace(query.Text) != "" {
		where = append(where, `h.history_id in (
			select history_id from history_entries_fts where history_entries_fts match ?
		)`)
		args = append(args, strings.TrimSpace(query.Text))
	}
	cursor, err := history.DecodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	if !cursor.CreatedAt.IsZero() {
		where = append(where, "(h.created_at < ? or (h.created_at = ? and h.history_id < ?))")
		createdAt := formatTime(cursor.CreatedAt)
		args = append(args, createdAt, createdAt, cursor.HistoryID)
	}
	if len(where) > 0 {
		builder.WriteString(" where ")
		builder.WriteString(strings.Join(where, " and "))
	}
	builder.WriteString(" order by h.created_at desc, h.history_id desc limit ?")
	args = append(args, limit)
	return builder.String(), args, nil
}

func scanItem(rows *sql.Rows) (history.HistoryItem, error) {
	var item history.HistoryItem
	var entryType, interactionID, taskID, agentID, agentName, instanceID, creatorUserID, provider, kind, expectedResponse sql.NullString
	var subject, message, requestedBy, answer, respondedBy, cancelReason, targetHostname, targetIP, command, result, policyJustification sql.NullString
	var createdAt, completedAt, expiresAt, respondedAt, taskCreatedAt sql.NullString
	var approved sql.NullInt64
	if err := rows.Scan(
		&item.HistoryID, &entryType, &interactionID, &taskID, &agentID, &agentName,
		&instanceID, &creatorUserID, &provider, &kind, &expectedResponse, &item.Status,
		&subject, &message, &requestedBy, &approved, &answer, &respondedBy,
		&cancelReason, &targetHostname, &targetIP, &command, &result, &policyJustification,
		&createdAt, &completedAt, &expiresAt, &respondedAt, &taskCreatedAt,
	); err != nil {
		return item, err
	}
	item.EntryType = entryType.String
	item.InteractionID = interactionID.String
	item.TaskID = taskID.String
	item.AgentID = agentID.String
	item.AgentName = agentName.String
	item.InteractionInstanceID = instanceID.String
	item.InteractionInstanceCreatorUserID = creatorUserID.String
	item.Provider = provider.String
	item.Kind = kind.String
	item.ExpectedResponse = expectedResponse.String
	item.Subject = subject.String
	item.Message = message.String
	item.RequestedBy = requestedBy.String
	if approved.Valid {
		value := approved.Int64 != 0
		item.Approved = &value
	}
	item.Answer = answer.String
	item.RespondedBy = respondedBy.String
	item.CancelReason = cancelReason.String
	item.TargetHostname = targetHostname.String
	item.TargetIP = targetIP.String
	item.Target = item.TargetHostname
	if item.Target == "" {
		item.Target = item.TargetIP
	}
	item.Command = command.String
	item.Result = result.String
	item.PolicyJustification = policyJustification.String
	parsedCreatedAt, err := parseTime(createdAt.String)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsedCreatedAt
	for value, destination := range map[string]**time.Time{
		completedAt.String: &item.CompletedAt,
		expiresAt.String:   &item.ExpiresAt,
		respondedAt.String: &item.RespondedAt,
	} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := parseTime(value)
		if err != nil {
			return item, err
		}
		*destination = &parsed
	}
	if strings.TrimSpace(taskCreatedAt.String) != "" {
		parsed, err := parseTime(taskCreatedAt.String)
		if err != nil {
			return item, err
		}
		item.TaskCreatedAt = parsed
	}
	return item, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `select count(*) from sqlite_master where type in ('table', 'view') and name = ?`, name).Scan(&count)
	return count > 0, err
}

func nonZeroTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func formatOptionalTime(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*value), Valid: true}
}

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func placeholders(count int) string {
	values := make([]string, count)
	for i := range values {
		values[i] = "?"
	}
	return strings.Join(values, ",")
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func globMatch(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	var builder strings.Builder
	builder.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			builder.WriteString(".*")
		case '?':
			builder.WriteString(".")
		default:
			builder.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	builder.WriteString("$")
	return regexp.MustCompile(builder.String()).MatchString(value)
}

func rollback(tx *sql.Tx) {
	if tx == nil {
		return
	}
	err := tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		return
	}
}
