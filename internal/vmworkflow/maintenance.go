package vmworkflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Service) Doctor(ctx context.Context, checkProxyma bool) (map[string]any, error) {
	result := map[string]any{}
	integrity, err := s.DB.Integrity(ctx)
	if err != nil {
		return nil, err
	}
	result["integrity_check"] = integrity
	var foreignKeys []map[string]any
	rows, err := s.DB.DB.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		var row, parent, fk int
		if err := rows.Scan(&table, &row, &parent, &fk); err != nil {
			return nil, err
		}
		foreignKeys = append(foreignKeys, map[string]any{"table": table, "row": row, "parent": parent, "foreign_key": fk})
	}
	result["foreign_key_violations"] = foreignKeys
	var version int
	if err := s.DB.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return nil, err
	}
	result["schema_version"] = version
	tableCounts := map[string]int{}
	for _, table := range []string{"reports", "findings", "finding_observations", "remediation_plans", "remediation_steps", "finding_operations", "finding_events", "asset_access"} {
		var count int
		if err := s.DB.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return nil, err
		}
		tableCounts[table] = count
	}
	result["table_counts"] = tableCounts
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	result["status"] = status
	checks := map[string]int{}
	for _, query := range []struct{ name, sql string }{{"findings_without_valid_plan", `SELECT COUNT(*) FROM findings f WHERE f.workflow_status IN ('in_progress','verification_pending') AND (f.plan_id IS NULL OR NOT EXISTS (SELECT 1 FROM remediation_plans p WHERE p.plan_id=f.plan_id AND p.status='active'))`}, {"plans_without_steps", `SELECT COUNT(*) FROM remediation_plans p WHERE p.status='active' AND NOT EXISTS (SELECT 1 FROM remediation_steps s WHERE s.plan_id=p.plan_id)`}, {"operations_without_proxyma_task", `SELECT COUNT(*) FROM finding_operations WHERE operation_status IN ('submitted','waiting_proxyma') AND (client_task_id IS NULL OR client_task_id='')`}, {"orphaned_observations", `SELECT COUNT(*) FROM finding_observations o LEFT JOIN findings f ON f.finding_key=o.finding_key WHERE f.finding_key IS NULL`}, {"orphaned_events", `SELECT COUNT(*) FROM finding_events e LEFT JOIN findings f ON f.finding_key=e.finding_key WHERE f.finding_key IS NULL`}, {"stale_waiting_operations", `SELECT COUNT(*) FROM finding_operations WHERE operation_status='waiting_proxyma' AND (last_checked_at IS NULL OR last_checked_at < ?)`}} {
		var count int
		var scanErr error
		if strings.Contains(query.sql, "?") {
			scanErr = s.DB.DB.QueryRowContext(ctx, query.sql, s.now().Add(-30*time.Minute).Format(time.RFC3339Nano)).Scan(&count)
		} else {
			scanErr = s.DB.DB.QueryRowContext(ctx, query.sql).Scan(&count)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		checks[query.name] = count
	}
	result["checks"] = checks
	if checkProxyma && s.Proxy != nil {
		_, err := s.Proxy.Resume(ctx)
		result["proxyma_healthy"] = err == nil
		if err != nil {
			code, _ := proxymaError(err)
			if code == "" {
				code = "proxyma_error"
			}
			result["proxyma_error_code"] = code
		}
	}
	return result, nil
}

func (d *Database) Backup(ctx context.Context, output string) (string, error) {
	if output == "" {
		return "", fmt.Errorf("backup output is required")
	}
	if err := os.MkdirAll(output, 0o750); err != nil {
		return "", err
	}
	path := output
	if info, err := os.Stat(output); err == nil && info.IsDir() {
		path = filepath.Join(output, "vm-workflow-"+time.Now().UTC().Format("20060102T150405Z")+".sqlite")
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("backup destination already exists: %s", path)
	}
	escaped := strings.ReplaceAll(filepath.ToSlash(path), "'", "''")
	if _, err := d.DB.ExecContext(ctx, `VACUUM INTO '`+escaped+`'`); err != nil {
		return "", fmt.Errorf("create SQLite backup: %w", err)
	}
	return path, nil
}

func (d *Database) Reset(ctx context.Context, backupDir string, confirm, force bool) (string, error) {
	if !confirm {
		return "", fmt.Errorf("reset requires --confirm-reset")
	}
	if !force {
		var count int
		if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM findings WHERE lease_expires_at IS NOT NULL AND lease_expires_at > ?`, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count); err != nil {
			return "", err
		}
		if count > 0 {
			return "", fmt.Errorf("active leases exist; use --force only after stopping workers")
		}
	}
	if backupDir == "" {
		return "", fmt.Errorf("--backup-dir is required for reset")
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	quarantine := filepath.Join(backupDir, "vm-workflow-reset-"+stamp+".sqlite")
	if err := d.DB.Close(); err != nil {
		return "", err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		source := d.Path + suffix
		if _, err := os.Stat(source); err == nil {
			destination := quarantine + suffix
			if err := os.Rename(source, destination); err != nil {
				return "", fmt.Errorf("quarantine %s: %w", source, err)
			}
		}
	}
	newDB, err := OpenDatabase(d.Path)
	if err != nil {
		return "", err
	}
	_ = newDB.Close()
	return quarantine, nil
}

func RestoreDatabase(ctx context.Context, currentPath, backupPath string) (string, error) {
	if currentPath == "" || backupPath == "" {
		return "", fmt.Errorf("current and backup database paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o750); err != nil {
		return "", err
	}
	testPath := currentPath + ".restore-check-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := copyFile(backupPath, testPath); err != nil {
		return "", err
	}
	checked, err := OpenDatabase(testPath)
	if err != nil {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(testPath + suffix)
		}
		return "", fmt.Errorf("backup failed validation: %w", err)
	}
	_ = checked.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(testPath + suffix)
	}
	stagedPath := currentPath + ".restore-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := copyFile(backupPath, stagedPath); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	oldPath := currentPath + ".before-restore-" + stamp
	if _, err := os.Stat(currentPath); err == nil {
		if err := os.Rename(currentPath, oldPath); err != nil {
			_ = os.Remove(stagedPath)
			return "", err
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(currentPath + suffix); err == nil {
			if err := os.Rename(currentPath+suffix, oldPath+suffix); err != nil {
				_ = os.Remove(stagedPath)
				return "", err
			}
		}
	}
	if err := os.Rename(stagedPath, currentPath); err != nil {
		_ = os.Remove(stagedPath)
		return "", err
	}
	return oldPath, nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return output.Sync()
}
