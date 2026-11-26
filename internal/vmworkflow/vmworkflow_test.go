package vmworkflow

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxyma/internal/clientcli"
)

func TestImportCSVIsIdempotentAndKeepsObservationRows(t *testing.T) {
	db := testDatabase(t)
	profile := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "profiles", "nessus-detailed.json")
	fixture := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "tests", "fixtures", "nessus-detailed-sample.csv")
	first, err := ImportReport(context.Background(), db, profile, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 3 || first.Rows != 3 {
		t.Fatalf("first import = %#v", first)
	}
	second, err := ImportReport(context.Background(), db, profile, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate {
		t.Fatalf("second import was not idempotent: %#v", second)
	}
	var observations, findings int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM finding_observations`).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM findings`).Scan(&findings); err != nil {
		t.Fatal(err)
	}
	if observations != 3 || findings != 3 {
		t.Fatalf("observations=%d findings=%d", observations, findings)
	}
}

func TestClaimAndLeasePreventsSecondWorker(t *testing.T) {
	db := testDatabase(t)
	profile := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "profiles", "nessus-detailed.json")
	fixture := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "tests", "fixtures", "nessus-detailed-sample.csv")
	if _, err := ImportReport(context.Background(), db, profile, fixture); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, nil)
	if _, err := service.Claim(context.Background(), "worker-a", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Claim(context.Background(), "worker-a", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Claim(context.Background(), "worker-b", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestProfileRejectsNormalizedCVSSV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	content := `{"schema_version":"1","profile_id":"bad","profile_version":"1","formats":["csv"],"columns":{"cvss_v3_base_score":{"source_headers":["CVSS"]}},"identity_fields":["cvss_v3_base_score"],"preserve_unmapped_columns":true}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path); err == nil {
		t.Fatal("expected CVSS v3 profile rejection")
	}
}

func TestReadMinimalXLSX(t *testing.T) {
	data := makeXLSX(t)
	rows, headers, err := readXLSX(data, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if !headers["Host ID"] || len(rows) != 1 || rows[0].Values["Host ID"] != "host-1" || rows[0].Values["Plugin Output"] != "line 1\nline 2" {
		t.Fatalf("rows=%#v headers=%#v", rows, headers)
	}
}

func TestResolutionRequiresVerifiedCompletedPlan(t *testing.T) {
	db := testDatabase(t)
	profile := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "profiles", "nessus-detailed.json")
	fixture := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "tests", "fixtures", "nessus-detailed-sample.csv")
	if _, err := ImportReport(context.Background(), db, profile, fixture); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := db.DB.QueryRow(`SELECT finding_key FROM findings ORDER BY finding_key LIMIT 1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, nil)
	claimed, err := service.Claim(context.Background(), "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanCreate(context.Background(), PlanInput{FindingKey: claimed.FindingKey, Goal: "test remediation", WorkerID: "worker-a", Steps: []StepInput{{Kind: "remediate", Description: "apply fix"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	planView, err := service.InspectPlan(context.Background(), claimed.FindingKey)
	if err != nil {
		t.Fatal(err)
	}
	steps := planView["steps"].([]Step)
	if len(steps) != 1 || plan["plan_id"] == "" {
		t.Fatalf("unexpected plan: %#v", planView)
	}
	if _, err := service.Step(context.Background(), "start", steps[0].StepID, "", "", "worker-a"); err != nil {
		t.Fatal(err)
	}
	if err := service.FindingTransition(context.Background(), claimed.FindingKey, "resolve", "verified", "", "worker-a"); err == nil {
		t.Fatal("expected resolution to require completed verification")
	}
	if _, err := service.Step(context.Background(), "complete", steps[0].StepID, "fix applied", "verified by a follow-up check", "worker-a"); err != nil {
		t.Fatal(err)
	}
	if err := service.FindingTransition(context.Background(), claimed.FindingKey, "resolve", "verified", "", "worker-a"); err != nil {
		t.Fatal(err)
	}
	finding, err := service.ShowFinding(context.Background(), claimed.FindingKey)
	if err != nil {
		t.Fatal(err)
	}
	if finding.WorkflowStatus != "resolved" {
		t.Fatalf("status=%s", finding.WorkflowStatus)
	}
}

func TestAccessReconciliationBlocksAndRecovers(t *testing.T) {
	db := testDatabase(t)
	profile := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "profiles", "nessus-detailed.json")
	fixture := filepath.Join("..", "..", "harness", "workflows", "vulnerability-management", "tests", "fixtures", "nessus-detailed-sample.csv")
	if _, err := ImportReport(context.Background(), db, profile, fixture); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProxyma{resolveErr: cliErr("not_found", "target unavailable", false)}
	service := NewService(db, fake)
	blocked, err := service.ReconcileAccess(context.Background())
	if err != nil || blocked.Unavailable == 0 {
		t.Fatalf("blocked=%#v err=%v", blocked, err)
	}
	var blockedCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM findings WHERE workflow_status='blocked' AND blocked_reason='proxyma_access_unavailable'`).Scan(&blockedCount); err != nil {
		t.Fatal(err)
	}
	if blockedCount == 0 {
		t.Fatal("expected unavailable access to block findings")
	}
	fake.resolveErr = nil
	recovered, err := service.ReconcileAccess(context.Background())
	if err != nil || recovered.Available == 0 {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM findings WHERE workflow_status='ready' AND blocked_reason IS NULL`).Scan(&blockedCount); err != nil {
		t.Fatal(err)
	}
	if blockedCount != 3 {
		t.Fatalf("expected all findings to recover, ready=%d", blockedCount)
	}
	var events int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM finding_events WHERE event_type='unblocked'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events == 0 {
		t.Fatal("expected access recovery audit events")
	}
}

func TestBackupAndResetQuarantinesDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "workflow.sqlite")
	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	backup, err := db.Backup(context.Background(), backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	quarantine, err := db.Reset(context.Background(), backupDir, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(quarantine); err != nil {
		t.Fatal(err)
	}
	fresh, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	var findings int
	if err := fresh.DB.QueryRow(`SELECT COUNT(*) FROM findings`).Scan(&findings); err != nil {
		t.Fatal(err)
	}
	if findings != 0 {
		t.Fatalf("fresh database contains %d findings", findings)
	}
}

func TestRestoreValidatesAndAtomicallyReplacesDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "workflow.sqlite")
	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	backup, err := db.Backup(context.Background(), backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	oldPath, err := RestoreDatabase(context.Background(), dbPath, backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	integrity, err := restored.Integrity(context.Background())
	if err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
	diagnostics, err := NewService(restored, nil).Doctor(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics["schema_version"] != 1 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

type fakeProxyma struct {
	resolveErr error
}

func (f *fakeProxyma) Resolve(context.Context, clientcli.Target) (clientcli.Envelope, error) {
	if f.resolveErr != nil {
		return clientcli.Envelope{}, f.resolveErr
	}
	return clientcli.Envelope{OK: true}, nil
}

func (f *fakeProxyma) Submit(context.Context, clientcli.SubmitInput) (clientcli.Envelope, error) {
	return clientcli.Envelope{}, errors.New("not implemented")
}

func (f *fakeProxyma) Resume(context.Context) (clientcli.Envelope, error) {
	return clientcli.Envelope{OK: true}, nil
}

func testDatabase(t *testing.T) *Database {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "workflow.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeXLSX(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData></worksheet>`,
		"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Host ID</t></si><si><t>Plugin Output</t></si><si><t>host-1</t></si><si><t>line 1
line 2</t></si></sst>`,
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
