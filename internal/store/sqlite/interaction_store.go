package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/interaction/instances"
)

// InteractionStore persists complete asynchronous interaction state.
type InteractionStore struct {
	database  *Database
	instances instances.Registry
}

var _ interaction.Store = (*InteractionStore)(nil)

// LoadAll loads durable interaction records in creation order.
func (s *InteractionStore) LoadAll() ([]interaction.Interaction, error) {
	stmt, err := s.database.sqlDB().PrepareContext(context.Background(), `
		SELECT record_json
		FROM interactions
		ORDER BY created_at ASC, interaction_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("prepare load interactions: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load interactions: %w", err)
	}
	defer rows.Close()
	var result []interaction.Interaction
	for rows.Next() {
		var record string
		if err := rows.Scan(&record); err != nil {
			return nil, fmt.Errorf("scan interaction: %w", err)
		}
		var item interaction.Interaction
		if err := json.Unmarshal([]byte(record), &item); err != nil {
			return nil, fmt.Errorf("decode interaction: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interactions: %w", err)
	}
	return result, nil
}

// Save inserts or updates one interaction using transaction-bound prepared statements.
func (s *InteractionStore) Save(record interaction.Interaction) error {
	record.InteractionInstanceID = instances.NormalizeInstanceID(record.InteractionInstanceID)
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode interaction %s: %w", record.InteractionID, err)
	}
	creator, provider := s.instanceMetadata(record.InteractionInstanceID)
	var taskID string
	if record.TaskID != "" {
		taskID = record.TaskID
	}
	var approved any
	var answer, respondedBy string
	var respondedAt *time.Time
	if record.Response != nil {
		approved = boolValue(record.Response.Approved)
		answer = record.Response.Answer
		respondedBy = record.Response.RespondedBy
		respondedAt = record.Response.RespondedAt
	}
	var deliveryReference string
	var deliveredAt *time.Time
	if record.Delivery != nil {
		provider = record.Delivery.Provider
		deliveryReference = record.Delivery.Reference
		deliveredAt = &record.Delivery.DeliveredAt
	}
	tx, err := s.database.sqlDB().BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin interaction save: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(context.Background(), `
		INSERT INTO interactions (
			interaction_id, task_id, interaction_instance_id, interaction_instance_creator_user_id,
			provider, kind, expected_response, subject, message, requested_by, status,
			created_at, expires_at, responded_at, approved, answer, responded_by, cancel_reason,
			delivery_reference, delivered_at, record_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(interaction_id) DO UPDATE SET
			task_id = excluded.task_id,
			interaction_instance_id = excluded.interaction_instance_id,
			interaction_instance_creator_user_id = excluded.interaction_instance_creator_user_id,
			provider = excluded.provider,
			kind = excluded.kind,
			expected_response = excluded.expected_response,
			subject = excluded.subject,
			message = excluded.message,
			requested_by = excluded.requested_by,
			status = excluded.status,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			responded_at = excluded.responded_at,
			approved = excluded.approved,
			answer = excluded.answer,
			responded_by = excluded.responded_by,
			cancel_reason = excluded.cancel_reason,
			delivery_reference = excluded.delivery_reference,
			delivered_at = excluded.delivered_at,
			record_json = excluded.record_json`)
	if err != nil {
		return fmt.Errorf("prepare interaction save: %w", err)
	}
	_, err = stmt.ExecContext(context.Background(),
		record.InteractionID, taskID, instances.NormalizeInstanceID(record.InteractionInstanceID), creator,
		provider, record.Kind, record.ExpectedResponse, record.Subject, record.Message, record.RequestedBy, record.Status,
		record.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTime(record.ExpiresAt), nullableTime(respondedAt), approved,
		answer, respondedBy, record.CancelReason, deliveryReference, nullableTime(deliveredAt), string(encoded))
	closeErr := stmt.Close()
	if err != nil {
		return fmt.Errorf("save interaction %s: %w", record.InteractionID, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close interaction save statement: %w", closeErr)
	}
	if err := syncInteractionSearch(context.Background(), tx, record, taskID, answer); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit interaction %s: %w", record.InteractionID, err)
	}
	return nil
}

func (s *InteractionStore) instanceMetadata(id string) (creator, provider string) {
	provider = "local"
	id = instances.NormalizeInstanceID(id)
	for _, definition := range s.instances.All() {
		metadata := definition.Metadata()
		if metadata.ID == id {
			return metadata.CreatorUserID, metadata.ProviderType
		}
	}
	return "", provider
}

func syncInteractionSearch(ctx context.Context, tx *sql.Tx, record interaction.Interaction, taskID, answer string) error {
	deleteStmt, err := tx.PrepareContext(ctx, `DELETE FROM interactions_fts WHERE interaction_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare interaction search delete: %w", err)
	}
	if _, err := deleteStmt.ExecContext(ctx, record.InteractionID); err != nil {
		_ = deleteStmt.Close()
		return fmt.Errorf("delete interaction search row: %w", err)
	}
	if err := deleteStmt.Close(); err != nil {
		return fmt.Errorf("close interaction search delete: %w", err)
	}
	insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO interactions_fts(interaction_id, task_id, subject, message, answer, requested_by) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare interaction search insert: %w", err)
	}
	_, execErr := insertStmt.ExecContext(ctx, record.InteractionID, taskID, record.Subject, record.Message, answer, record.RequestedBy)
	closeErr := insertStmt.Close()
	if execErr != nil {
		return fmt.Errorf("insert interaction search row: %w", execErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close interaction search insert: %w", closeErr)
	}
	return nil
}
