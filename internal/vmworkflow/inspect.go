package vmworkflow

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Service) InspectPlan(ctx context.Context, findingKey string) (map[string]any, error) {
	var plan map[string]any
	var planID, assumptions, current, status, created, updated string
	var revision int
	var goal string
	err := s.DB.DB.QueryRowContext(ctx, `SELECT plan_id, plan_revision, goal, COALESCE(assumptions,''), COALESCE(current_step_id,''), status, created_at, updated_at FROM remediation_plans WHERE finding_key=? AND status='active' ORDER BY plan_revision DESC LIMIT 1`, findingKey).Scan(&planID, &revision, &goal, &assumptions, &current, &status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cliErr("not_found", "active remediation plan was not found", false)
	}
	if err != nil {
		return nil, err
	}
	plan = map[string]any{"plan_id": planID, "finding_key": findingKey, "plan_revision": revision, "goal": goal, "assumptions": assumptions, "current_step_id": current, "status": status, "created_at": created, "updated_at": updated}
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT step_id, ordinal, kind, description, status, COALESCE(operation_id,''), COALESCE(result_summary,''), COALESCE(verification_evidence,'') FROM remediation_steps WHERE plan_id=? ORDER BY ordinal`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := []Step{}
	for rows.Next() {
		var step Step
		if err := rows.Scan(&step.StepID, &step.Ordinal, &step.Kind, &step.Description, &step.Status, &step.OperationID, &step.ResultSummary, &step.VerificationEvidence); err != nil {
			return nil, err
		}
		step.PlanID = planID
		steps = append(steps, step)
	}
	plan["steps"] = steps
	return plan, rows.Err()
}

func (s *Service) InspectOperation(ctx context.Context, operationID string) (map[string]any, error) {
	var finding, step, kind, clientTask, clientOperation, status, hash, summary, submitted, completed, checked sql.NullString
	err := s.DB.DB.QueryRowContext(ctx, `SELECT finding_key, step_id, operation_kind, client_task_id, client_operation_id, operation_status, request_hash, safe_result_summary, submitted_at, completed_at, last_checked_at FROM finding_operations WHERE operation_id=?`, operationID).Scan(&finding, &step, &kind, &clientTask, &clientOperation, &status, &hash, &summary, &submitted, &completed, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cliErr("not_found", "operation was not found", false)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"operation_id": operationID, "finding_key": finding.String, "step_id": step.String, "operation_kind": kind.String, "client_task_id": clientTask.String, "client_operation_id": clientOperation.String, "operation_status": status.String, "request_hash": hash.String, "safe_result_summary": summary.String, "submitted_at": submitted.String, "completed_at": completed.String, "last_checked_at": checked.String}, nil
}

func (s *Service) InspectReport(ctx context.Context, reportID string) (map[string]any, error) {
	var id, hash, profile, version, filename, scanID, scanDate, imported, metadata string
	err := s.DB.DB.QueryRowContext(ctx, `SELECT report_id, report_sha256, source_profile, source_profile_version, source_filename, COALESCE(scan_id,''), COALESCE(scan_date,''), imported_at, raw_metadata_json FROM reports WHERE report_id=?`, reportID).Scan(&id, &hash, &profile, &version, &filename, &scanID, &scanDate, &imported, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cliErr("not_found", "report was not found", false)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"report_id": id, "report_sha256": hash, "source_profile": profile, "source_profile_version": version, "source_filename": filename, "scan_id": scanID, "scan_date": scanDate, "imported_at": imported, "raw_metadata_json": metadata}, nil
}

func (s *Service) InspectAccess(ctx context.Context, assetID string) (map[string]any, error) {
	var key, id, ip, dns, status, reason, checked, next, details string
	err := s.DB.DB.QueryRowContext(ctx, `SELECT asset_key, asset_id, COALESCE(asset_ip,''), COALESCE(asset_dns_name,''), access_status, COALESCE(reason_code,''), checked_at, COALESCE(next_check_at,''), details_json FROM asset_access WHERE asset_id=?`, assetID).Scan(&key, &id, &ip, &dns, &status, &reason, &checked, &next, &details)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cliErr("not_found", "asset access record was not found", false)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"asset_key": key, "asset_id": id, "asset_ip": ip, "asset_dns_name": dns, "access_status": status, "reason_code": reason, "checked_at": checked, "next_check_at": next, "details_json": details}, nil
}

func (s *Service) InspectEvents(ctx context.Context, findingKey string) ([]map[string]any, error) {
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT event_id, event_type, COALESCE(message,''), metadata_json, created_at FROM finding_events WHERE finding_key=? ORDER BY event_id`, findingKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []map[string]any
	for rows.Next() {
		var id int
		var typ, message, metadata, created string
		if err := rows.Scan(&id, &typ, &message, &metadata, &created); err != nil {
			return nil, err
		}
		events = append(events, map[string]any{"event_id": id, "event_type": typ, "message": message, "metadata_json": metadata, "created_at": created})
	}
	return events, rows.Err()
}
