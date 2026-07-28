// Package history defines Proxyma's query-optimized interaction history boundary.
package history

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/interaction"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/tasks"
)

const (
	CommandPatternLiteral = "literal"
	CommandPatternGlob    = "glob"
	CommandPatternRegex   = "regex"

	DefaultLimit = 50
	MaxLimit     = 100
)

// QueryStore lists interaction history rows for the GUI.
type QueryStore interface {
	ListInteractions(ctx context.Context, query InteractionQuery) (InteractionPage, error)
}

// Indexer upserts query rows derived from durable task and interaction records.
type Indexer interface {
	UpsertInteraction(ctx context.Context, record IndexedInteraction) error
	DeleteAll(ctx context.Context) error
}

// InteractionQuery is the filter set supported by the user GUI history endpoint.
type InteractionQuery struct {
	CurrentUserID                    string
	CurrentUserIsAdmin               bool
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

// InteractionPage is a cursor page of interaction history rows.
type InteractionPage struct {
	Items      []InteractionHistoryItem
	NextCursor string
}

// InteractionHistoryItem is the safe GUI representation of one durable interaction.
type InteractionHistoryItem struct {
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
	TargetHostname                   string     `json:"target_hostname,omitempty"`
	TargetIP                         string     `json:"target_ip,omitempty"`
	Command                          string     `json:"command"`
	CreatedAt                        time.Time  `json:"created_at"`
	ExpiresAt                        *time.Time `json:"expires_at,omitempty"`
	RespondedAt                      *time.Time `json:"responded_at,omitempty"`
	TaskCreatedAt                    time.Time  `json:"task_created_at"`
}

// IndexedInteraction is the complete history row plus visibility facts.
type IndexedInteraction struct {
	Item           InteractionHistoryItem
	VisibleUserIDs []string
	RecordJSON     string
}

// Diagnostic describes a history row that could not be fully indexed.
type Diagnostic struct {
	InteractionID string
	TaskID        string
	Message       string
}

// Cursor is the opaque pagination position used by QueryStore implementations.
type Cursor struct {
	CreatedAt     time.Time `json:"created_at"`
	InteractionID string    `json:"interaction_id"`
}

// EncodeCursor returns an opaque cursor token for a row.
func EncodeCursor(createdAt time.Time, interactionID string) string {
	payload, err := json.Marshal(Cursor{CreatedAt: createdAt.UTC(), InteractionID: interactionID})
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
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.InteractionID) == "" {
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

// BuildIndexedInteraction joins durable interaction and task records with current
// registries. Visibility is based only on the task agent's assigned users.
func BuildIndexedInteraction(record interaction.Interaction, task tasks.Task, agent agents.Agent, agentFound bool, instanceRegistry interactioninstances.Registry) (IndexedInteraction, []Diagnostic, error) {
	body, err := json.Marshal(record)
	if err != nil {
		return IndexedInteraction{}, nil, err
	}
	metadata := instanceMetadata(record.InteractionInstanceID, instanceRegistry)
	provider := ""
	if record.Delivery != nil {
		provider = strings.TrimSpace(record.Delivery.Provider)
	}
	if provider == "" {
		provider = metadata.ProviderType
	}
	if provider == "" && interactioninstances.NormalizeInstanceID(record.InteractionInstanceID) == interaction.DefaultInstanceLocal {
		provider = interaction.DefaultInstanceLocal
	}
	var approved *bool
	answer := ""
	respondedBy := ""
	if record.Response != nil {
		approved = cloneBool(record.Response.Approved)
		answer = record.Response.Answer
		respondedBy = record.Response.RespondedBy
	}
	targetHostname := ""
	targetIP := ""
	if task.Target != nil {
		targetHostname = strings.TrimSpace(task.Target.Hostname)
		targetIP = strings.TrimSpace(task.Target.IP)
	}
	item := InteractionHistoryItem{
		InteractionID:                    record.InteractionID,
		TaskID:                           record.TaskID,
		AgentID:                          task.AgentID,
		AgentName:                        agent.DisplayName,
		InteractionInstanceID:            interactioninstances.NormalizeInstanceID(record.InteractionInstanceID),
		InteractionInstanceCreatorUserID: metadata.CreatorUserID,
		Provider:                         provider,
		Kind:                             record.Kind,
		ExpectedResponse:                 record.ExpectedResponse,
		Status:                           record.Status,
		Subject:                          record.Subject,
		Message:                          record.Message,
		RequestedBy:                      record.RequestedBy,
		Approved:                         approved,
		Answer:                           answer,
		RespondedBy:                      respondedBy,
		CancelReason:                     record.CancelReason,
		Target:                           formatTarget(targetHostname, targetIP),
		TargetHostname:                   targetHostname,
		TargetIP:                         targetIP,
		Command:                          task.Command,
		CreatedAt:                        record.CreatedAt.UTC(),
		ExpiresAt:                        cloneTime(record.ExpiresAt),
		RespondedAt:                      cloneTime(record.RespondedAt),
		TaskCreatedAt:                    task.CreatedAt.UTC(),
	}
	out := IndexedInteraction{
		Item:           item,
		VisibleUserIDs: nil,
		RecordJSON:     string(body),
	}
	var diagnostics []Diagnostic
	if agentFound {
		out.VisibleUserIDs = uniqueNonEmpty(agent.UserIDs)
	} else {
		diagnostics = append(diagnostics, Diagnostic{
			InteractionID: record.InteractionID,
			TaskID:        record.TaskID,
			Message:       fmt.Sprintf("agent %q not found; interaction has no non-admin visible users", task.AgentID),
		})
	}
	return out, diagnostics, nil
}

// Rebuild loads every joined interaction into the index after clearing it.
func Rebuild(ctx context.Context, indexer Indexer, interactions []interaction.Interaction, taskRecords []tasks.Task, agentRegistry agents.Registry, instanceRegistry interactioninstances.Registry) ([]Diagnostic, error) {
	if indexer == nil {
		return nil, fmt.Errorf("interaction history indexer is not configured")
	}
	if err := indexer.DeleteAll(ctx); err != nil {
		return nil, err
	}
	tasksByID := make(map[string]tasks.Task, len(taskRecords))
	for _, task := range taskRecords {
		tasksByID[task.TaskID] = task
	}
	var diagnostics []Diagnostic
	for _, record := range interactions {
		task, ok := tasksByID[record.TaskID]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{InteractionID: record.InteractionID, TaskID: record.TaskID, Message: "task not found"})
			continue
		}
		agent, agentFound := agentRegistry.Find(task.AgentID)
		indexed, rowDiagnostics, err := BuildIndexedInteraction(record, task, agent, agentFound, instanceRegistry)
		if err != nil {
			return diagnostics, err
		}
		diagnostics = append(diagnostics, rowDiagnostics...)
		if err := indexer.UpsertInteraction(ctx, indexed); err != nil {
			return diagnostics, err
		}
	}
	return diagnostics, nil
}

// IndexingInteractionStore decorates the durable JSON store with incremental indexing.
type IndexingInteractionStore struct {
	base             interaction.Store
	indexer          Indexer
	agentRegistry    agents.Registry
	instanceRegistry interactioninstances.Registry
	logf             func(string, ...any)
	mu               sync.RWMutex
	taskFinder       func(string) (*tasks.Task, bool)
}

// NewIndexingInteractionStore returns a store wrapper that saves to base first.
func NewIndexingInteractionStore(base interaction.Store, indexer Indexer, agentRegistry agents.Registry, instanceRegistry interactioninstances.Registry, logf func(string, ...any)) *IndexingInteractionStore {
	return &IndexingInteractionStore{
		base:             base,
		indexer:          indexer,
		agentRegistry:    agentRegistry,
		instanceRegistry: instanceRegistry,
		logf:             logf,
	}
}

// SetTaskFinder configures the task lookup used for incremental updates.
func (s *IndexingInteractionStore) SetTaskFinder(finder func(string) (*tasks.Task, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskFinder = finder
}

// LoadAll loads from the underlying lifecycle store.
func (s *IndexingInteractionStore) LoadAll() ([]interaction.Interaction, error) {
	return s.base.LoadAll()
}

// Save persists to the lifecycle store, then best-effort upserts the history index.
func (s *IndexingInteractionStore) Save(record interaction.Interaction) error {
	if err := s.base.Save(record); err != nil {
		return err
	}
	s.index(record)
	return nil
}

func (s *IndexingInteractionStore) index(record interaction.Interaction) {
	if s == nil || s.indexer == nil {
		return
	}
	s.mu.RLock()
	finder := s.taskFinder
	s.mu.RUnlock()
	if finder == nil || strings.TrimSpace(record.TaskID) == "" {
		return
	}
	task, ok := finder(record.TaskID)
	if !ok || task == nil {
		s.log("interaction history index skipped interaction_id=%s task_id=%s reason=%q", record.InteractionID, record.TaskID, "task not found")
		return
	}
	agent, agentFound := s.agentRegistry.Find(task.AgentID)
	indexed, diagnostics, err := BuildIndexedInteraction(record, *task, agent, agentFound, s.instanceRegistry)
	if err != nil {
		s.log("interaction history index failed interaction_id=%s task_id=%s error=%q", record.InteractionID, record.TaskID, err.Error())
		return
	}
	for _, diagnostic := range diagnostics {
		s.log("interaction history index diagnostic interaction_id=%s task_id=%s message=%q", diagnostic.InteractionID, diagnostic.TaskID, diagnostic.Message)
	}
	if err := s.indexer.UpsertInteraction(context.Background(), indexed); err != nil {
		s.log("interaction history index failed interaction_id=%s task_id=%s error=%q", record.InteractionID, record.TaskID, err.Error())
	}
}

func (s *IndexingInteractionStore) log(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

func instanceMetadata(id string, registry interactioninstances.Registry) interactioninstances.InstanceMetadata {
	normalized := interactioninstances.NormalizeInstanceID(id)
	for _, definition := range registry.All() {
		metadata := definition.Metadata()
		if metadata.ID == normalized {
			return metadata
		}
	}
	if normalized == interaction.DefaultInstanceLocal {
		return interactioninstances.InstanceMetadata{ID: interaction.DefaultInstanceLocal, ProviderType: interaction.DefaultInstanceLocal}
	}
	return interactioninstances.InstanceMetadata{ID: normalized}
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func formatTarget(hostname, ip string) string {
	if strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return strings.TrimSpace(ip)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := value.UTC()
	return &copied
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
