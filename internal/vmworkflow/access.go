package vmworkflow

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"proxyma/internal/clientcli"
)

func (s *Service) ReconcileAccess(ctx context.Context) (AccessSummary, error) {
	if s.Proxy == nil {
		return AccessSummary{}, cliErr("proxyma_unavailable", "Proxyma integration is not configured", true)
	}
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT asset_id, COALESCE(MAX(asset_ip),''), COALESCE(MAX(asset_dns_name),'') FROM findings GROUP BY asset_id`)
	if err != nil {
		return AccessSummary{}, err
	}
	defer rows.Close()
	var assets []struct{ id, ip, dns string }
	for rows.Next() {
		var a struct{ id, ip, dns string }
		if err := rows.Scan(&a.id, &a.ip, &a.dns); err != nil {
			return AccessSummary{}, err
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		return AccessSummary{}, err
	}
	result := AccessSummary{}
	for _, asset := range assets {
		result.Checked++
		target := clientcli.Target{IP: asset.ip}
		if target.IP == "" {
			target.Hostname = asset.dns
		}
		status, reason := "available", ""
		if target.IP == "" && target.Hostname == "" {
			status, reason = "unknown", "target_identity_missing"
		} else {
			_, err := s.Proxy.Resolve(ctx, target)
			if err != nil {
				code, retryable := proxymaError(err)
				if code == "not_found" || code == "unauthorized" || code == "forbidden" {
					status, reason = "unavailable", "proxyma_access_unavailable"
				} else if retryable {
					status, reason = "unknown", code
				} else {
					status, reason = "unknown", "proxyma_check_failed"
				}
			}
		}
		if status == "available" {
			result.Available++
		}
		if status == "unavailable" {
			result.Unavailable++
		}
		if status == "unknown" {
			result.Unknown++
		}
		changed, err := s.storeAccess(ctx, asset.id, asset.ip, asset.dns, status, reason)
		if err != nil {
			return result, err
		}
		if changed {
			result.Changed++
		}
	}
	return result, nil
}

func (s *Service) storeAccess(ctx context.Context, assetID, ip, dns, status, reason string) (bool, error) {
	key := assetKey(assetID)
	now := s.now().Format(time.RFC3339Nano)
	var previous string
	err := s.DB.DB.QueryRowContext(ctx, `SELECT access_status FROM asset_access WHERE asset_key=?`, key).Scan(&previous)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if _, err := s.DB.DB.ExecContext(ctx, `INSERT INTO asset_access(asset_key, asset_id, asset_ip, asset_dns_name, access_status, reason_code, checked_at, details_json) VALUES (?, ?, ?, ?, ?, ?, ?, '{}') ON CONFLICT(asset_key) DO UPDATE SET asset_ip=excluded.asset_ip, asset_dns_name=excluded.asset_dns_name, access_status=excluded.access_status, reason_code=excluded.reason_code, checked_at=excluded.checked_at`, key, assetID, nullable(ip), nullable(dns), status, nullable(reason), now); err != nil {
		return false, err
	}
	changed := !exists || previous != status
	if status == "unavailable" {
		_, _ = s.DB.DB.ExecContext(ctx, `UPDATE findings SET workflow_status='blocked', blocked_reason='proxyma_access_unavailable', required_next_action='provide or authorize Proxyma access', lease_owner=NULL, lease_expires_at=NULL, updated_at=? WHERE asset_id=? AND workflow_status='ready'`, now, assetID)
		if changed {
			_ = s.recordAssetEvents(ctx, assetID, "blocked", now)
		}
	}
	if status == "available" {
		if changed {
			// Capture the blocked findings before clearing their reason so the
			// access recovery is visible in the audit history.
			if err := s.recordAssetEvents(ctx, assetID, "unblocked", now); err != nil {
				return false, err
			}
		}
		_, _ = s.DB.DB.ExecContext(ctx, `UPDATE findings SET workflow_status='ready', blocked_reason=NULL, required_next_action=NULL, updated_at=? WHERE asset_id=? AND workflow_status='blocked' AND blocked_reason='proxyma_access_unavailable'`, now, assetID)
	}
	return changed, nil
}

func (s *Service) recordAssetEvents(ctx context.Context, assetID, kind, now string) error {
	rows, err := s.DB.DB.QueryContext(ctx, `SELECT finding_key FROM findings WHERE asset_id=? AND blocked_reason='proxyma_access_unavailable'`, assetID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		if _, err := s.DB.DB.ExecContext(ctx, `INSERT INTO finding_events(finding_key, event_type, message, created_at) VALUES (?, ?, ?, ?)`, key, kind, "Proxyma asset access state changed", now); err != nil {
			return err
		}
	}
	return nil
}

func accessTarget(f Finding) clientcli.Target {
	if strings.TrimSpace(f.AssetIP) != "" {
		return clientcli.Target{IP: f.AssetIP}
	}
	return clientcli.Target{Hostname: f.AssetDNSName}
}
