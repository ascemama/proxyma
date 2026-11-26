package vmworkflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"proxyma/internal/clientcli"
)

func (s *Service) Step(ctx context.Context, action, stepID, summary, evidence, worker string) (Step, error) {
	if strings.TrimSpace(stepID) == "" {
		return Step{}, cliErr("invalid_input", "step_id is required", false)
	}
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return Step{}, cliErr("invalid_input", "worker-id is required for step changes", false)
	}
	now := s.now().Format(time.RFC3339Nano)
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		return Step{}, err
	}
	defer tx.Rollback()
	var step Step
	var started, completed sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT step_id, plan_id, ordinal, kind, description, status, COALESCE(operation_id,''), COALESCE(result_summary,''), COALESCE(verification_evidence,''), started_at, completed_at FROM remediation_steps WHERE step_id=?`, stepID).Scan(&step.StepID, &step.PlanID, &step.Ordinal, &step.Kind, &step.Description, &step.Status, &step.OperationID, &step.ResultSummary, &step.VerificationEvidence, &started, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Step{}, cliErr("not_found", "step was not found", false)
	}
	if err != nil {
		return Step{}, err
	}
	var findingKey string
	var leaseExpires sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT f.finding_key, f.lease_expires_at FROM findings f JOIN remediation_plans p ON p.finding_key=f.finding_key WHERE p.plan_id=? AND f.lease_owner=?`, step.PlanID, worker).Scan(&findingKey, &leaseExpires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Step{}, cliErr("lease_conflict", "finding is not leased by this worker", false)
		}
		return Step{}, err
	}
	if !leaseActive(leaseExpires.String, s.now()) {
		return Step{}, cliErr("lease_conflict", "finding lease has expired", false)
	}
	status := step.Status
	switch action {
	case "start":
		if step.Status != "pending" && step.Status != "blocked" {
			return Step{}, cliErr("invalid_transition", "only pending or blocked steps can start", false)
		}
		status = "running"
	case "complete":
		if step.Status != "running" {
			return Step{}, cliErr("invalid_transition", "only running steps can complete", false)
		}
		if strings.TrimSpace(summary) == "" || strings.TrimSpace(evidence) == "" {
			return Step{}, cliErr("invalid_input", "completed steps require result_summary and verification_evidence", false)
		}
		status = "completed"
	case "block":
		if step.Status != "running" && step.Status != "pending" {
			return Step{}, cliErr("invalid_transition", "only pending or running steps can block", false)
		}
		if strings.TrimSpace(summary) == "" {
			return Step{}, cliErr("invalid_input", "blocked steps require a result summary", false)
		}
		status = "blocked"
	case "skip":
		if step.Status == "completed" {
			return Step{}, cliErr("invalid_transition", "completed steps cannot be skipped", false)
		}
		status = "skipped"
	default:
		return Step{}, cliErr("invalid_input", "unknown step action", false)
	}
	startedValue := started.String
	completedValue := completed.String
	if action == "start" && startedValue == "" {
		startedValue = now
	}
	if status == "completed" || status == "skipped" {
		completedValue = now
	}
	if _, err := tx.ExecContext(ctx, `UPDATE remediation_steps SET status=?, result_summary=CASE WHEN ?='' THEN result_summary ELSE ? END, verification_evidence=CASE WHEN ?='' THEN verification_evidence ELSE ? END, started_at=NULLIF(?, ''), completed_at=NULLIF(?, '') WHERE step_id=?`, status, summary, summary, evidence, evidence, startedValue, completedValue, stepID); err != nil {
		return Step{}, err
	}
	nextStatus := "in_progress"
	if status == "blocked" {
		nextStatus = "blocked"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE findings SET workflow_status=?, current_step_id=?, updated_at=? WHERE finding_key=?`, nextStatus, stepID, now, findingKey); err != nil {
		return Step{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO finding_events(finding_key, plan_id, step_id, event_type, message, created_at) VALUES (?, ?, ?, ?, ?, ?)`, findingKey, step.PlanID, stepID, "step_"+action, summary, now); err != nil {
		return Step{}, err
	}
	if err := tx.Commit(); err != nil {
		return Step{}, err
	}
	return s.loadStep(ctx, stepID)
}

func (s *Service) loadStep(ctx context.Context, id string) (Step, error) {
	var step Step
	var summary, evidence sql.NullString
	err := s.DB.DB.QueryRowContext(ctx, `SELECT step_id, plan_id, ordinal, kind, description, status, COALESCE(operation_id,''), COALESCE(result_summary,''), COALESCE(verification_evidence,'') FROM remediation_steps WHERE step_id=?`, id).Scan(&step.StepID, &step.PlanID, &step.Ordinal, &step.Kind, &step.Description, &step.Status, &step.OperationID, &summary, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return Step{}, cliErr("not_found", "step was not found", false)
	}
	step.ResultSummary = summary.String
	step.VerificationEvidence = evidence.String
	return step, err
}

func (s *Service) FindingTransition(ctx context.Context, key, target, reason, next, worker string) error {
	allowed := map[string]bool{"resolve": true, "block": true, "unsupported": true, "fail": true, "requeue": true}
	if !allowed[target] {
		return cliErr("invalid_input", "unsupported finding transition", false)
	}
	current, err := s.ShowFinding(ctx, key)
	if err != nil {
		return err
	}
	if target == "resolve" && (current.WorkflowStatus == "blocked" || current.WorkflowStatus == "unsupported" || current.WorkflowStatus == "failed") {
		return cliErr("invalid_transition", "blocked or failed findings must be requeued before resolution", false)
	}
	if target == "resolve" {
		if strings.TrimSpace(worker) == "" || current.LeaseOwner != worker || current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(s.now()) {
			return cliErr("lease_conflict", "finding must be resolved by its active lease owner", false)
		}
		var steps, pending, verified int
		if err := s.DB.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status IN ('pending','running') THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status='completed' AND TRIM(COALESCE(verification_evidence,''))<>'' THEN 1 ELSE 0 END), 0) FROM remediation_steps WHERE plan_id=?`, current.PlanID).Scan(&steps, &pending, &verified); err != nil {
			return err
		}
		if current.PlanID == "" || steps == 0 || pending > 0 || verified == 0 {
			return cliErr("invalid_transition", "finding requires completed remediation and verification evidence before resolution", false)
		}
	}
	if target != "requeue" && target != "resolve" {
		if strings.TrimSpace(worker) == "" || current.LeaseOwner != worker || current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(s.now()) {
			return cliErr("lease_conflict", "finding must be changed by its active lease owner", false)
		}
	}
	if target == "requeue" && current.LeaseOwner != "" && (strings.TrimSpace(worker) == "" || current.LeaseOwner != worker) {
		return cliErr("lease_conflict", "leased finding must be requeued by its lease owner", false)
	}
	if target != "requeue" && strings.TrimSpace(reason) == "" {
		return cliErr("invalid_input", "a reason is required", false)
	}
	status := map[string]string{"resolve": "resolved", "block": "blocked", "unsupported": "unsupported", "fail": "failed", "requeue": "ready"}[target]
	now := s.now().Format(time.RFC3339Nano)
	var query string
	var args []any
	switch target {
	case "resolve":
		query = `UPDATE findings SET workflow_status='resolved', resolved_at=?, lease_owner=NULL, lease_expires_at=NULL, blocked_reason=NULL, required_next_action=NULL, updated_at=? WHERE finding_key=?`
		args = []any{now, now, key}
	case "requeue":
		query = `UPDATE findings SET workflow_status='ready', next_attempt_at=NULL, blocked_reason=NULL, required_next_action=NULL, lease_owner=NULL, lease_expires_at=NULL, updated_at=? WHERE finding_key=?`
		args = []any{now, key}
	default:
		query = `UPDATE findings SET workflow_status=?, blocked_reason=?, required_next_action=?, lease_owner=NULL, lease_expires_at=NULL, updated_at=? WHERE finding_key=?`
		args = []any{status, reason, next, now, key}
	}
	result, err := s.DB.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return cliErr("not_found", "finding was not found", false)
	}
	eventName := map[string]string{"resolve": "resolved", "block": "blocked", "unsupported": "unsupported", "fail": "failed", "requeue": "requeued"}[target]
	return event(ctx, s.DB, key, eventName, reason)
}

func (s *Service) ExecuteOperation(ctx context.Context, input OperationInput) (map[string]any, error) {
	if strings.TrimSpace(input.FindingKey) == "" || strings.TrimSpace(input.StepID) == "" || strings.TrimSpace(input.OperationKind) == "" || strings.TrimSpace(input.Payload) == "" || strings.TrimSpace(input.Reasoning) == "" {
		return nil, cliErr("invalid_input", "finding_key, step_id, operation_kind, payload, and reasoning are required", false)
	}
	finding, err := s.ShowFinding(ctx, input.FindingKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.WorkerID) == "" || finding.LeaseOwner != input.WorkerID || finding.LeaseExpiresAt == nil || !finding.LeaseExpiresAt.After(s.now()) {
		return nil, cliErr("lease_conflict", "finding must be operated by its active lease owner", false)
	}
	if finding.WorkflowStatus != "in_progress" && finding.WorkflowStatus != "verification_pending" {
		return nil, cliErr("invalid_transition", "finding must be claimed before an operation", false)
	}
	target := clientcli.Target{IP: finding.AssetIP}
	if target.IP == "" {
		target = clientcli.Target{Hostname: finding.AssetDNSName}
	}
	if target.IP == "" && target.Hostname == "" {
		return nil, cliErr("target_unavailable", "finding has no IP or DNS target", false)
	}
	requestHash := fmt.Sprintf("%x", sha256Bytes([]byte(strings.Join([]string{input.FindingKey, input.StepID, input.OperationKind, input.Payload, input.Reasoning}, "\x00"))))
	var existingID, existingStatus string
	if err := s.DB.DB.QueryRowContext(ctx, `SELECT operation_id, operation_status FROM finding_operations WHERE finding_key=? AND step_id=? AND request_hash=? ORDER BY submitted_at DESC LIMIT 1`, input.FindingKey, input.StepID, requestHash).Scan(&existingID, &existingStatus); err == nil {
		return map[string]any{"operation_id": existingID, "operation_status": existingStatus, "replayed": true}, nil
	}
	operationID := uuid.NewString()
	now := s.now().Format(time.RFC3339Nano)
	if _, err := s.DB.DB.ExecContext(ctx, `INSERT INTO finding_operations(operation_id, finding_key, step_id, operation_kind, request_hash, operation_status, last_checked_at) VALUES (?, ?, ?, ?, ?, 'prepared', ?)`, operationID, input.FindingKey, input.StepID, nullable(input.OperationKind), requestHash, now); err != nil {
		return nil, err
	}
	if _, err := s.DB.DB.ExecContext(ctx, `UPDATE remediation_steps SET status='running', operation_id=? WHERE step_id=?`, operationID, input.StepID); err != nil {
		return nil, err
	}
	if s.Proxy == nil {
		return nil, cliErr("proxyma_unavailable", "Proxyma integration is not configured", true)
	}
	envelope, err := s.Proxy.Submit(ctx, clientcli.SubmitInput{UserGoal: input.Reasoning, Target: target, Payload: input.Payload, Reasoning: input.Reasoning, HarnessName: "vm-workflow", Metadata: map[string]any{"vm_workflow_operation_id": operationID, "finding_key": input.FindingKey, "step_id": input.StepID}})
	if err != nil {
		code, retryable := proxymaError(err)
		status := "failed"
		if retryable {
			status = "waiting_proxyma"
		}
		_, _ = s.DB.DB.ExecContext(ctx, `UPDATE finding_operations SET operation_status=?, safe_result_summary=?, last_checked_at=? WHERE operation_id=?`, status, err.Error(), s.now().Format(time.RFC3339Nano), operationID)
		if code == "not_found" {
			_ = s.FindingTransition(ctx, input.FindingKey, "block", "proxyma_access_unavailable", "provide or authorize Proxyma access", input.WorkerID)
		}
		return nil, err
	}
	if envelope.Task == nil || len(envelope.Task.Operations) == 0 {
		return nil, cliErr("proxyma_invalid_response", "Proxyma client did not return durable task state", true)
	}
	operation := envelope.Task.Operations[len(envelope.Task.Operations)-1]
	_, err = s.DB.DB.ExecContext(ctx, `UPDATE finding_operations SET client_task_id=?, client_operation_id=?, client_task_revision=?, operation_status='waiting_proxyma', submitted_at=?, last_checked_at=? WHERE operation_id=?`, envelope.Task.ClientTaskID, operation.OperationID, envelope.Task.Revision, now, now, operationID)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.DB.ExecContext(ctx, `UPDATE findings SET workflow_status='waiting_proxyma', updated_at=? WHERE finding_key=?`, now, input.FindingKey)
	_ = event(ctx, s.DB, input.FindingKey, "operation_submitted", "Proxyma operation submitted")
	return map[string]any{"operation_id": operationID, "client_task_id": envelope.Task.ClientTaskID, "client_operation_id": operation.OperationID, "operation_status": "waiting_proxyma"}, nil
}

func sha256Bytes(value []byte) []byte { h := sha256.Sum256(value); return h[:] }

func leaseActive(value string, now time.Time) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.After(now)
		}
	}
	return false
}

func (s *Service) Pulse(ctx context.Context) (map[string]any, error) {
	if s.Proxy != nil {
		envelope, err := s.Proxy.Resume(ctx)
		if err != nil {
			return nil, err
		}
		for _, task := range envelope.Tasks {
			s.reconcileTask(ctx, task)
		}
	}
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"action_required": status.ActionRequired, "status": status}, nil
}

func (s *Service) reconcileTask(ctx context.Context, task clientcli.TaskState) {
	for _, operation := range task.Operations {
		var workflowID string
		if operation.Metadata != nil {
			if value, ok := operation.Metadata["vm_workflow_operation_id"].(string); ok {
				workflowID = value
			}
		}
		if workflowID == "" {
			continue
		}
		status := "waiting_proxyma"
		if operation.Phase == "completed" || task.Status == "pending" {
			status = "completed"
		}
		if operation.Phase == "blocked" || task.Status == "blocked" || task.Status == "failed" {
			status = "failed"
		}
		now := s.now().Format(time.RFC3339Nano)
		_, _ = s.DB.DB.ExecContext(ctx, `UPDATE finding_operations SET operation_status=?, safe_result_summary=?, completed_at=CASE WHEN ?='completed' THEN ? ELSE completed_at END, last_checked_at=? WHERE operation_id=?`, status, task.FinalSummary, status, now, now, workflowID)
		var findingKey string
		if s.DB.DB.QueryRowContext(ctx, `SELECT finding_key FROM finding_operations WHERE operation_id=?`, workflowID).Scan(&findingKey) == nil {
			if status == "completed" {
				_, _ = s.DB.DB.ExecContext(ctx, `UPDATE findings SET workflow_status='verification_pending', updated_at=? WHERE finding_key=?`, now, findingKey)
				_ = event(ctx, s.DB, findingKey, "operation_completed", "Proxyma operation completed; verification required")
			}
			if status == "failed" {
				_, _ = s.DB.DB.ExecContext(ctx, `UPDATE findings SET workflow_status='failed', blocked_reason=?, required_next_action=?, updated_at=? WHERE finding_key=?`, "Proxyma operation failed", "inspect the Proxyma result before retrying", now, findingKey)
				_ = event(ctx, s.DB, findingKey, "operation_failed", "Proxyma operation failed")
			}
		}
	}
}
