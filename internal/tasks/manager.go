package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/remoteexec"
	"proxyma/internal/sshclient"
	"proxyma/internal/transcriptor"
	"proxyma/internal/winrmclient"
)

// Manager creates, executes, persists, and retrieves tasks.
type Manager struct {
	mu           sync.RWMutex
	store        Store
	executor     executor.Executor
	interactions interaction.Communicator
	ssh          sshclient.Client
	winrm        winrmclient.Client
	now          func() time.Time
	tasks        map[string]*Task
	operations   map[string]string
	runs         sync.WaitGroup
	debug        bool
	logger       *log.Logger
}

// ManagerOptions configures optional task manager behavior.
type ManagerOptions struct {
	Debug        bool
	Logger       *log.Logger
	SSHClient    sshclient.Client
	WinRMClient  winrmclient.Client
	Interactions interaction.Communicator
}

// NewManager creates a task manager.
func NewManager(taskStore Store, exec executor.Executor) *Manager {
	return NewManagerWithOptions(taskStore, exec, ManagerOptions{})
}

// NewManagerWithOptions creates a task manager with optional behavior.
func NewManagerWithOptions(taskStore Store, exec executor.Executor, options ManagerOptions) *Manager {
	return &Manager{
		store:        taskStore,
		executor:     exec,
		interactions: options.Interactions,
		ssh:          options.SSHClient,
		winrm:        options.WinRMClient,
		now:          time.Now,
		tasks:        make(map[string]*Task),
		operations:   make(map[string]string),
		debug:        options.Debug,
		logger:       options.Logger,
	}
}

// Recover loads persisted tasks and marks non-final tasks abandoned.
func (m *Manager) Recover() error {
	if m.store == nil {
		return fmt.Errorf("task store is not configured")
	}
	records, err := m.store.LoadAll()
	if err != nil {
		return err
	}
	for _, task := range records {
		if task.ClientOperationID != "" {
			key := operationKey(task.AgentID, task.ClientOperationID)
			if existing, ok := m.operations[key]; ok && existing != task.TaskID {
				return fmt.Errorf("duplicate persisted client operation %q for agent %q", task.ClientOperationID, task.AgentID)
			}
			m.operations[key] = task.TaskID
		}
		if task.Status == StatusWaitingUserResponse {
			copied := task
			m.tasks[task.TaskID] = &copied
			continue
		}
		if !IsFinalStatus(task.Status) {
			m.transition(&task, StatusAbandoned, "task abandoned by Proxyma restart and will not be resumed", &TaskResult{
				Stdout:    "",
				Stderr:    "",
				Truncated: false,
				Error:     "task abandoned by Proxyma restart and will not be resumed",
			})
			if err := m.store.Save(task); err != nil {
				return err
			}
		}
		copied := task
		m.tasks[task.TaskID] = &copied
	}
	return nil
}

// ErrIdempotencyConflict reports reuse of a client operation ID with a different request.
var ErrIdempotencyConflict = errors.New("client operation id was already used for a different request")

// LookupOperation returns a previously submitted task for an idempotent client operation.
func (m *Manager) LookupOperation(agentID, operationID, requestHash string) (*Task, bool, error) {
	if strings.TrimSpace(operationID) == "" {
		return nil, false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	taskID, ok := m.operations[operationKey(agentID, operationID)]
	if !ok {
		return nil, false, nil
	}
	task, ok := m.tasks[taskID]
	if !ok {
		return nil, false, fmt.Errorf("idempotency index references missing task %q", taskID)
	}
	if task.ClientRequestHash != requestHash {
		return nil, true, ErrIdempotencyConflict
	}
	return cloneTask(task), true, nil
}

// Submit creates and persists a task.
func (m *Manager) Submit(ctx context.Context, command CommandContext) (*Task, error) {
	task, _, err := m.SubmitIdempotent(ctx, command)
	return task, err
}

// SubmitIdempotent creates a task or returns the existing task for a matching operation.
func (m *Manager) SubmitIdempotent(_ context.Context, command CommandContext) (*Task, bool, error) {
	if strings.TrimSpace(command.Command) == "" {
		return nil, false, fmt.Errorf("command is required")
	}
	if command.ClientOperationID != "" && strings.TrimSpace(command.ClientRequestHash) == "" {
		return nil, false, fmt.Errorf("client request hash is required with client operation id")
	}
	id, err := newID()
	if err != nil {
		return nil, false, err
	}
	now := m.now().UTC()
	task := &Task{
		TaskID:                id,
		ClientOperationID:     command.ClientOperationID,
		ClientRequestHash:     command.ClientRequestHash,
		AgentID:               command.AgentID,
		Target:                cloneTarget(command.Target),
		Command:               command.Command,
		Reasoning:             command.Reasoning,
		RequestedBy:           command.RequestedBy,
		InteractionInstanceID: normalizeInteractionInstanceID(command.InteractionInstanceID),
		Metadata:              command.Metadata,
		Policy:                clonePolicyDecision(command.Policy),
		Transcription:         cloneMetadata(command.Transcription),
		Status:                StatusCreated,
		CreatedAt:             now,
		History:               []LifecycleEvent{{Status: StatusCreated, At: now, Message: "task created"}},
		ExecutionPlan:         clonePlan(command.ExecutionPlan),
		SensitiveValues:       append([]string(nil), command.SensitiveValues...),
	}
	if m.store == nil {
		return nil, false, fmt.Errorf("task store is not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if task.ClientOperationID != "" {
		key := operationKey(task.AgentID, task.ClientOperationID)
		if taskID, ok := m.operations[key]; ok {
			existing, exists := m.tasks[taskID]
			if !exists {
				return nil, false, fmt.Errorf("idempotency index references missing task %q", taskID)
			}
			if existing.ClientRequestHash != task.ClientRequestHash {
				return nil, false, ErrIdempotencyConflict
			}
			return cloneTask(existing), true, nil
		}
		if err := m.store.Save(*cloneTask(task)); err != nil {
			return nil, false, err
		}
		m.tasks[task.TaskID] = cloneTask(task)
		m.operations[key] = task.TaskID
		return cloneTask(task), false, nil
	}
	if err := m.store.Save(*cloneTask(task)); err != nil {
		return nil, false, err
	}
	m.tasks[task.TaskID] = cloneTask(task)
	return cloneTask(task), false, nil
}

func operationKey(agentID, operationID string) string {
	return strings.TrimSpace(agentID) + "\x00" + strings.TrimSpace(operationID)
}

func normalizeInteractionInstanceID(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return interaction.DefaultInstanceLocal
	}
	return trimmed
}

// SetPolicyDecision persists the policy decision that governs a task.
func (m *Manager) SetPolicyDecision(taskID string, decision PolicyDecision) (*Task, error) {
	task, ok := m.getMutable(taskID)
	if !ok {
		return nil, fmt.Errorf("task not found")
	}
	if IsFinalStatus(task.Status) {
		return cloneTask(task), nil
	}
	task.Policy = clonePolicyDecision(&decision)
	if err := m.save(task); err != nil {
		return nil, err
	}
	return cloneTask(task), nil
}

// Get returns a task by id.
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	return cloneTask(task), true
}

// List returns cloned task records.
func (m *Manager) List() []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		items = append(items, cloneTask(task))
	}
	return items
}

// ListByStatus returns cloned tasks with the requested status.
func (m *Manager) ListByStatus(status string) []*Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tasks []*Task
	for _, task := range m.tasks {
		if task.Status == status {
			tasks = append(tasks, cloneTask(task))
		}
	}
	return tasks
}

// WaitForInteraction moves a task to the human-input waiting state.
func (m *Manager) WaitForInteraction(taskID string, pending interaction.Summary) (*Task, error) {
	task, ok := m.getMutable(taskID)
	if !ok {
		return nil, fmt.Errorf("task not found")
	}
	if IsFinalStatus(task.Status) {
		return nil, fmt.Errorf("task is %s", task.Status)
	}
	now := m.now().UTC()
	task.Status = StatusWaitingUserResponse
	task.PendingInteraction = cloneInteractionSummary(&pending)
	if pending.Kind == interaction.KindApprovalRequest {
		task.Approval = &Approval{
			Required:      true,
			Status:        ApprovalStatusPending,
			InteractionID: pending.InteractionID,
		}
	}
	task.History = append(task.History, LifecycleEvent{Status: StatusWaitingUserResponse, At: now, Message: "waiting for user response"})
	if err := m.save(task); err != nil {
		return nil, err
	}
	m.logf("task state changed task_id=%s from=%s to=%s interaction_id=%s interaction_kind=%s",
		task.TaskID, StatusCreated, StatusWaitingUserResponse, pending.InteractionID, pending.Kind)
	return cloneTask(task), nil
}

// ApplyInteraction updates the waiting task linked to an answered, expired, or canceled interaction.
func (m *Manager) ApplyInteraction(result interaction.Interaction) (*Task, bool, error) {
	if strings.TrimSpace(result.TaskID) == "" {
		return nil, false, nil
	}
	task, ok := m.getMutable(result.TaskID)
	if !ok {
		return nil, false, fmt.Errorf("task not found")
	}
	if task.PendingInteraction == nil || task.PendingInteraction.InteractionID != result.InteractionID {
		return nil, false, fmt.Errorf("task is not waiting for interaction %s", result.InteractionID)
	}
	if task.Status != StatusWaitingUserResponse {
		return nil, false, fmt.Errorf("task is %s", task.Status)
	}
	switch result.Status {
	case interaction.StatusAnswered:
		if result.ExpectedResponse == interaction.ExpectedApproval && result.Response != nil && result.Response.Approved != nil && !*result.Response.Approved {
			m.setApprovalDecision(task, ApprovalStatusRejected, result.InteractionID)
			m.logf("task approval decision task_id=%s interaction_id=%s approval_status=%s", task.TaskID, result.InteractionID, ApprovalStatusRejected)
			m.block(task, "user rejected interaction", "user rejected interaction")
		} else {
			from := task.Status
			now := m.now().UTC()
			nextStatus := StatusCreated
			message := "user response received"
			if result.ExpectedResponse == interaction.ExpectedApproval {
				nextStatus = StatusAcceptedByUser
				message = "user approved interaction"
			}
			task.Status = nextStatus
			task.PendingInteraction = nil
			if result.ExpectedResponse == interaction.ExpectedApproval {
				m.setApprovalDecision(task, ApprovalStatusApproved, result.InteractionID)
				m.logf("task approval decision task_id=%s interaction_id=%s approval_status=%s", task.TaskID, result.InteractionID, ApprovalStatusApproved)
			}
			task.History = append(task.History, LifecycleEvent{Status: nextStatus, At: now, Message: message})
			m.logf("task state changed task_id=%s from=%s to=%s reason=%q", task.TaskID, from, nextStatus, message)
		}
	case interaction.StatusExpired:
		m.setApprovalDecision(task, ApprovalStatusExpired, result.InteractionID)
		m.logf("task approval decision task_id=%s interaction_id=%s approval_status=%s", task.TaskID, result.InteractionID, ApprovalStatusExpired)
		m.block(task, "user response expired", "user response expired")
	case interaction.StatusCanceled:
		m.setApprovalDecision(task, ApprovalStatusCanceled, result.InteractionID)
		m.logf("task approval decision task_id=%s interaction_id=%s approval_status=%s", task.TaskID, result.InteractionID, ApprovalStatusCanceled)
		message := "user response canceled"
		if strings.TrimSpace(result.CancelReason) != "" {
			message = result.CancelReason
		}
		m.block(task, message, message)
	default:
		return nil, false, nil
	}
	if err := m.save(task); err != nil {
		return nil, true, err
	}
	return cloneTask(task), true, nil
}

// StartTask starts one created task.
func (m *Manager) StartTask(ctx context.Context, id string) (bool, error) {
	return m.startCreated(context.WithoutCancel(ctx), id)
}

// Wait waits for all background task executions to finish.
//
// It is intended for orderly shutdown and tests; task execution itself is
// still started asynchronously by StartTask.
func (m *Manager) Wait() {
	m.runs.Wait()
}

// BlockTask marks one non-final task blocked.
func (m *Manager) BlockTask(id string, message string) (*Task, error) {
	task, ok := m.getMutable(id)
	if !ok {
		return nil, fmt.Errorf("task not found")
	}
	if IsFinalStatus(task.Status) {
		return cloneTask(task), nil
	}
	m.block(task, message, message)
	if err := m.save(task); err != nil {
		return nil, err
	}
	return cloneTask(task), nil
}

// StartCreated starts all currently runnable created or user-accepted tasks and returns the number started.
func (m *Manager) StartCreated(ctx context.Context) (int, error) {
	ids := m.idsByStatuses(StatusCreated, StatusAcceptedByUser)
	started := 0
	for _, id := range ids {
		ok, err := m.startCreated(context.WithoutCancel(ctx), id)
		if err != nil {
			return started, err
		}
		if ok {
			started++
		}
	}
	return started, nil
}

// ManageState applies pending lifecycle state changes and reports running tasks.
func (m *Manager) ManageState(ctx context.Context) (int, error) {
	if m.interactions != nil {
		expired, err := m.interactions.ExpirePending(ctx, m.now().UTC())
		if err != nil {
			return 0, err
		}
		for _, expiredInteraction := range expired {
			if _, _, err := m.ApplyInteraction(expiredInteraction); err != nil {
				return 0, err
			}
		}
	}
	return len(m.idsByStatus(StatusRunning)), nil
}

func (m *Manager) idsByStatus(status string) []string {
	return m.idsByStatuses(status)
}

func (m *Manager) idsByStatuses(statuses ...string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wanted := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		wanted[status] = true
	}
	var ids []string
	for id, task := range m.tasks {
		if wanted[task.Status] {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *Manager) startCreated(ctx context.Context, id string) (bool, error) {
	task, ok := m.getMutable(id)
	if !ok || (task.Status != StatusCreated && task.Status != StatusAcceptedByUser) {
		return false, nil
	}
	from := task.Status
	started := m.now().UTC()
	task.StartedAt = &started
	task.Status = StatusRunning
	task.History = append(task.History, LifecycleEvent{Status: StatusRunning, At: started, Message: "command running"})
	if err := m.save(task); err != nil {
		return false, err
	}
	m.logf("task state changed task_id=%s from=%s to=%s reason=%q", task.TaskID, from, StatusRunning, "command running")
	m.runs.Add(1)
	go func() {
		defer m.runs.Done()
		m.run(ctx, id)
	}()
	return true, nil
}

func (m *Manager) run(ctx context.Context, id string) {
	task, ok := m.getMutable(id)
	if !ok {
		return
	}
	m.debugf("debug command start task_id=%s target=%s command_family=%s strategy=%s command=%q",
		task.TaskID, formatTarget(task.Target), commandFamily(task), strategyName(task), task.Command)

	runStarted := m.now().UTC()
	result := m.runCommand(ctx, task)
	duration := m.now().UTC().Sub(runStarted)
	taskResult := &TaskResult{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitStatus: result.ExitStatus,
		Truncated:  result.Truncated,
		Error:      RedactCredentialValues(result.Error, task.SensitiveValues...),
	}
	status := StatusCompleted
	message := "command completed"
	if result.TimedOut {
		status = StatusTimedOut
		message = "command timed out"
	} else if result.Error != "" {
		status = StatusFailed
		message = "command failed"
	}
	exitStatus := "nil"
	if result.ExitStatus != nil {
		exitStatus = fmt.Sprintf("%d", *result.ExitStatus)
	}
	safeError := RedactCredentialValues(result.Error, task.SensitiveValues...)
	m.debugf("debug command finish task_id=%s target=%s command_family=%s strategy=%s status=%s exit_status=%s duration=%s stdout_bytes=%d stderr_bytes=%d truncated=%t timed_out=%t error=%q",
		task.TaskID, formatTarget(task.Target), commandFamily(task), strategyName(task), status, exitStatus, duration, len(result.Stdout), len(result.Stderr), result.Truncated, result.TimedOut, safeError)
	m.debugf("debug command result task_id=%s target=%s command_family=%s strategy=%s stdout=%q stderr=%q",
		task.TaskID, formatTarget(task.Target), commandFamily(task), strategyName(task), result.Stdout, result.Stderr)
	m.transition(task, status, message, taskResult)
	_ = m.save(task)
	m.logf("task state changed task_id=%s from=%s to=%s reason=%q", task.TaskID, StatusRunning, status, message)
}

func (m *Manager) runCommand(ctx context.Context, task *Task) remoteexec.Result {
	if task.ExecutionPlan != nil {
		switch task.ExecutionPlan.Kind {
		case transcriptor.KindSSHRemoteCommand:
			if m.ssh == nil || task.ExecutionPlan.SSHRequest == nil {
				return remoteexec.Result{Error: "ssh execution plan is not runnable because no SSH client is configured"}
			}
			return m.ssh.Run(ctx, *task.ExecutionPlan.SSHRequest)
		case transcriptor.KindWinRMRemoteCommand:
			if m.winrm == nil || task.ExecutionPlan.WinRMRequest == nil {
				return remoteexec.Result{Error: "winrm execution plan is not runnable because no WinRM client is configured"}
			}
			return m.winrm.Run(ctx, *task.ExecutionPlan.WinRMRequest)
		default:
			return remoteexec.Result{Error: "execution plan is not runnable because its transport is unsupported"}
		}
	}

	result := m.executor.Run(ctx, task.Command)
	return remoteexec.Result{
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitStatus: result.ExitStatus,
		Truncated:  result.Truncated,
		Error:      result.Error,
		TimedOut:   result.TimedOut,
	}
}

func (m *Manager) debugf(format string, args ...any) {
	if !m.debug {
		return
	}
	logger := m.logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
}

func (m *Manager) logf(format string, args ...any) {
	logger := m.logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
}

func (m *Manager) getMutable(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	return task, ok
}

func (m *Manager) transition(task *Task, status, message string, result *TaskResult) {
	now := m.now().UTC()
	task.Status = status
	task.CompletedAt = &now
	task.Result = result
	task.History = append(task.History, LifecycleEvent{Status: status, At: now, Message: message})
}

func (m *Manager) block(task *Task, message, errorMessage string) {
	from := task.Status
	m.transition(task, StatusBlocked, message, &TaskResult{
		Stdout:    "",
		Stderr:    "",
		Truncated: false,
		Error:     errorMessage,
	})
	task.PendingInteraction = nil
	m.logf("task state changed task_id=%s from=%s to=%s reason=%q", task.TaskID, from, StatusBlocked, message)
}

func (m *Manager) setApprovalDecision(task *Task, status string, interactionID string) {
	now := m.now().UTC()
	if task.Approval == nil {
		task.Approval = &Approval{Required: true}
	}
	task.Approval.Status = status
	task.Approval.InteractionID = interactionID
	task.Approval.DecidedAt = &now
}

func (m *Manager) save(task *Task) error {
	if m.store == nil {
		return fmt.Errorf("task store is not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := cloneTask(task)
	if err := m.store.Save(*copied); err != nil {
		return err
	}
	m.tasks[task.TaskID] = copied
	return nil
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	copied := *task
	copied.Target = cloneTarget(task.Target)
	copied.History = append([]LifecycleEvent(nil), task.History...)
	if task.Result != nil {
		result := *task.Result
		copied.Result = &result
	}
	copied.PendingInteraction = cloneInteractionSummary(task.PendingInteraction)
	copied.Approval = cloneApproval(task.Approval)
	copied.Policy = clonePolicyDecision(task.Policy)
	if task.Metadata != nil {
		copied.Metadata = make(map[string]any, len(task.Metadata))
		for key, value := range task.Metadata {
			copied.Metadata[key] = value
		}
	}
	copied.Transcription = cloneMetadata(task.Transcription)
	copied.ExecutionPlan = clonePlan(task.ExecutionPlan)
	copied.SensitiveValues = append([]string(nil), task.SensitiveValues...)
	return &copied
}

func cloneApproval(approval *Approval) *Approval {
	if approval == nil {
		return nil
	}
	copied := *approval
	if approval.DecidedAt != nil {
		decidedAt := *approval.DecidedAt
		copied.DecidedAt = &decidedAt
	}
	return &copied
}

func cloneInteractionSummary(summary *interaction.Summary) *interaction.Summary {
	if summary == nil {
		return nil
	}
	copied := *summary
	if summary.ExpiresAt != nil {
		expiresAt := *summary.ExpiresAt
		copied.ExpiresAt = &expiresAt
	}
	return &copied
}

func cloneTarget(target *Target) *Target {
	if target == nil {
		return nil
	}
	copied := *target
	copied.PolicyIDs = append([]string(nil), target.PolicyIDs...)
	return &copied
}

func clonePolicyDecision(decision *PolicyDecision) *PolicyDecision {
	if decision == nil {
		return nil
	}
	copied := *decision
	return &copied
}

func cloneMetadata(metadata *transcriptor.Metadata) *transcriptor.Metadata {
	if metadata == nil {
		return nil
	}
	copied := *metadata
	return &copied
}

func clonePlan(plan *transcriptor.Plan) *transcriptor.Plan {
	if plan == nil {
		return nil
	}
	copied := *plan
	copied.SensitiveValues = append([]string(nil), plan.SensitiveValues...)
	if plan.SSHRequest != nil {
		request := *plan.SSHRequest
		copied.SSHRequest = &request
	}
	if plan.WinRMRequest != nil {
		request := *plan.WinRMRequest
		request.SensitiveValues = append([]string(nil), plan.WinRMRequest.SensitiveValues...)
		copied.WinRMRequest = &request
	}
	return &copied
}

func commandFamily(task *Task) string {
	if task.Transcription == nil {
		return ""
	}
	return task.Transcription.CommandFamily
}

func strategyName(task *Task) string {
	if task.Transcription == nil {
		return ""
	}
	return task.Transcription.Strategy
}

func formatTarget(target *Target) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.Hostname) != "" {
		return target.Hostname
	}
	return target.IP
}

func newID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
