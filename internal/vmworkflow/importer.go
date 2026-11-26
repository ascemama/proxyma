package vmworkflow

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type reportRow struct {
	Number int
	Values map[string]string
	Raw    string
}

type normalizedRecord struct {
	Row                                                                                                                reportRow
	AssetID, AssetIP, DNS, NetBIOS, AgentID, Repository                                                                string
	PluginID, PluginName, PluginFamily, PluginVersion, Protocol, Port, CheckType                                       string
	Severity, RiskFactor, STIGSeverity                                                                                 string
	VPR, EPSS, CVSSBase, CVSSThreat                                                                                    *float64
	Exploit                                                                                                            *bool
	CVSSVector, CVSSThreatVector, CVSSSupplemental                                                                     string
	CVE, CPE, CrossReferences                                                                                          []string
	Description, Synopsis, Evidence, Remediation                                                                       string
	FirstDiscovered, LastObserved, VulnPublication, PatchPublication, EndOfLife, PluginPublication, PluginModification string
	RecastRiskComment, AcceptRiskComment                                                                               string
	InstanceKey, FindingKey                                                                                            string
}

func ImportReport(ctx context.Context, db *Database, profilePath, inputPath string) (ImportSummary, error) {
	profile, err := LoadProfile(profilePath)
	if err != nil {
		return ImportSummary{}, err
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return ImportSummary{}, fmt.Errorf("read report: %w", err)
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(inputPath)), ".")
	if format == "xls" {
		return ImportSummary{}, fmt.Errorf("legacy XLS reports are not supported; export CSV or XLSX")
	}
	formatAllowed := false
	for _, supported := range profile.Formats {
		if supported == format {
			formatAllowed = true
			break
		}
	}
	if !formatAllowed {
		return ImportSummary{}, fmt.Errorf("profile %q does not support %s reports", profile.ProfileID, format)
	}
	rows, headers, err := readReport(data, filepath.Ext(inputPath), profile)
	if err != nil {
		return ImportSummary{}, err
	}
	for _, mapping := range profile.Columns {
		if mapping.Required {
			found := false
			for _, source := range mapping.SourceHeaders {
				if _, ok := headers[source]; ok {
					found = true
					break
				}
			}
			if !found {
				return ImportSummary{}, fmt.Errorf("required report column missing: %s", strings.Join(mapping.SourceHeaders, ", "))
			}
		}
	}
	records := make([]normalizedRecord, 0, len(rows))
	counts := map[string]int{}
	for _, row := range rows {
		record, err := normalizeRecord(row, profile)
		if err != nil {
			return ImportSummary{}, fmt.Errorf("row %d: %w", row.Number, err)
		}
		base := strings.Join([]string{normalize(record.AssetID), normalize(record.PluginID), normalize(record.Protocol), normalize(record.Port)}, "\x00")
		counts[base]++
		records = append(records, record)
	}
	seen := map[string]int{}
	for i := range records {
		record := &records[i]
		base := strings.Join([]string{normalize(record.AssetID), normalize(record.PluginID), normalize(record.Protocol), normalize(record.Port)}, "\x00")
		if counts[base] > 1 {
			sum := sha256.Sum256([]byte(normalize(record.Evidence)))
			record.InstanceKey = hex.EncodeToString(sum[:])[:16]
			seen[base+"\x00"+record.InstanceKey]++
			if seen[base+"\x00"+record.InstanceKey] > 1 {
				record.InstanceKey += "-" + strconv.Itoa(seen[base+"\x00"+record.InstanceKey])
			}
		}
		record.FindingKey = findingKey(record.AssetID, record.PluginID, record.Protocol, record.Port, record.InstanceKey)
	}
	reportHash := sha256.Sum256(data)
	reportSHA := hex.EncodeToString(reportHash[:])
	var existing string
	if err := db.DB.QueryRowContext(ctx, `SELECT report_id FROM reports WHERE report_sha256 = ?`, reportSHA).Scan(&existing); err == nil {
		return ImportSummary{ReportID: existing, Duplicate: true, Rows: len(rows), Findings: len(records)}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ImportSummary{}, fmt.Errorf("check report provenance: %w", err)
	}
	reportID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return ImportSummary{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO reports(report_id, report_sha256, source_profile, source_profile_version, source_filename, imported_at, raw_metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?)`, reportID, reportSHA, profile.ProfileID, profile.ProfileVersion, filepath.Base(inputPath), now, `{}`); err != nil {
		return ImportSummary{}, fmt.Errorf("insert report: %w", err)
	}
	summary := ImportSummary{ReportID: reportID, Rows: len(rows), Findings: len(records)}
	for _, record := range records {
		created, reopened, err := upsertFinding(ctx, tx, record, reportID, now)
		if err != nil {
			return ImportSummary{}, err
		}
		if created {
			summary.Created++
		} else {
			summary.Updated++
		}
		if reopened {
			summary.Reopened++
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO finding_observations(report_id, finding_key, source_row_number, observed_at, raw_record_json) VALUES (?, ?, ?, ?, ?)`, reportID, record.FindingKey, record.Row.Number, now, record.Row.Raw); err != nil {
			return ImportSummary{}, fmt.Errorf("insert observation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportSummary{}, err
	}
	return summary, nil
}

func upsertFinding(ctx context.Context, tx *sql.Tx, record normalizedRecord, reportID, now string) (bool, bool, error) {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT workflow_status FROM findings WHERE finding_key = ?`, record.FindingKey).Scan(&status)
	existing := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, false, err
	}
	args := findingArgs(record, reportID, now)
	if !existing {
		insertSQL := `INSERT INTO findings(
		finding_key, report_id, asset_id, asset_ip, asset_dns_name, asset_netbios_name, agent_id, repository,
		plugin_id, plugin_name, instance_key, plugin_family, plugin_version, protocol, port, check_type, severity,
		risk_factor, vpr, epss, exploit_available, stig_severity, cvss_v4_base_score, cvss_v4_threat_score,
		cvss_v4_vector, cvss_v4_threat_vector, cvss_v4_supplemental, cve_json, cpe_json, cross_references_json, description, synopsis,
		evidence, remediation, first_discovered_at, last_observed_at, vulnerability_publication_date, patch_publication_date,
		security_end_of_life_date, plugin_publication_date, plugin_modification_date, recast_risk_comment, accept_risk_comment,
		raw_record_json, created_at, updated_at)
		VALUES (` + placeholders(len(args)-2) + `, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
		_, err := tx.ExecContext(ctx, insertSQL, args[:len(args)-2]...)
		if err != nil {
			return false, false, fmt.Errorf("insert finding %s: %w", record.FindingKey, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO finding_events(finding_key, event_type, message, created_at) VALUES (?, 'imported', 'finding imported', ?)`, record.FindingKey, now); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	reopened := status == "resolved"
	query := `UPDATE findings SET report_id=?, asset_id=?, asset_ip=?, asset_dns_name=?, asset_netbios_name=?, agent_id=?, repository=?, plugin_id=?, plugin_name=?, instance_key=?, plugin_family=?, plugin_version=?, protocol=?, port=?, check_type=?, severity=?, risk_factor=?, vpr=?, epss=?, exploit_available=?, stig_severity=?, cvss_v4_base_score=?, cvss_v4_threat_score=?, cvss_v4_vector=?, cvss_v4_threat_vector=?, cvss_v4_supplemental=?, cve_json=?, cpe_json=?, cross_references_json=?, description=?, synopsis=?, evidence=?, remediation=?, first_discovered_at=?, last_observed_at=?, vulnerability_publication_date=?, patch_publication_date=?, security_end_of_life_date=?, plugin_publication_date=?, plugin_modification_date=?, recast_risk_comment=?, accept_risk_comment=?, raw_record_json=?, updated_at=?`
	argsUpdate := args[1 : len(args)-1]
	if reopened {
		query += `, workflow_status='ready', reopened_at=?`
		argsUpdate = append(argsUpdate, now)
	}
	query += ` WHERE finding_key=?`
	argsUpdate = append(argsUpdate, record.FindingKey)
	if _, err := tx.ExecContext(ctx, query, argsUpdate...); err != nil {
		return false, false, fmt.Errorf("update finding %s: %w", record.FindingKey, err)
	}
	event := "observed"
	if reopened {
		event = "reopened"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO finding_events(finding_key, event_type, message, created_at) VALUES (?, ?, ?, ?)`, record.FindingKey, event, "finding observed in report", now); err != nil {
		return false, false, err
	}
	return false, reopened, nil
}

func findingArgs(r normalizedRecord, reportID, now string) []any {
	cve, _ := json.Marshal(r.CVE)
	cpe, _ := json.Marshal(r.CPE)
	cross, _ := json.Marshal(r.CrossReferences)
	return []any{r.FindingKey, reportID, r.AssetID, nullable(r.AssetIP), nullable(r.DNS), nullable(r.NetBIOS), nullable(r.AgentID), nullable(r.Repository), r.PluginID, r.PluginName, r.InstanceKey, nullable(r.PluginFamily), nullable(r.PluginVersion), nullable(r.Protocol), nullable(r.Port), nullable(r.CheckType), nullable(r.Severity), nullable(r.RiskFactor), r.VPR, r.EPSS, boolValue(r.Exploit), nullable(r.STIGSeverity), r.CVSSBase, r.CVSSThreat, nullable(r.CVSSVector), nullable(r.CVSSThreatVector), nullable(r.CVSSSupplemental), string(cve), string(cpe), string(cross), nullable(r.Description), nullable(r.Synopsis), nullable(r.Evidence), nullable(r.Remediation), nullable(r.FirstDiscovered), nullable(r.LastObserved), nullable(r.VulnPublication), nullable(r.PatchPublication), nullable(r.EndOfLife), nullable(r.PluginPublication), nullable(r.PluginModification), nullable(r.RecastRiskComment), nullable(r.AcceptRiskComment), r.Row.Raw, now, now}
}

func placeholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
func boolValue(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return 1
	}
	return 0
}

func normalizeRecord(row reportRow, profile Profile) (normalizedRecord, error) {
	v := func(field string) string {
		mapping := profile.Columns[field]
		for _, source := range mapping.SourceHeaders {
			if value, ok := row.Values[source]; ok {
				return normalize(value)
			}
		}
		return ""
	}
	floatValue := func(field string) (*float64, error) {
		raw := v(field)
		if raw == "" {
			return nil, nil
		}
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(raw), "%"), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid %s value %q", field, raw)
		}
		if field == "epss" && (strings.Contains(raw, "%") || n > 1) {
			n /= 100
		}
		return &n, nil
	}
	boolValue := func(field string) (*bool, error) {
		raw := strings.ToLower(v(field))
		if raw == "" || raw == "n/a" {
			return nil, nil
		}
		switch raw {
		case "yes", "true", "1":
			b := true
			return &b, nil
		case "no", "false", "0":
			b := false
			return &b, nil
		}
		return nil, fmt.Errorf("invalid %s value %q", field, raw)
	}
	var err error
	r := normalizedRecord{Row: row, AssetID: v("asset_id"), AssetIP: v("asset_ip"), DNS: v("asset_dns_name"), NetBIOS: v("asset_netbios_name"), AgentID: v("agent_id"), Repository: v("repository"), PluginID: v("plugin_id"), PluginName: v("plugin_name"), PluginFamily: v("plugin_family"), PluginVersion: v("plugin_version"), Protocol: v("protocol"), Port: v("port"), CheckType: v("check_type"), Severity: v("severity"), RiskFactor: v("risk_factor"), STIGSeverity: v("stig_severity"), CVSSVector: v("cvss_v4_vector"), CVSSThreatVector: v("cvss_v4_threat_vector"), CVSSSupplemental: v("cvss_v4_supplemental"), Description: v("description"), Synopsis: v("synopsis"), Evidence: v("plugin_output"), Remediation: v("remediation"), RecastRiskComment: v("recast_risk_comment"), AcceptRiskComment: v("accept_risk_comment")}
	if r.AssetID == "" || r.PluginID == "" || r.PluginName == "" {
		return r, fmt.Errorf("asset_id, plugin_id, and plugin_name are required")
	}
	if r.VPR, err = floatValue("vpr"); err != nil {
		return r, err
	}
	if r.EPSS, err = floatValue("epss"); err != nil {
		return r, err
	}
	if r.CVSSBase, err = floatValue("cvss_v4_base_score"); err != nil {
		return r, err
	}
	if r.CVSSThreat, err = floatValue("cvss_v4_threat_score"); err != nil {
		return r, err
	}
	if r.Exploit, err = boolValue("exploit_available"); err != nil {
		return r, err
	}
	for _, item := range []struct {
		field string
		dst   *[]string
	}{{"cve", &r.CVE}, {"cpe", &r.CPE}, {"cross_references", &r.CrossReferences}} {
		*item.dst = splitList(v(item.field))
	}
	for _, item := range []struct {
		field string
		dst   *string
	}{{"first_discovered_at", &r.FirstDiscovered}, {"last_observed_at", &r.LastObserved}, {"vulnerability_publication_date", &r.VulnPublication}, {"patch_publication_date", &r.PatchPublication}, {"security_end_of_life_date", &r.EndOfLife}, {"plugin_publication_date", &r.PluginPublication}, {"plugin_modification_date", &r.PluginModification}} {
		*item.dst, err = normalizeDate(v(item.field))
		if err != nil {
			return r, fmt.Errorf("%s: %w", item.field, err)
		}
	}
	return r, nil
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func normalizeDate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "n/a") || strings.EqualFold(value, "na") {
		return "", nil
	}
	layouts := []string{time.RFC3339, "Jan 2, 2006 15:04:05 MST", "2006-01-02 15:04:05 -0700", "01/02/2006 03:04 PM", "2006-01-02", "01/02/2006"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("unsupported date %q", value)
}

func readReport(data []byte, extension string, profile Profile) ([]reportRow, map[string]bool, error) {
	if strings.EqualFold(extension, ".xlsx") {
		return readXLSX(data, profile)
	}
	return readCSV(data, profile)
}

func readCSV(data []byte, profile Profile) ([]reportRow, map[string]bool, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	if profile.CSV.Delimiter != "" {
		runes := []rune(profile.CSV.Delimiter)
		if len(runes) == 1 {
			r.Comma = runes[0]
		}
	}
	headers, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read CSV header: %w", err)
	}
	return rowsFromValues(headers, r)
}

func rowsFromValues(headers []string, r interface{ Read() ([]string, error) }) ([]reportRow, map[string]bool, error) {
	headerSet := map[string]bool{}
	for _, h := range headers {
		headerSet[h] = true
	}
	var rows []reportRow
	number := 1
	for {
		values, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read report row %d: %w", number+1, err)
		}
		number++
		record := map[string]string{}
		for i, h := range headers {
			if i < len(values) {
				record[h] = values[i]
			} else {
				record[h] = ""
			}
		}
		raw, _ := json.Marshal(record)
		rows = append(rows, reportRow{Number: number, Values: record, Raw: string(raw)})
	}
	return rows, headerSet, nil
}

type xlsxWorkbook struct {
	Sheets []struct {
		Name  string `xml:"name,attr"`
		RelID string `xml:"{http://schemas.openxmlformats.org/officeDocument/2006/relationships}id,attr"`
	} `xml:"sheets>sheet"`
}
type xlsxRelationships struct {
	Relationships []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}
type xlsxSharedStrings struct {
	Items []struct {
		Text string `xml:"t"`
	} `xml:"si"`
}
type xlsxSheet struct {
	Rows []struct {
		Cells []struct {
			Ref    string `xml:"r,attr"`
			Type   string `xml:"t,attr"`
			Value  string `xml:"v"`
			Inline string `xml:"is>t"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

func readXLSX(data []byte, profile Profile) ([]reportRow, map[string]bool, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("read XLSX: %w", err)
	}
	files := map[string][]byte{}
	for _, file := range archive.File {
		if file.Name == "xl/workbook.xml" || file.Name == "xl/_rels/workbook.xml.rels" || file.Name == "xl/sharedStrings.xml" || strings.HasPrefix(file.Name, "xl/worksheets/sheet") {
			rc, e := file.Open()
			if e != nil {
				return nil, nil, e
			}
			content, e := io.ReadAll(rc)
			_ = rc.Close()
			if e != nil {
				return nil, nil, e
			}
			files[file.Name] = content
		}
	}
	var workbook xlsxWorkbook
	if err := xml.Unmarshal(files["xl/workbook.xml"], &workbook); err != nil {
		return nil, nil, fmt.Errorf("decode XLSX workbook: %w", err)
	}
	selectedSheet := 0
	if profile.XLSX.Sheet != nil && strings.TrimSpace(*profile.XLSX.Sheet) != "" {
		selectedSheet = -1
		for index, sheet := range workbook.Sheets {
			if sheet.Name == strings.TrimSpace(*profile.XLSX.Sheet) {
				selectedSheet = index
				break
			}
		}
		if selectedSheet < 0 {
			return nil, nil, fmt.Errorf("XLSX sheet %q was not found", *profile.XLSX.Sheet)
		}
	}
	relTarget := "xl/worksheets/sheet1.xml"
	var rels xlsxRelationships
	if len(files["xl/_rels/workbook.xml.rels"]) > 0 {
		_ = xml.Unmarshal(files["xl/_rels/workbook.xml.rels"], &rels)
		if len(workbook.Sheets) > selectedSheet {
			selectedRelationship := workbook.Sheets[selectedSheet].RelID
			for _, rel := range rels.Relationships {
				if rel.ID == selectedRelationship {
					relTarget = "xl/" + strings.TrimPrefix(rel.Target, "/xl/")
					break
				}
			}
		}
	}
	var shared xlsxSharedStrings
	if len(files["xl/sharedStrings.xml"]) > 0 {
		_ = xml.Unmarshal(files["xl/sharedStrings.xml"], &shared)
	}
	var sheet xlsxSheet
	if err := xml.Unmarshal(files[relTarget], &sheet); err != nil {
		return nil, nil, fmt.Errorf("decode XLSX sheet: %w", err)
	}
	var values [][]string
	for _, row := range sheet.Rows {
		max := 0
		cells := map[int]string{}
		for _, cell := range row.Cells {
			col := columnNumber(cell.Ref)
			if col > max {
				max = col
			}
			value := cell.Value
			if cell.Type == "s" {
				index, _ := strconv.Atoi(value)
				if index >= 0 && index < len(shared.Items) {
					value = shared.Items[index].Text
				}
			}
			if cell.Inline != "" {
				value = cell.Inline
			}
			cells[col] = value
		}
		line := make([]string, max)
		for i := 1; i <= max; i++ {
			line[i-1] = cells[i]
		}
		values = append(values, line)
	}
	headerRow := profile.XLSX.HeaderRow
	if headerRow == 0 {
		headerRow = 1
	}
	if len(values) < headerRow {
		return nil, nil, fmt.Errorf("XLSX contains no rows")
	}
	fake := &sliceReader{rows: values[headerRow:]}
	return rowsFromValues(values[headerRow-1], fake)
}

type sliceReader struct {
	rows  [][]string
	index int
}

func (r *sliceReader) Read() ([]string, error) {
	if r.index >= len(r.rows) {
		return nil, io.EOF
	}
	value := r.rows[r.index]
	r.index++
	return value, nil
}
func columnNumber(ref string) int {
	n := 0
	for _, r := range strings.ToUpper(ref) {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n
}
