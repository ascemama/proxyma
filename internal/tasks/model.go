// Package tasks owns Proxyma task records, task state mutation, and task execution primitives.
package tasks

import (
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/transcriptor"
)

const (
	StatusCreated             = "created"
	StatusWaitingUserResponse = "waiting_user_response"
	StatusAcceptedByUser      = "accepted_by_user"
	StatusRunning             = "running"
	StatusCompleted           = "completed"
	StatusFailed              = "failed"
	StatusTimedOut            = "timed_out"
	StatusInterrupted         = "interrupted"
	StatusAbandoned           = "abandoned"
	StatusBlocked             = "blocked"
)

const (
	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved"
	ApprovalStatusRejected = "rejected"
	ApprovalStatusExpired  = "expired"
	ApprovalStatusCanceled = "canceled"
)

const (
	PlaceholderUser     = "{user}"
	PlaceholderPassword = "{password}"
)

// CommandContext is the agent-provided JSON command request.
type CommandContext struct {
	ClientOperationID     string                 `json:"-"`
	ClientRequestHash     string                 `json:"-"`
	Command               string                 `json:"command"`
	Reasoning             string                 `json:"reasoning"`
	RequestedBy           string                 `json:"requested_by,omitempty"`
	InteractionInstanceID string                 `json:"-"`
	AgentID               string                 `json:"-"`
	Metadata              map[string]interface{} `json:"metadata,omitempty"`
	Target                *Target                `json:"-"`
	Policy                *PolicyDecision        `json:"-"`
	Transcription         *transcriptor.Metadata `json:"-"`
	ExecutionPlan         *transcriptor.Plan     `json:"-"`
	SensitiveValues       []string               `json:"-"`
}

// Target identifies the matched remote system without exposing credentials.
type Target struct {
	Transport string   `json:"transport"`
	OS        string   `json:"os"`
	Hostname  string   `json:"hostname,omitempty"`
	IP        string   `json:"ip,omitempty"`
	PolicyIDs []string `json:"policy_ids,omitempty"`
}

// Task is a durable Proxyma work item.
type Task struct {
	TaskID                string                 `json:"task_id"`
	ClientOperationID     string                 `json:"client_operation_id,omitempty"`
	ClientRequestHash     string                 `json:"client_request_hash,omitempty"`
	AgentID               string                 `json:"agent_id,omitempty"`
	Target                *Target                `json:"target,omitempty"`
	Command               string                 `json:"command"`
	Reasoning             string                 `json:"reasoning"`
	RequestedBy           string                 `json:"requested_by,omitempty"`
	InteractionInstanceID string                 `json:"interaction_instance_id,omitempty"`
	Metadata              map[string]any         `json:"metadata,omitempty"`
	Transcription         *transcriptor.Metadata `json:"transcription,omitempty"`
	Status                string                 `json:"status"`
	CreatedAt             time.Time              `json:"created_at"`
	StartedAt             *time.Time             `json:"started_at,omitempty"`
	CompletedAt           *time.Time             `json:"completed_at,omitempty"`
	Result                *TaskResult            `json:"result,omitempty"`
	PendingInteraction    *interaction.Summary   `json:"pending_interaction,omitempty"`
	Approval              *Approval              `json:"approval,omitempty"`
	Policy                *PolicyDecision        `json:"policy,omitempty"`
	History               []LifecycleEvent       `json:"history"`
	ExecutionPlan         *transcriptor.Plan     `json:"-"`
	SensitiveValues       []string               `json:"-"`
}

// Approval is safe command-approval state attached to a task.
type Approval struct {
	Required      bool       `json:"required"`
	Status        string     `json:"status"`
	InteractionID string     `json:"interaction_id,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
}

// PolicyDecision records the command policy decision that governed execution.
type PolicyDecision struct {
	Decision string `json:"decision"`
	PolicyID string `json:"policy_id,omitempty"`
	RuleID   string `json:"rule_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// TaskResult is the final command outcome exposed to agents.
type TaskResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitStatus *int   `json:"exit_status"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

// LifecycleEvent records one status transition for audit-ready reconstruction.
type LifecycleEvent struct {
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
	Message string    `json:"message,omitempty"`
}

// IsFinalStatus reports whether a status is terminal.
func IsFinalStatus(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusTimedOut, StatusInterrupted, StatusAbandoned, StatusBlocked:
		return true
	default:
		return false
	}
}
