package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"proxyma/internal/tasks"
)

// CommandStore persists the complete command record in the commands table.
// Frequently filtered fields are duplicated in typed columns; record_json
// preserves the evolving task shape without making the schema brittle.
type CommandStore struct {
	database *Database
}

var _ tasks.Store = (*CommandStore)(nil)

// LoadAll loads commands in creation order for task-manager recovery.
func (s *CommandStore) LoadAll() ([]tasks.Task, error) {
	stmt, err := s.database.sqlDB().PrepareContext(context.Background(), `
		SELECT task_id, agent_name, target_id, record_json
		FROM commands
		ORDER BY created_at ASC, task_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("prepare load commands: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load commands: %w", err)
	}
	defer rows.Close()
	var result []tasks.Task
	for rows.Next() {
		var taskID, agentName, targetID, record string
		if err := rows.Scan(&taskID, &agentName, &targetID, &record); err != nil {
			return nil, fmt.Errorf("scan command: %w", err)
		}
		var task tasks.Task
		if err := json.Unmarshal([]byte(record), &task); err != nil {
			return nil, fmt.Errorf("decode command %s: %w", taskID, err)
		}
		task.TaskID = taskID
		task.AgentName = agentName
		if task.Target != nil {
			task.Target.ID = targetID
		}
		result = append(result, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate commands: %w", err)
	}
	return result, nil
}

// Save inserts or updates one command using a transaction-bound prepared statement.
func (s *CommandStore) Save(task tasks.Task) error {
	record, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("encode command %s: %w", task.TaskID, err)
	}
	var targetID, hostname, ip, transport, operatingSystem, policyID string
	if task.Target != nil {
		targetID = task.Target.ID
		hostname = task.Target.Hostname
		ip = task.Target.IP
		transport = task.Target.Transport
		operatingSystem = task.Target.OS
		policyID = task.Target.PolicyID
	}
	resultClass := ""
	if task.Result != nil {
		if task.Result.Error != "" || (task.Result.ExitStatus != nil && *task.Result.ExitStatus != 0) {
			resultClass = "error"
		} else {
			resultClass = "success"
		}
	}
	policyJustification := ""
	if task.Policy != nil {
		policyJustification = task.Policy.Justification
	}
	tx, err := s.database.sqlDB().BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin command save: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(context.Background(), `
		INSERT INTO commands (
			task_id, client_operation_id, client_request_hash, agent_id, agent_name,
			target_id, target_hostname, target_ip, target_transport, target_os, target_policy_id,
			command, reasoning, requested_by, interaction_instance_id, status, policy_justification,
			created_at, started_at, completed_at, result_class, record_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			client_operation_id = excluded.client_operation_id,
			client_request_hash = excluded.client_request_hash,
			agent_id = excluded.agent_id,
			agent_name = excluded.agent_name,
			target_id = excluded.target_id,
			target_hostname = excluded.target_hostname,
			target_ip = excluded.target_ip,
			target_transport = excluded.target_transport,
			target_os = excluded.target_os,
			target_policy_id = excluded.target_policy_id,
			command = excluded.command,
			reasoning = excluded.reasoning,
			requested_by = excluded.requested_by,
			interaction_instance_id = excluded.interaction_instance_id,
			status = excluded.status,
			policy_justification = excluded.policy_justification,
			created_at = excluded.created_at,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			result_class = excluded.result_class,
			record_json = excluded.record_json`)
	if err != nil {
		return fmt.Errorf("prepare command save: %w", err)
	}
	_, err = stmt.ExecContext(context.Background(),
		task.TaskID, task.ClientOperationID, task.ClientRequestHash, task.AgentID, task.AgentName,
		targetID, hostname, ip, transport, operatingSystem, policyID,
		task.Command, task.Reasoning, task.RequestedBy, task.InteractionInstanceID, task.Status, policyJustification,
		task.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTime(task.StartedAt), nullableTime(task.CompletedAt), resultClass, string(record))
	closeErr := stmt.Close()
	if err != nil {
		return fmt.Errorf("save command %s: %w", task.TaskID, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close command save statement: %w", closeErr)
	}
	if err := syncCommandSearch(context.Background(), tx, task, hostname, ip, resultClass); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit command %s: %w", task.TaskID, err)
	}
	return nil
}

func syncCommandSearch(ctx context.Context, tx *sql.Tx, task tasks.Task, hostname, ip, resultClass string) error {
	deleteStmt, err := tx.PrepareContext(ctx, `DELETE FROM commands_fts WHERE task_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare command search delete: %w", err)
	}
	if _, err := deleteStmt.ExecContext(ctx, task.TaskID); err != nil {
		_ = deleteStmt.Close()
		return fmt.Errorf("delete command search row: %w", err)
	}
	if err := deleteStmt.Close(); err != nil {
		return fmt.Errorf("close command search delete: %w", err)
	}
	insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO commands_fts(task_id, command, reasoning, requested_by, target_hostname, target_ip, result) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare command search insert: %w", err)
	}
	_, execErr := insertStmt.ExecContext(ctx, task.TaskID, task.Command, task.Reasoning, task.RequestedBy, hostname, ip, resultClass)
	closeErr := insertStmt.Close()
	if execErr != nil {
		return fmt.Errorf("insert command search row: %w", execErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close command search insert: %w", closeErr)
	}
	return nil
}
