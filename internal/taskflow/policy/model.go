// Package policy evaluates target-owned Proxyma command execution policy.
package policy

import (
	"context"
	"strings"
)

const (
	DecisionAllow    = "allow"
	DecisionApproval = "approval"
	DecisionDeny     = "deny"
)

// Diagnostic describes a rejected policy file or invalid policy reference.
type Diagnostic struct {
	Path    string
	Message string
}

// Evaluator decides whether a prepared command can run, is denied, or needs approval.
type Evaluator interface {
	Evaluate(ctx context.Context, request Request) (Result, error)
}

// Request contains safe command context used by policy rules.
type Request struct {
	TaskID        string
	RequestedBy   string
	Target        Target
	Command       string
	RemoteCommand string
	PolicyID      string
	Metadata      map[string]any
}

// Target identifies the matched Proxyma target for policy evaluation.
type Target struct {
	Hostname string
	IP       string
	PolicyID string
}

// Result is the durable policy decision attached to a task.
type Result struct {
	Decision string `json:"decision"`
	PolicyID string `json:"policy_id,omitempty"`
	RuleID   string `json:"rule_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// IsFinal reports whether the result is one of Proxyma's runnable policy decisions.
func (r Result) IsFinal() bool {
	switch r.Decision {
	case DecisionAllow, DecisionApproval, DecisionDeny:
		return true
	default:
		return false
	}
}

// NoopEvaluator preserves the previous global approval switch when no policy set is configured.
type NoopEvaluator struct {
	RequireApproval bool
}

// Evaluate maps the compatibility approval flag to an Proxyma policy result.
func (e NoopEvaluator) Evaluate(_ context.Context, _ Request) (Result, error) {
	if e.RequireApproval {
		return Result{Decision: DecisionApproval, Reason: "command approval is required"}, nil
	}
	return Result{Decision: DecisionAllow, Reason: "command approval is disabled"}, nil
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}
