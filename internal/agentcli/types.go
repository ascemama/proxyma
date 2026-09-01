// Package agentcli implements the deterministic Proxyma harness client.
package agentcli

import "time"

const (
	ProtocolVersion = "1"
	StateVersion    = "2"
)

type Target struct {
	Hostname string `json:"hostname,omitempty"`
	IP       string `json:"ip,omitempty"`
}

type SafeError struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable,omitempty"`
	Hint      string    `json:"hint,omitempty"`
	At        time.Time `json:"at,omitempty"`
}

type Result struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitStatus *int   `json:"exit_status"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

type Operation struct {
	OperationID      string         `json:"operation_id"`
	RequestHash      string         `json:"request_hash"`
	Phase            string         `json:"phase"`
	Target           Target         `json:"target"`
	Payload          string         `json:"payload"`
	PayloadKind      string         `json:"payload_kind"`
	CanonicalCommand string         `json:"canonical_command"`
	Reasoning        string         `json:"reasoning"`
	RequestedBy      string         `json:"requested_by"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	ProxymaTaskID    string         `json:"proxyma_task_id,omitempty"`
	ProxymaStatus    string         `json:"proxyma_status,omitempty"`
	PreparedAt       time.Time      `json:"prepared_at"`
	SubmittedAt      *time.Time     `json:"submitted_at,omitempty"`
	CheckedAt        *time.Time     `json:"checked_at,omitempty"`
	Result           *Result        `json:"result,omitempty"`
	LastError        *SafeError     `json:"last_error,omitempty"`
}

type TaskState struct {
	SchemaVersion      string      `json:"schema_version"`
	ClientTaskID       string      `json:"client_task_id"`
	Revision           int64       `json:"revision"`
	UserGoal           string      `json:"user_goal"`
	Status             string      `json:"status"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
	LastCheckedAt      *time.Time  `json:"last_checked_at,omitempty"`
	RemainingWork      string      `json:"remaining_work"`
	Operations         []Operation `json:"operations"`
	BlockedReason      string      `json:"blocked_reason,omitempty"`
	RequiredNextAction string      `json:"required_next_action,omitempty"`
	FailureReason      string      `json:"failure_reason,omitempty"`
	FinalSummary       string      `json:"final_summary,omitempty"`
}

type SubmitInput struct {
	ProtocolVersion  string         `json:"protocol_version,omitempty"`
	ClientTaskID     string         `json:"client_task_id,omitempty"`
	ExpectedRevision *int64         `json:"expected_revision,omitempty"`
	UserGoal         string         `json:"user_goal,omitempty"`
	Target           Target         `json:"target"`
	Payload          string         `json:"payload"`
	Reasoning        string         `json:"reasoning"`
	RequestedBy      string         `json:"requested_by"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type UpdateInput struct {
	ProtocolVersion    string `json:"protocol_version,omitempty"`
	ClientTaskID       string `json:"client_task_id"`
	ExpectedRevision   int64  `json:"expected_revision"`
	RemainingWork      string `json:"remaining_work,omitempty"`
	Status             string `json:"status,omitempty"`
	BlockedReason      string `json:"blocked_reason,omitempty"`
	RequiredNextAction string `json:"required_next_action,omitempty"`
	FailureReason      string `json:"failure_reason,omitempty"`
}

type CompleteInput struct {
	ProtocolVersion  string `json:"protocol_version,omitempty"`
	ClientTaskID     string `json:"client_task_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	FinalSummary     string `json:"final_summary"`
}

type Counts struct {
	Checked   int `json:"checked"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Errors    int `json:"errors"`
}

type ResolvedTarget struct {
	Target struct {
		Transport string `json:"transport"`
		OS        string `json:"os"`
		Hostname  string `json:"hostname,omitempty"`
		IP        string `json:"ip,omitempty"`
	} `json:"target"`
	CanonicalCommand struct {
		Prefix      string `json:"prefix"`
		PayloadKind string `json:"payload_kind"`
	} `json:"canonical_command"`
}

type HelpFlag struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

type HelpField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

type HelpCommand struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type HelpInfo struct {
	Usage       string        `json:"usage"`
	Summary     string        `json:"summary"`
	Flags       []HelpFlag    `json:"flags,omitempty"`
	InputFields []HelpField   `json:"input_fields,omitempty"`
	Example     any           `json:"example,omitempty"`
	Notes       []string      `json:"notes,omitempty"`
	Commands    []HelpCommand `json:"commands,omitempty"`
}

type Envelope struct {
	ProtocolVersion string          `json:"protocol_version"`
	OK              bool            `json:"ok"`
	Command         string          `json:"command"`
	Action          string          `json:"action,omitempty"`
	Task            *TaskState      `json:"task,omitempty"`
	Tasks           []TaskState     `json:"tasks,omitempty"`
	Counts          *Counts         `json:"counts,omitempty"`
	Target          *ResolvedTarget `json:"target,omitempty"`
	Version         string          `json:"version,omitempty"`
	Commit          string          `json:"commit,omitempty"`
	StateSchema     string          `json:"state_schema,omitempty"`
	Help            *HelpInfo       `json:"help,omitempty"`
	Error           *SafeError      `json:"error,omitempty"`
	Warnings        []string        `json:"warnings,omitempty"`
}

type commandError struct {
	code      string
	message   string
	retryable bool
	hint      string
	exitCode  int
	task      *TaskState
}

func (e *commandError) Error() string { return e.message }

func cliError(code, message string, exitCode int) *commandError {
	return &commandError{code: code, message: message, exitCode: exitCode}
}
