// Package history defines the query boundary used by Proxyma's History GUI.
package history

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EntryTypeCommand     = "command"
	EntryTypeInteraction = "interaction"

	CommandPatternLiteral = "literal"
	CommandPatternGlob    = "glob"
	CommandPatternRegex   = "regex"

	DefaultLimit = 50
	MaxLimit     = 100
)

// QueryStore lists unified command and interaction history rows for the GUI.
type QueryStore interface {
	ListHistory(ctx context.Context, query HistoryQuery) (HistoryPage, error)
}

// VisibilityScope describes the records a non-admin user may read. The API
// resolves this scope from the JSON-backed agent and target registries before
// calling the storage layer.
type VisibilityScope struct {
	Admin     bool
	AgentIDs  []string
	TargetIDs []string
}

// HistoryQuery is the filter set supported by the user GUI history endpoint.
type HistoryQuery struct {
	Visibility                       VisibilityScope
	EntryType                        string
	AgentID                          string
	InteractionInstanceID            string
	InteractionInstanceCreatorUserID string
	Status                           []string
	Kind                             []string
	Target                           string
	CreatedFrom                      *time.Time
	CreatedTo                        *time.Time
	Text                             string
	CommandPattern                   string
	CommandPatternMode               string
	Limit                            int
	Cursor                           string
}

// HistoryPage is a cursor page of unified history rows.
type HistoryPage struct {
	Items      []HistoryItem
	NextCursor string
}

// HistoryItem is the safe GUI representation of one durable command or interaction.
type HistoryItem struct {
	HistoryID                        string     `json:"history_id"`
	EntryType                        string     `json:"entry_type"`
	InteractionID                    string     `json:"interaction_id"`
	TaskID                           string     `json:"task_id"`
	AgentID                          string     `json:"agent_id"`
	AgentName                        string     `json:"agent_name,omitempty"`
	InteractionInstanceID            string     `json:"interaction_instance_id"`
	InteractionInstanceCreatorUserID string     `json:"interaction_instance_creator_user_id,omitempty"`
	Provider                         string     `json:"provider,omitempty"`
	Kind                             string     `json:"kind"`
	ExpectedResponse                 string     `json:"expected_response"`
	Status                           string     `json:"status"`
	Subject                          string     `json:"subject"`
	Message                          string     `json:"message"`
	RequestedBy                      string     `json:"requested_by,omitempty"`
	Approved                         *bool      `json:"approved,omitempty"`
	Answer                           string     `json:"answer,omitempty"`
	RespondedBy                      string     `json:"responded_by,omitempty"`
	CancelReason                     string     `json:"cancel_reason,omitempty"`
	Target                           string     `json:"target,omitempty"`
	TargetID                         string     `json:"-"`
	TargetHostname                   string     `json:"target_hostname,omitempty"`
	TargetIP                         string     `json:"target_ip,omitempty"`
	Command                          string     `json:"command"`
	Result                           string     `json:"result,omitempty"`
	PolicyJustification              string     `json:"policy_justification,omitempty"`
	CreatedAt                        time.Time  `json:"created_at"`
	CompletedAt                      *time.Time `json:"completed_at,omitempty"`
	ExpiresAt                        *time.Time `json:"expires_at,omitempty"`
	RespondedAt                      *time.Time `json:"responded_at,omitempty"`
	TaskCreatedAt                    time.Time  `json:"task_created_at"`
}

// InteractionHistoryItem is retained as a source-compatible alias for callers
// that use the older interaction-specific query name.
type InteractionHistoryItem = HistoryItem

// InteractionQuery is retained as a source-compatible alias for the unified query.
type InteractionQuery = HistoryQuery

// InteractionPage is retained as a source-compatible alias for the unified page.
type InteractionPage = HistoryPage

// Cursor is the opaque pagination position used by QueryStore implementations.
type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	HistoryID string    `json:"history_id"`
}

// EncodeCursor returns an opaque cursor token for a row.
func EncodeCursor(createdAt time.Time, historyID string) string {
	payload, err := json.Marshal(Cursor{CreatedAt: createdAt.UTC(), HistoryID: historyID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeCursor parses an opaque cursor token.
func DecodeCursor(value string) (Cursor, error) {
	if strings.TrimSpace(value) == "" {
		return Cursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	var cursor Cursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.HistoryID) == "" {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	return cursor, nil
}

// NormalizeLimit applies the API default and cap.
func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}
