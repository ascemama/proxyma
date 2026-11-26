package vmworkflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"proxyma/internal/clientcli"
)

type Service struct {
	DB    *Database
	Proxy Proxyma
	Now   func() time.Time
}

func NewService(db *Database, proxy Proxyma) *Service {
	return &Service{DB: db, Proxy: proxy, Now: time.Now}
}

func (s *Service) now() time.Time { return s.Now().UTC() }

func (s *Service) Status(ctx context.Context) (StatusSummary, error) {
	result := StatusSummary{Counts: map[string]int{}}
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT workflow_status, COUNT(*) FROM findings GROUP BY workflow_status`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return result, err
		}
		result.Counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	now := s.now().Format(time.RFC3339Nano)
	if err := s.DB.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM findings WHERE lease_expires_at IS NOT NULL AND lease_expires_at < ?`, now).Scan(&result.ExpiredLeases); err != nil {
		return result, err
	}
	if err := s.DB.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM finding_operations WHERE operation_status IN ('prepared', 'submitting', 'submission_unknown', 'submitted', 'waiting_proxyma')`).Scan(&result.PendingOperations); err != nil {
		return result, err
	}
	result.ActionRequired = result.Counts["ready"] > 0 || result.Counts["verification_pending"] > 0 || result.Counts["failed"] > 0 || result.ExpiredLeases > 0
	return result, nil
}

func (s *Service) Claim(ctx context.Context, worker string, lease time.Duration) (Finding, error) {
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return Finding{}, cliErr("invalid_input", "worker-id is required", false)
	}
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	expires := now.Add(lease).Format(time.RFC3339Nano)
	conn, err := s.DB.DB.Conn(ctx)
	if err != nil {
		return Finding{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return Finding{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var key string
	query := `SELECT finding_key FROM findings WHERE (workflow_status='ready' OR (workflow_status='failed' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR workflow_status='verification_pending' OR (workflow_status='in_progress' AND lease_owner=?)) AND (lease_expires_at IS NULL OR lease_expires_at < ? OR lease_owner=?) ORDER BY CASE WHEN cvss_v4_threat_score IS NULL THEN 1 ELSE 0 END, cvss_v4_threat_score DESC, CASE WHEN cvss_v4_base_score IS NULL THEN 1 ELSE 0 END, cvss_v4_base_score DESC, COALESCE(vpr, -1) DESC, COALESCE(epss, -1) DESC, COALESCE(last_observed_at, '') ASC, finding_key LIMIT 1`
	if err := conn.QueryRowContext(ctx, query, nowText, worker, nowText, worker).Scan(&key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Finding{}, cliErr("no_work", "no actionable findings", false)
		}
		return Finding{}, err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE findings SET workflow_status='in_progress', lease_owner=?, lease_expires_at=?, attempt_count=attempt_count+1, updated_at=? WHERE finding_key=?`, worker, expires, nowText, key); err != nil {
		return Finding{}, err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO finding_events(finding_key, event_type, message, metadata_json, created_at) VALUES (?, 'claimed', 'finding claimed by worker', ?, ?)`, key, fmt.Sprintf(`{"worker_id":%q}`, worker), nowText); err != nil {
		return Finding{}, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return Finding{}, err
	}
	committed = true
	return s.ShowFinding(ctx, key)
}

func (s *Service) RenewLease(ctx context.Context, key, worker string, lease time.Duration) error {
	if lease <= 0 {
		lease = 15 * time.Minute
	}
	expires := s.now().Add(lease).Format(time.RFC3339Nano)
	result, err := s.DB.DB.ExecContext(ctx, `UPDATE findings SET lease_expires_at=?, updated_at=? WHERE finding_key=? AND lease_owner=? AND workflow_status='in_progress'`, expires, s.now().Format(time.RFC3339Nano), key, worker)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return cliErr("lease_conflict", "finding is not leased by this worker", false)
	}
	return nil
}

func (s *Service) ReleaseLease(ctx context.Context, key, worker string) error {
	result, err := s.DB.DB.ExecContext(ctx, `UPDATE findings SET lease_owner=NULL, lease_expires_at=NULL, updated_at=? WHERE finding_key=? AND lease_owner=?`, s.now().Format(time.RFC3339Nano), key, worker)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return cliErr("lease_conflict", "finding is not leased by this worker", false)
	}
	return nil
}

func (s *Service) ShowFinding(ctx context.Context, key string) (Finding, error) {
	var f Finding
	var lease, next, resolved, reopened sql.NullString
	var vpr, epss, base, threat sql.NullFloat64
	var created, updated sql.NullString
	err := s.DB.DB.QueryRowContext(ctx, `SELECT finding_key, asset_id, COALESCE(asset_ip,''), COALESCE(asset_dns_name,''), COALESCE(asset_netbios_name,''), plugin_id, plugin_name, COALESCE(instance_key,''), COALESCE(protocol,''), COALESCE(port,''), COALESCE(severity,''), vpr, epss, cvss_v4_base_score, cvss_v4_threat_score, COALESCE(workflow_status,''), COALESCE(plan_id,''), COALESCE(current_step_id,''), attempt_count, COALESCE(lease_owner,''), lease_expires_at, next_attempt_at, COALESCE(blocked_reason,''), COALESCE(required_next_action,''), resolved_at, reopened_at, created_at, updated_at FROM findings WHERE finding_key=?`, key).Scan(&f.FindingKey, &f.AssetID, &f.AssetIP, &f.AssetDNSName, &f.AssetNetBIOSName, &f.PluginID, &f.PluginName, &f.InstanceKey, &f.Protocol, &f.Port, &f.Severity, &vpr, &epss, &base, &threat, &f.WorkflowStatus, &f.PlanID, &f.CurrentStepID, &f.AttemptCount, &f.LeaseOwner, &lease, &next, &f.BlockedReason, &f.RequiredNextAction, &resolved, &reopened, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Finding{}, cliErr("not_found", "finding was not found", false)
	}
	if err != nil {
		return Finding{}, err
	}
	if vpr.Valid {
		f.VPR = &vpr.Float64
	}
	if epss.Valid {
		f.EPSS = &epss.Float64
	}
	if base.Valid {
		f.CVSSV4BaseScore = &base.Float64
	}
	if threat.Valid {
		f.CVSSV4ThreatScore = &threat.Float64
	}
	parseTime := func(n sql.NullString) *time.Time {
		if !n.Valid || n.String == "" {
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
			if t, e := time.Parse(layout, n.String); e == nil {
				return &t
			}
		}
		return nil
	}
	f.LeaseExpiresAt = parseTime(lease)
	f.NextAttemptAt = parseTime(next)
	f.ResolvedAt = parseTime(resolved)
	f.ReopenedAt = parseTime(reopened)
	if value := parseTime(created); value != nil {
		f.CreatedAt = *value
	}
	if value := parseTime(updated); value != nil {
		f.UpdatedAt = *value
	}
	return f, nil
}

func (s *Service) PlanCreate(ctx context.Context, input PlanInput, revise bool) (map[string]any, error) {
	if strings.TrimSpace(input.FindingKey) == "" || strings.TrimSpace(input.Goal) == "" || len(input.Steps) == 0 {
		return nil, cliErr("invalid_input", "finding_key, goal, and at least one step are required", false)
	}
	finding, err := s.ShowFinding(ctx, input.FindingKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.WorkerID) == "" || finding.LeaseOwner != input.WorkerID || finding.LeaseExpiresAt == nil || !finding.LeaseExpiresAt.After(s.now()) {
		return nil, cliErr("lease_conflict", "finding plan must be changed by its active lease owner", false)
	}
	var revision int
	_ = s.DB.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(plan_revision), 0) FROM remediation_plans WHERE finding_key=?`, input.FindingKey).Scan(&revision)
	if revision > 0 && !revise {
		return nil, cliErr("plan_exists", "an active or historical plan already exists; use plan revise", false)
	}
	revision++
	now := s.now().Format(time.RFC3339Nano)
	planID := uuid.NewString()
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if revise {
		if _, err := tx.ExecContext(ctx, `UPDATE remediation_plans SET status='superseded', updated_at=? WHERE finding_key=? AND status='active'`, now, input.FindingKey); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO remediation_plans(plan_id, finding_key, plan_revision, goal, assumptions, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, planID, input.FindingKey, revision, input.Goal, nullable(input.Assumptions), now, now); err != nil {
		return nil, err
	}
	for i, step := range input.Steps {
		if strings.TrimSpace(step.Kind) == "" || strings.TrimSpace(step.Description) == "" {
			return nil, cliErr("invalid_input", "plan steps require kind and description", false)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO remediation_steps(step_id, plan_id, ordinal, kind, description, status) VALUES (?, ?, ?, ?, ?, 'pending')`, uuid.NewString(), planID, i+1, step.Kind, step.Description); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE findings SET plan_id=?, current_step_id=(SELECT step_id FROM remediation_steps WHERE plan_id=? ORDER BY ordinal LIMIT 1), updated_at=? WHERE finding_key=?`, planID, planID, now, input.FindingKey); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO finding_events(finding_key, plan_id, event_type, message, created_at) VALUES (?, ?, 'plan_created', 'remediation plan created', ?)`, input.FindingKey, planID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"plan_id": planID, "plan_revision": revision}, nil
}

func cliErr(code, message string, retryable bool) error {
	return &workflowError{Code: code, Message: message, Retryable: retryable}
}

type workflowError struct {
	Code, Message string
	Retryable     bool
}

func (e *workflowError) Error() string { return e.Message }

func event(ctx context.Context, db *Database, finding, eventType, message string) error {
	_, err := db.DB.ExecContext(ctx, `INSERT INTO finding_events(finding_key, event_type, message, created_at) VALUES (?, ?, ?, ?)`, finding, eventType, message, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func jsonString(value any) string { data, _ := json.Marshal(value); return string(data) }
func proxymaError(err error) (string, bool) {
	if typed, ok := err.(*workflowError); ok {
		return typed.Code, typed.Retryable
	}
	code := clientcli.ErrorCode(err)
	return code, code == "proxyma_unreachable" || code == "proxyma_error"
}
