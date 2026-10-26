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
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
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

// Indexer upserts query rows derived from durable task and interaction records.
type Indexer interface {
	UpsertCommand(ctx context.Context, record IndexedCommand) error
	UpsertInteraction(ctx context.Context, record IndexedInteraction) error
	DeleteAll(ctx context.Context) error
}

// VisibilitySynchronizer updates materialized history access without rebuilding
// the durable JSON stores.
type VisibilitySynchronizer interface {
	SyncAgentVisibility(ctx context.Context, agentID string, userIDs []string) error
	SyncTargetVisibility(ctx context.Context, targetID string, userIDs []string) error
}

// VisibilitySource identifies the ownership relationship that grants access to
// one history row.
type VisibilitySource struct {
	Type    string
	ID      string
	UserIDs []string
}

// HistoryQuery is the filter set supported by the user GUI history endpoint.
type HistoryQuery struct {
	CurrentUserID                    string
	CurrentUserIsAdmin               bool
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

// InteractionHistoryItem is retained as a source-compatible alias for domain tests
// and callers while the query model is unified.
type InteractionHistoryItem = HistoryItem

// InteractionQuery is retained as a source-compatible alias for the unified query.
type InteractionQuery = HistoryQuery

// InteractionPage is retained as a source-compatible alias for the unified page.
type InteractionPage = HistoryPage

// IndexedCommand is the complete command history row plus visibility facts.
type IndexedCommand struct {
	Item           HistoryItem
	VisibleUserIDs []string
	AgentUserIDs   []string
	TargetUserIDs  []string
}

// IndexedInteraction is the complete history row plus visibility facts.
type IndexedInteraction struct {
	Item           HistoryItem
	VisibleUserIDs []string
	AgentUserIDs   []string
	RecordJSON     string
}

func (r IndexedCommand) VisibilitySources() []VisibilitySource {
	if len(r.AgentUserIDs) == 0 && len(r.TargetUserIDs) == 0 {
		return []VisibilitySource{{Type: "agent", ID: r.Item.AgentID, UserIDs: uniqueNonEmpty(r.VisibleUserIDs)}}
	}
	sources := []VisibilitySource{{Type: "agent", ID: r.Item.AgentID, UserIDs: uniqueNonEmpty(r.AgentUserIDs)}}
	if strings.TrimSpace(r.Item.TargetID) != "" {
		sources = append(sources, VisibilitySource{Type: "target", ID: r.Item.TargetID, UserIDs: uniqueNonEmpty(r.TargetUserIDs)})
	}
	return sources
}

func (r IndexedInteraction) VisibilitySources() []VisibilitySource {
	users := r.AgentUserIDs
	if len(users) == 0 {
		users = r.VisibleUserIDs
	}
	return []VisibilitySource{{Type: "agent", ID: r.Item.AgentID, UserIDs: uniqueNonEmpty(users)}}
}

// Diagnostic describes a history row that could not be fully indexed.
type Diagnostic struct {
	InteractionID string
	TaskID        string
	Message       string
}

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

// BuildIndexedInteraction joins durable interaction and task records with current
// registries. Visibility is based only on the task agent's assigned users.
func BuildIndexedInteraction(record interaction.Interaction, task tasks.Task, agent agents.Agent, agentFound bool, targetRegistry targets.Registry, instanceRegistry interactioninstances.Registry) (IndexedInteraction, []Diagnostic, error) {
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
	targetID := matchingTargetID(task.Target, targetRegistry)
	item := HistoryItem{
		HistoryID:                        interactionHistoryID(record.InteractionID),
		EntryType:                        EntryTypeInteraction,
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
		TargetID:                         targetID,
		TargetHostname:                   targetHostname,
		TargetIP:                         targetIP,
		Command:                          task.Command,
		PolicyJustification:              policyJustification(task.Policy),
		CreatedAt:                        record.CreatedAt.UTC(),
		ExpiresAt:                        cloneTime(record.ExpiresAt),
		RespondedAt:                      cloneTime(record.RespondedAt),
		TaskCreatedAt:                    task.CreatedAt.UTC(),
	}
	out := IndexedInteraction{
		Item:           item,
		VisibleUserIDs: nil,
		AgentUserIDs:   nil,
		RecordJSON:     string(body),
	}
	var diagnostics []Diagnostic
	if agentFound {
		out.AgentUserIDs = uniqueNonEmpty(agent.UserIDs)
		out.VisibleUserIDs = append([]string(nil), out.AgentUserIDs...)
	} else {
		diagnostics = append(diagnostics, Diagnostic{
			InteractionID: record.InteractionID,
			TaskID:        record.TaskID,
			Message:       fmt.Sprintf("agent %q not found; interaction has no non-admin visible users", task.AgentID),
		})
	}
	return out, diagnostics, nil
}

// BuildIndexedCommand creates the safe SQLite projection for one durable task.
// Command visibility matches the legacy command-history behavior: users assigned
// to the agent or owning the matched target can see the row.
func BuildIndexedCommand(task tasks.Task, agent agents.Agent, targetRegistry targets.Registry) IndexedCommand {
	targetHostname := ""
	targetIP := ""
	if task.Target != nil {
		targetHostname = strings.TrimSpace(task.Target.Hostname)
		targetIP = strings.TrimSpace(task.Target.IP)
	}
	createdAt := task.CreatedAt.UTC()
	if task.CompletedAt != nil {
		createdAt = task.CompletedAt.UTC()
	}
	targetID := matchingTargetID(task.Target, targetRegistry)
	agentUserIDs := uniqueNonEmpty(agent.UserIDs)
	targetUserIDs := []string(nil)
	for _, target := range targetRegistry.All() {
		if sameTaskTarget(task.Target, target) {
			targetUserIDs = uniqueNonEmpty(target.UserIDs)
			break
		}
	}
	visible := uniqueNonEmpty(append(append([]string(nil), agentUserIDs...), targetUserIDs...))
	return IndexedCommand{
		Item: HistoryItem{
			HistoryID:           commandHistoryID(task.TaskID),
			EntryType:           EntryTypeCommand,
			TaskID:              task.TaskID,
			AgentID:             task.AgentID,
			AgentName:           agent.DisplayName,
			Target:              formatTarget(targetHostname, targetIP),
			TargetID:            targetID,
			TargetHostname:      targetHostname,
			TargetIP:            targetIP,
			Command:             task.Command,
			Status:              task.Status,
			Result:              commandResult(task),
			PolicyJustification: policyJustification(task.Policy),
			CreatedAt:           createdAt,
			CompletedAt:         cloneTime(task.CompletedAt),
			TaskCreatedAt:       task.CreatedAt.UTC(),
		},
		VisibleUserIDs: visible,
		AgentUserIDs:   agentUserIDs,
		TargetUserIDs:  targetUserIDs,
	}
}

func policyJustification(decision *tasks.PolicyDecision) string {
	if decision == nil {
		return ""
	}
	return decision.Justification
}

// Rebuild loads every command and joined interaction into the index after clearing it.
func Rebuild(ctx context.Context, indexer Indexer, interactions []interaction.Interaction, taskRecords []tasks.Task, agentRegistry agents.Registry, targetRegistry targets.Registry, instanceRegistry interactioninstances.Registry) ([]Diagnostic, error) {
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
	for _, task := range taskRecords {
		agent, agentFound := agentRegistry.Find(task.AgentID)
		if !agentFound {
			diagnostics = append(diagnostics, Diagnostic{TaskID: task.TaskID, Message: fmt.Sprintf("agent %q not found; command has no non-admin visible users", task.AgentID)})
		}
		if err := indexer.UpsertCommand(ctx, BuildIndexedCommand(task, agent, targetRegistry)); err != nil {
			return diagnostics, err
		}
	}
	for _, record := range interactions {
		task, ok := tasksByID[record.TaskID]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{InteractionID: record.InteractionID, TaskID: record.TaskID, Message: "task not found"})
			continue
		}
		agent, agentFound := agentRegistry.Find(task.AgentID)
		indexed, rowDiagnostics, err := BuildIndexedInteraction(record, task, agent, agentFound, targetRegistry, instanceRegistry)
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

// IndexingTaskStore decorates the durable JSON task store with incremental indexing.
type IndexingTaskStore struct {
	base           tasks.Store
	indexer        Indexer
	agentRegistry  agents.Registry
	targetRegistry targets.Registry
	logf           func(string, ...any)
	mu             sync.RWMutex
}

// NewIndexingTaskStore returns a task store that saves JSON before indexing SQLite.
func NewIndexingTaskStore(base tasks.Store, indexer Indexer, agentRegistry agents.Registry, targetRegistry targets.Registry, logf func(string, ...any)) *IndexingTaskStore {
	return &IndexingTaskStore{base: base, indexer: indexer, agentRegistry: agentRegistry, targetRegistry: targetRegistry, logf: logf}
}

func (s *IndexingTaskStore) LoadAll() ([]tasks.Task, error) { return s.base.LoadAll() }

// SetRegistries updates the registries used for future task index writes.
func (s *IndexingTaskStore) SetRegistries(agentRegistry agents.Registry, targetRegistry targets.Registry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentRegistry = agentRegistry
	s.targetRegistry = targetRegistry
}

func (s *IndexingTaskStore) Save(task tasks.Task) error {
	if err := s.base.Save(task); err != nil {
		return err
	}
	if s == nil || s.indexer == nil {
		return nil
	}
	s.mu.RLock()
	agentRegistry := s.agentRegistry
	targetRegistry := s.targetRegistry
	s.mu.RUnlock()
	agent, _ := agentRegistry.Find(task.AgentID)
	if err := s.indexer.UpsertCommand(context.Background(), BuildIndexedCommand(task, agent, targetRegistry)); err != nil && s.logf != nil {
		s.logf("command history index failed task_id=%s error=%q", task.TaskID, err.Error())
	}
	return nil
}

// IndexingInteractionStore decorates the durable JSON store with incremental indexing.
type IndexingInteractionStore struct {
	base             interaction.Store
	indexer          Indexer
	agentRegistry    agents.Registry
	instanceRegistry interactioninstances.Registry
	targetRegistry   targets.Registry
	logf             func(string, ...any)
	mu               sync.RWMutex
	taskFinder       func(string) (*tasks.Task, bool)
}

// NewIndexingInteractionStore returns a store wrapper that saves to base first.
func NewIndexingInteractionStore(base interaction.Store, indexer Indexer, agentRegistry agents.Registry, targetRegistry targets.Registry, instanceRegistry interactioninstances.Registry, logf func(string, ...any)) *IndexingInteractionStore {
	return &IndexingInteractionStore{
		base:             base,
		indexer:          indexer,
		agentRegistry:    agentRegistry,
		targetRegistry:   targetRegistry,
		instanceRegistry: instanceRegistry,
		logf:             logf,
	}
}

// SetRegistries updates the registries used for future interaction index writes.
func (s *IndexingInteractionStore) SetRegistries(agentRegistry agents.Registry, targetRegistry targets.Registry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentRegistry = agentRegistry
	s.targetRegistry = targetRegistry
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
	agentRegistry := s.agentRegistry
	targetRegistry := s.targetRegistry
	s.mu.RUnlock()
	if finder == nil || strings.TrimSpace(record.TaskID) == "" {
		return
	}
	task, ok := finder(record.TaskID)
	if !ok || task == nil {
		s.log("interaction history index skipped interaction_id=%s task_id=%s reason=%q", record.InteractionID, record.TaskID, "task not found")
		return
	}
	agent, agentFound := agentRegistry.Find(task.AgentID)
	indexed, diagnostics, err := BuildIndexedInteraction(record, *task, agent, agentFound, targetRegistry, s.instanceRegistry)
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

func commandHistoryID(taskID string) string {
	return EntryTypeCommand + ":" + strings.TrimSpace(taskID)
}

func interactionHistoryID(interactionID string) string {
	return EntryTypeInteraction + ":" + strings.TrimSpace(interactionID)
}

func commandResult(task tasks.Task) string {
	switch task.Status {
	case tasks.StatusCompleted:
		if task.Result != nil && task.Result.ExitStatus != nil && *task.Result.ExitStatus != 0 {
			return "warning"
		}
		return "success"
	case tasks.StatusFailed, tasks.StatusTimedOut, tasks.StatusInterrupted, tasks.StatusAbandoned, tasks.StatusBlocked:
		return "failed"
	case tasks.StatusWaitingUserResponse:
		return "waiting"
	default:
		return task.Status
	}
}

func sameTaskTarget(taskTarget *tasks.Target, target targets.Target) bool {
	if taskTarget == nil {
		return false
	}
	if strings.TrimSpace(taskTarget.Hostname) != "" && strings.EqualFold(strings.TrimSpace(taskTarget.Hostname), strings.TrimSpace(target.Hostname)) {
		return true
	}
	return strings.TrimSpace(taskTarget.IP) != "" && strings.EqualFold(strings.TrimSpace(taskTarget.IP), strings.TrimSpace(target.IP))
}

func matchingTargetID(taskTarget *tasks.Target, registry targets.Registry) string {
	for _, target := range registry.All() {
		if sameTaskTarget(taskTarget, target) {
			return strings.TrimSpace(target.ID)
		}
	}
	return ""
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
