package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"proxyma/internal/interaction/history"
)

// HistoryStore queries the canonical command and interaction tables directly.
// It intentionally has no projection table or user-session state.
type HistoryStore struct {
	database *Database
}

var _ history.QueryStore = (*HistoryStore)(nil)

// ListHistory applies typed filters, FTS search, authorization scope, and cursor pagination.
func (s *HistoryStore) ListHistory(ctx context.Context, query history.HistoryQuery) (history.HistoryPage, error) {
	plan, err := buildHistoryQuery(query)
	if err != nil {
		return history.HistoryPage{}, err
	}
	stmt, err := s.database.sqlDB().PrepareContext(ctx, plan.sql)
	if err != nil {
		return history.HistoryPage{}, fmt.Errorf("prepare history query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx, plan.args...)
	if err != nil {
		return history.HistoryPage{}, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()
	limit := history.NormalizeLimit(query.Limit)
	page := history.HistoryPage{Items: make([]history.HistoryItem, 0, limit)}
	for rows.Next() {
		item, err := scanHistoryItem(rows)
		if err != nil {
			return history.HistoryPage{}, err
		}
		if plan.postFilter != nil && !plan.postFilter(item) {
			continue
		}
		if len(page.Items) == limit {
			page.NextCursor = history.EncodeCursor(page.Items[len(page.Items)-1].CreatedAt, page.Items[len(page.Items)-1].HistoryID)
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return history.HistoryPage{}, fmt.Errorf("iterate history: %w", err)
	}
	return page, nil
}

func scanHistoryItem(scanner interface{ Scan(...any) error }) (history.HistoryItem, error) {
	var (
		item                                                          history.HistoryItem
		createdAt, completedAt, expiresAt, respondedAt, taskCreatedAt sql.NullString
		approved                                                      sql.NullInt64
	)
	if err := scanner.Scan(
		&item.EntryType, &item.HistoryID, &item.InteractionID, &item.TaskID, &item.AgentID, &item.AgentName,
		&item.InteractionInstanceID, &item.InteractionInstanceCreatorUserID, &item.Provider, &item.Kind,
		&item.ExpectedResponse, &item.Status, &item.Subject, &item.Message, &item.HarnessName, &approved,
		&item.Answer, &item.RespondedBy, &item.CancelReason, &item.TargetID, &item.TargetHostname, &item.TargetIP,
		&item.Command, &item.Result, &item.PolicyJustification, &createdAt, &completedAt, &expiresAt,
		&respondedAt, &taskCreatedAt); err != nil {
		return history.HistoryItem{}, fmt.Errorf("scan history row: %w", err)
	}
	var err error
	item.CreatedAt, err = parseRequiredTime(stringValue(createdAt))
	if err != nil {
		return history.HistoryItem{}, fmt.Errorf("parse history created_at: %w", err)
	}
	if item.CompletedAt, err = scanTime(completedAt); err != nil {
		return history.HistoryItem{}, fmt.Errorf("parse history completed_at: %w", err)
	}
	if item.ExpiresAt, err = scanTime(expiresAt); err != nil {
		return history.HistoryItem{}, fmt.Errorf("parse history expires_at: %w", err)
	}
	if item.RespondedAt, err = scanTime(respondedAt); err != nil {
		return history.HistoryItem{}, fmt.Errorf("parse history responded_at: %w", err)
	}
	item.TaskCreatedAt, err = parseRequiredTime(stringValue(taskCreatedAt))
	if err != nil {
		return history.HistoryItem{}, fmt.Errorf("parse history task_created_at: %w", err)
	}
	item.Approved = boolPointer(approved)
	if item.TargetHostname != "" {
		item.Target = item.TargetHostname
	} else {
		item.Target = item.TargetIP
	}
	return item, nil
}
