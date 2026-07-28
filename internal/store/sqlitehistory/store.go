// Package sqlitehistory stores Proxyma interaction history in SQLite.
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
	"time"

	"proxyma/internal/interaction/history"

	_ "modernc.org/sqlite"
)

const schema = `
create table if not exists interaction_history (
	interaction_id text primary key,
	task_id text not null,
	agent_id text not null,
	agent_name text,
	interaction_instance_id text not null,
	interaction_instance_creator_user_id text,
	provider text,
	kind text not null,
	expected_response text not null,
	status text not null,
	subject text not null,
	message text not null,
	requested_by text,
	approved integer,
	answer text,
	responded_by text,
	cancel_reason text,
	target_hostname text,
	target_ip text,
	command text not null,
	created_at text not null,
	expires_at text,
	responded_at text,
	task_created_at text not null,
	record_json text not null
);

create table if not exists interaction_history_visible_users (
	interaction_id text not null,
	user_id text not null,
	primary key (interaction_id, user_id),
	foreign key (interaction_id) references interaction_history(interaction_id) on delete cascade
);

create index if not exists interaction_history_visible_user_created
	on interaction_history_visible_users(user_id, interaction_id);

create index if not exists interaction_history_agent_created
	on interaction_history(agent_id, created_at desc, interaction_id desc);

create index if not exists interaction_history_status_created
	on interaction_history(status, created_at desc, interaction_id desc);

create index if not exists interaction_history_instance_created
	on interaction_history(interaction_instance_id, created_at desc, interaction_id desc);

create index if not exists interaction_history_kind_created
	on interaction_history(kind, created_at desc, interaction_id desc);

create index if not exists interaction_history_created
	on interaction_history(created_at desc, interaction_id desc);

create virtual table if not exists interaction_history_fts using fts5(
	interaction_id unindexed,
	subject,
	message,
	command,
	requested_by,
	target_hostname,
	target_ip
);
`

// Store implements history.QueryStore and history.Indexer over SQLite.
type Store struct {
	db *sql.DB
}

// Open opens or creates the SQLite history database.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("interaction history db path is required")
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
	db, err := sql.Open("sqlite", "file:Proxyma-history?mode=memory&cache=shared")
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
		return fmt.Errorf("interaction history database is not configured")
	}
	if _, err := s.db.ExecContext(ctx, `pragma foreign_keys = on`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// DeleteAll clears the query index before a rebuild.
func (s *Store) DeleteAll(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("interaction history database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `delete from interaction_history_fts`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from interaction_history_visible_users`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from interaction_history`); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertInteraction indexes one durable interaction.
func (s *Store) UpsertInteraction(ctx context.Context, record history.IndexedInteraction) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("interaction history database is not configured")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	item := record.Item
	approved := sql.NullInt64{}
	if item.Approved != nil {
		approved.Valid = true
		if *item.Approved {
			approved.Int64 = 1
		}
	}
	if _, err := tx.ExecContext(ctx, `
insert into interaction_history (
	interaction_id, task_id, agent_id, agent_name, interaction_instance_id,
	interaction_instance_creator_user_id, provider, kind, expected_response,
	status, subject, message, requested_by, approved, answer, responded_by,
	cancel_reason, target_hostname, target_ip, command, created_at, expires_at,
	responded_at, task_created_at, record_json
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(interaction_id) do update set
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
	command = excluded.command,
	created_at = excluded.created_at,
	expires_at = excluded.expires_at,
	responded_at = excluded.responded_at,
	task_created_at = excluded.task_created_at,
	record_json = excluded.record_json`,
		item.InteractionID,
		item.TaskID,
		item.AgentID,
		nullString(item.AgentName),
		item.InteractionInstanceID,
		nullString(item.InteractionInstanceCreatorUserID),
		nullString(item.Provider),
		item.Kind,
		item.ExpectedResponse,
		item.Status,
		item.Subject,
		item.Message,
		nullString(item.RequestedBy),
		approved,
		nullString(item.Answer),
		nullString(item.RespondedBy),
		nullString(item.CancelReason),
		nullString(item.TargetHostname),
		nullString(item.TargetIP),
		item.Command,
		formatTime(item.CreatedAt),
		formatOptionalTime(item.ExpiresAt),
		formatOptionalTime(item.RespondedAt),
		formatTime(item.TaskCreatedAt),
		record.RecordJSON,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from interaction_history_visible_users where interaction_id = ?`, item.InteractionID); err != nil {
		return err
	}
	for _, userID := range record.VisibleUserIDs {
		if strings.TrimSpace(userID) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `insert or ignore into interaction_history_visible_users(interaction_id, user_id) values (?, ?)`, item.InteractionID, userID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `delete from interaction_history_fts where interaction_id = ?`, item.InteractionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
insert into interaction_history_fts(interaction_id, subject, message, command, requested_by, target_hostname, target_ip)
values (?, ?, ?, ?, ?, ?, ?)`,
		item.InteractionID,
		item.Subject,
		item.Message,
		item.Command,
		item.RequestedBy,
		item.TargetHostname,
		item.TargetIP,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListInteractions returns a filtered cursor page.
func (s *Store) ListInteractions(ctx context.Context, query history.InteractionQuery) (history.InteractionPage, error) {
	if s == nil || s.db == nil {
		return history.InteractionPage{}, fmt.Errorf("interaction history database is not configured")
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
			return history.InteractionPage{}, fmt.Errorf("invalid command regex")
		}
		commandRegex = compiled
		sqlLimit = 501
	}

	sqlText, args, err := buildListSQL(query, sqlLimit)
	if err != nil {
		return history.InteractionPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return history.InteractionPage{}, err
	}
	defer rows.Close()

	items := make([]history.InteractionHistoryItem, 0, limit+1)
	var lastScanned *history.InteractionHistoryItem
	scanned := 0
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return history.InteractionPage{}, err
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
		return history.InteractionPage{}, err
	}
	nextCursor := ""
	if len(items) > limit {
		nextCursor = history.EncodeCursor(items[limit-1].CreatedAt, items[limit-1].InteractionID)
		items = items[:limit]
	} else if strings.TrimSpace(query.CommandPattern) != "" && (mode == history.CommandPatternGlob || mode == history.CommandPatternRegex) && scanned >= sqlLimit && lastScanned != nil {
		nextCursor = history.EncodeCursor(lastScanned.CreatedAt, lastScanned.InteractionID)
	}
	return history.InteractionPage{Items: items, NextCursor: nextCursor}, nil
}

func buildListSQL(query history.InteractionQuery, limit int) (string, []any, error) {
	var builder strings.Builder
	args := []any{}
	builder.WriteString(`select h.interaction_id, h.task_id, h.agent_id, h.agent_name, h.interaction_instance_id,
	h.interaction_instance_creator_user_id, h.provider, h.kind, h.expected_response, h.status,
	h.subject, h.message, h.requested_by, h.approved, h.answer, h.responded_by, h.cancel_reason,
	h.target_hostname, h.target_ip, h.command, h.created_at, h.expires_at, h.responded_at, h.task_created_at
from interaction_history h`)
	if !query.CurrentUserIsAdmin {
		builder.WriteString(` join interaction_history_visible_users vu on vu.interaction_id = h.interaction_id and vu.user_id = ?`)
		args = append(args, query.CurrentUserID)
	}
	where := []string{}
	addEquals := func(column, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		where = append(where, column+" = ?")
		args = append(args, strings.TrimSpace(value))
	}
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
		where = append(where, `h.interaction_id in (
			select interaction_id from interaction_history_fts where interaction_history_fts match ?
		)`)
		args = append(args, strings.TrimSpace(query.Text))
	}
	cursor, err := history.DecodeCursor(query.Cursor)
	if err != nil {
		return "", nil, err
	}
	if !cursor.CreatedAt.IsZero() {
		where = append(where, "(h.created_at < ? or (h.created_at = ? and h.interaction_id < ?))")
		createdAt := formatTime(cursor.CreatedAt)
		args = append(args, createdAt, createdAt, cursor.InteractionID)
	}
	if len(where) > 0 {
		builder.WriteString(" where ")
		builder.WriteString(strings.Join(where, " and "))
	}
	builder.WriteString(" order by h.created_at desc, h.interaction_id desc limit ?")
	args = append(args, limit)
	return builder.String(), args, nil
}

func scanItem(rows *sql.Rows) (history.InteractionHistoryItem, error) {
	var item history.InteractionHistoryItem
	var agentName, creatorUserID, provider, requestedBy, answer, respondedBy, cancelReason sql.NullString
	var targetHostname, targetIP, expiresAt, respondedAt sql.NullString
	var approved sql.NullInt64
	var createdAt, taskCreatedAt string
	if err := rows.Scan(
		&item.InteractionID,
		&item.TaskID,
		&item.AgentID,
		&agentName,
		&item.InteractionInstanceID,
		&creatorUserID,
		&provider,
		&item.Kind,
		&item.ExpectedResponse,
		&item.Status,
		&item.Subject,
		&item.Message,
		&requestedBy,
		&approved,
		&answer,
		&respondedBy,
		&cancelReason,
		&targetHostname,
		&targetIP,
		&item.Command,
		&createdAt,
		&expiresAt,
		&respondedAt,
		&taskCreatedAt,
	); err != nil {
		return item, err
	}
	item.AgentName = agentName.String
	item.InteractionInstanceCreatorUserID = creatorUserID.String
	item.Provider = provider.String
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
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return item, err
	}
	item.CreatedAt = parsedCreatedAt
	parsedTaskCreatedAt, err := parseTime(taskCreatedAt)
	if err != nil {
		return item, err
	}
	item.TaskCreatedAt = parsedTaskCreatedAt
	if expiresAt.Valid {
		parsed, err := parseTime(expiresAt.String)
		if err != nil {
			return item, err
		}
		item.ExpiresAt = &parsed
	}
	if respondedAt.Valid {
		parsed, err := parseTime(respondedAt.String)
		if err != nil {
			return item, err
		}
		item.RespondedAt = &parsed
	}
	return item, nil
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*value), Valid: true}
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

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
