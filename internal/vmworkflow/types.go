package vmworkflow

import "time"

const (
	SchemaVersion   = "1"
	ProtocolVersion = "1"
)

var workflowStatuses = []string{"ready", "in_progress", "waiting_proxyma", "verification_pending", "blocked", "unsupported", "failed", "resolved"}

type Envelope struct {
	ProtocolVersion string     `json:"protocol_version"`
	OK              bool       `json:"ok"`
	Command         string     `json:"command"`
	Action          string     `json:"action,omitempty"`
	Data            any        `json:"data,omitempty"`
	Warnings        []string   `json:"warnings,omitempty"`
	Error           *SafeError `json:"error,omitempty"`
}

type SafeError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

type Finding struct {
	FindingKey         string     `json:"finding_key"`
	AssetID            string     `json:"asset_id"`
	AssetIP            string     `json:"asset_ip,omitempty"`
	AssetDNSName       string     `json:"asset_dns_name,omitempty"`
	AssetNetBIOSName   string     `json:"asset_netbios_name,omitempty"`
	PluginID           string     `json:"plugin_id"`
	PluginName         string     `json:"plugin_name"`
	InstanceKey        string     `json:"instance_key,omitempty"`
	Protocol           string     `json:"protocol,omitempty"`
	Port               string     `json:"port,omitempty"`
	Severity           string     `json:"severity,omitempty"`
	VPR                *float64   `json:"vpr,omitempty"`
	EPSS               *float64   `json:"epss,omitempty"`
	CVSSV4BaseScore    *float64   `json:"cvss_v4_base_score,omitempty"`
	CVSSV4ThreatScore  *float64   `json:"cvss_v4_threat_score,omitempty"`
	Evidence           string     `json:"evidence,omitempty"`
	Remediation        string     `json:"remediation,omitempty"`
	WorkflowStatus     string     `json:"workflow_status"`
	PlanID             string     `json:"plan_id,omitempty"`
	CurrentStepID      string     `json:"current_step_id,omitempty"`
	AttemptCount       int        `json:"attempt_count"`
	LeaseOwner         string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt     *time.Time `json:"lease_expires_at,omitempty"`
	NextAttemptAt      *time.Time `json:"next_attempt_at,omitempty"`
	BlockedReason      string     `json:"blocked_reason,omitempty"`
	RequiredNextAction string     `json:"required_next_action,omitempty"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
	ReopenedAt         *time.Time `json:"reopened_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type PlanInput struct {
	FindingKey  string      `json:"finding_key"`
	Goal        string      `json:"goal"`
	Assumptions string      `json:"assumptions,omitempty"`
	Steps       []StepInput `json:"steps"`
	WorkerID    string      `json:"worker_id,omitempty"`
}

type StepInput struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type OperationInput struct {
	FindingKey    string `json:"finding_key"`
	StepID        string `json:"step_id"`
	OperationKind string `json:"operation_kind"`
	Payload       string `json:"payload"`
	Reasoning     string `json:"reasoning"`
	WorkerID      string `json:"worker_id,omitempty"`
}

type StatusSummary struct {
	Counts            map[string]int `json:"counts"`
	ActionRequired    bool           `json:"action_required"`
	ExpiredLeases     int            `json:"expired_leases"`
	PendingOperations int            `json:"pending_operations"`
}

type AccessSummary struct {
	Checked     int `json:"checked"`
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
	Unknown     int `json:"unknown"`
	Changed     int `json:"changed"`
}

type ImportSummary struct {
	ReportID  string `json:"report_id"`
	Duplicate bool   `json:"duplicate"`
	Rows      int    `json:"rows"`
	Findings  int    `json:"findings"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Reopened  int    `json:"reopened"`
}

type Step struct {
	StepID               string `json:"step_id"`
	PlanID               string `json:"plan_id"`
	Ordinal              int    `json:"ordinal"`
	Kind                 string `json:"kind"`
	Description          string `json:"description"`
	Status               string `json:"status"`
	OperationID          string `json:"operation_id,omitempty"`
	ResultSummary        string `json:"result_summary,omitempty"`
	VerificationEvidence string `json:"verification_evidence,omitempty"`
}
