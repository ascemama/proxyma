package clientcli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	cfg    Config
	client *Client
	state  *StateStore
	now    func() time.Time
}

func NewService(cfg Config) *Service {
	return &Service{cfg: cfg, client: NewClient(cfg), state: NewStateStore(cfg.StateDir), now: time.Now}
}

func (s *Service) WithStateLock(fn func() (Envelope, error)) (Envelope, error) {
	lock, err := s.state.Lock(s.cfg.LockTimeout())
	if err != nil {
		return Envelope{}, normalizeStateError(err)
	}
	defer lock.Close()
	if err := s.state.Recover(); err != nil {
		return Envelope{}, normalizeStateError(err)
	}
	envelope, err := fn()
	return envelope, normalizeStateError(err)
}

func normalizeStateError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*commandError); ok {
		return err
	}
	return &commandError{code: "state_io", message: err.Error(), retryable: true, exitCode: 3}
}

func (s *Service) Submit(ctx context.Context, input SubmitInput) (Envelope, error) {
	if err := validateSubmitInput(input); err != nil {
		return Envelope{}, err
	}
	if err := s.client.Health(ctx); err != nil {
		return Envelope{}, err
	}
	resolved, err := s.client.Resolve(ctx, input.Target)
	if err != nil {
		return Envelope{}, err
	}
	now := s.now().UTC()
	var task TaskState
	if input.ClientTaskID == "" {
		task = TaskState{
			SchemaVersion: StateVersion,
			ClientTaskID:  uuid.NewString(),
			Revision:      0,
			UserGoal:      strings.TrimSpace(input.UserGoal),
			Status:        "pending",
			CreatedAt:     now,
			UpdatedAt:     now,
			RemainingWork: strings.TrimSpace(input.UserGoal),
			Operations:    []Operation{},
		}
	} else {
		loaded, loadErr := s.state.Load(input.ClientTaskID)
		if loadErr != nil {
			if errors.Is(loadErr, os.ErrNotExist) {
				return Envelope{}, cliError("state_not_found", "client task was not found", 3)
			}
			return Envelope{}, loadErr
		}
		task = *loaded
		if input.ExpectedRevision == nil || *input.ExpectedRevision != task.Revision {
			return Envelope{}, cliError("state_conflict", fmt.Sprintf("expected revision %d", task.Revision), 3)
		}
		if hasInFlight(task) {
			return Envelope{}, cliError("operation_in_flight", "task already has a Proxyma operation in flight", 3)
		}
		if task.Status == "completed" || task.Status == "failed" {
			return Envelope{}, cliError("invalid_task_state", "cannot submit work for a final task", 3)
		}
	}
	operationID := uuid.NewString()
	request := commandRequest{
		ClientOperationID: operationID,
		Target:            input.Target,
		Command:           resolved.CanonicalCommand.Prefix + input.Payload,
		Reasoning:         input.Reasoning,
		HarnessName:       input.HarnessName,
		Metadata:          input.Metadata,
	}
	requestHash, err := hashCommandRequest(request)
	if err != nil {
		return Envelope{}, err
	}
	task.Operations = append(task.Operations, Operation{
		OperationID:      operationID,
		RequestHash:      requestHash,
		Phase:            "prepared",
		Target:           input.Target,
		Payload:          input.Payload,
		PayloadKind:      resolved.CanonicalCommand.PayloadKind,
		CanonicalCommand: request.Command,
		Reasoning:        input.Reasoning,
		HarnessName:      input.HarnessName,
		Metadata:         input.Metadata,
		PreparedAt:       now,
	})
	task.Status = "waiting_proxyma"
	task.BlockedReason = ""
	task.RequiredNextAction = ""
	touch(&task, now)
	if err := s.state.Save(task); err != nil {
		return Envelope{}, err
	}
	return s.submitOperation(ctx, &task, len(task.Operations)-1)
}

func (s *Service) submitOperation(ctx context.Context, task *TaskState, index int) (Envelope, error) {
	operation := &task.Operations[index]
	now := s.now().UTC()
	operation.Phase = "submitting"
	operation.LastError = nil
	touch(task, now)
	if err := s.state.Save(*task); err != nil {
		return Envelope{}, err
	}
	response, err := s.client.Submit(ctx, commandRequest{
		ClientOperationID: operation.OperationID,
		Target:            operation.Target,
		Command:           operation.CanonicalCommand,
		Reasoning:         operation.Reasoning,
		HarnessName:       operation.HarnessName,
		Metadata:          operation.Metadata,
	})
	if err != nil {
		operation.Phase = "submission_unknown"
		operation.LastError = safeErrorFrom(err, now)
		if typed, ok := err.(*commandError); ok && !typed.retryable {
			operation.Phase = "blocked"
			task.Status = "blocked"
			task.BlockedReason = "Proxyma rejected command submission: " + typed.message
			task.RequiredNextAction = "inspect the error and correct configuration, authorization, target, or payload before submitting new work"
		}
		touch(task, now)
		if saveErr := s.state.Save(*task); saveErr != nil {
			return Envelope{}, saveErr
		}
		return Envelope{}, attachTask(err, task)
	}
	operation.Phase = "submitted"
	operation.ProxymaTaskID = response.TaskID
	operation.ProxymaStatus = response.Status
	operation.SubmittedAt = &now
	operation.LastError = nil
	task.Status = "waiting_proxyma"
	touch(task, now)
	if err := s.state.Save(*task); err != nil {
		return Envelope{}, err
	}
	return successEnvelope("submit", "wait", task), nil
}

func (s *Service) Resume(ctx context.Context) (Envelope, error) {
	tasks, err := s.state.ListActive()
	if err != nil {
		return Envelope{}, err
	}
	blocked, err := s.state.ListBlocked()
	if err != nil {
		return Envelope{}, err
	}
	response := Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: "resume", Action: "none", Counts: &Counts{}}
	capabilityChecked := false
	for i := range tasks {
		before := cloneTaskState(tasks[i])
		response.Counts.Checked++
		action, taskErr := s.advanceTask(ctx, &tasks[i], &capabilityChecked)
		if taskErr != nil {
			response.Counts.Errors++
		}
		if resumeTaskChanged(before, tasks[i]) || action == "continue" || action == "blocked" {
			response.Counts.Changed++
			response.Tasks = append(response.Tasks, cloneTaskState(tasks[i]))
			if response.Action == "none" || action == "blocked" || action == "continue" {
				response.Action = action
			}
		} else {
			response.Counts.Unchanged++
		}
	}
	for i := range blocked {
		response.Counts.Checked++
		response.Counts.Unchanged++
		response.Tasks = append(response.Tasks, cloneTaskState(blocked[i]))
		if response.Action == "none" {
			response.Action = "blocked"
		}
	}
	response.Warnings = s.state.LegacyWarnings()
	return response, nil
}

func (s *Service) Poll(ctx context.Context, id string) (Envelope, error) {
	task, err := s.state.Load(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Envelope{}, cliError("state_not_found", "client task was not found", 3)
		}
		return Envelope{}, err
	}
	checked := false
	action, advanceErr := s.advanceTask(ctx, task, &checked)
	if advanceErr != nil {
		return Envelope{}, attachTask(advanceErr, task)
	}
	return successEnvelope("poll", action, task), nil
}

func (s *Service) advanceTask(ctx context.Context, task *TaskState, capabilityChecked *bool) (string, error) {
	action := actionForTask(*task)
	for index := range task.Operations {
		operation := &task.Operations[index]
		switch operation.Phase {
		case "prepared", "submitting", "submission_unknown":
			if !*capabilityChecked {
				if err := s.client.Health(ctx); err != nil {
					return action, s.recordOperationError(task, index, err)
				}
				*capabilityChecked = true
			}
			_, err := s.submitOperation(ctx, task, index)
			if err != nil {
				if task.Status == "blocked" {
					return "blocked", err
				}
				return action, err
			}
			action = "wait"
		case "submitted":
			status, err := s.client.Task(ctx, operation.ProxymaTaskID)
			if err != nil {
				if typed, ok := err.(*commandError); ok && typed.code == "not_found" {
					if blockErr := s.blockOperation(task, index, "Proxyma task was not found", "verify the Proxyma server and task history"); blockErr != nil {
						return "blocked", blockErr
					}
					return "blocked", nil
				}
				return action, s.recordOperationError(task, index, err)
			}
			if status.ClientOperationID != operation.OperationID {
				if blockErr := s.blockOperation(task, index, "Proxyma task operation identity did not match durable client state", "inspect the Proxyma server and preserved task state before retrying"); blockErr != nil {
					return "blocked", blockErr
				}
				return "blocked", nil
			}
			var applyErr error
			action, applyErr = s.applyProxymaStatus(task, index, status)
			if applyErr != nil {
				return action, applyErr
			}
		}
	}
	return action, nil
}

func (s *Service) applyProxymaStatus(task *TaskState, index int, response taskResponse) (string, error) {
	now := s.now().UTC()
	operation := &task.Operations[index]
	operation.ProxymaStatus = response.Status
	operation.CheckedAt = &now
	operation.LastError = nil
	task.LastCheckedAt = &now
	action := "wait"
	switch response.Status {
	case "created", "waiting_user_response", "accepted_by_user", "running":
		task.Status = "waiting_proxyma"
	case "completed":
		operation.Phase = "completed"
		operation.Result = response.Result
		task.Status = "pending"
		action = "continue"
	case "failed", "timed_out", "interrupted", "abandoned", "blocked":
		operation.Phase = "blocked"
		operation.Result = response.Result
		task.Status = "blocked"
		task.BlockedReason = "Proxyma command ended with status " + response.Status
		task.RequiredNextAction = "inspect the Proxyma result and decide whether a new command is appropriate"
		action = "blocked"
	default:
		operation.Phase = "blocked"
		task.Status = "blocked"
		task.BlockedReason = "Proxyma returned unknown status " + response.Status
		task.RequiredNextAction = "upgrade proxyma-client or inspect the server response"
		action = "blocked"
	}
	touch(task, now)
	if err := s.state.Save(*task); err != nil {
		return action, err
	}
	return action, nil
}

func (s *Service) recordOperationError(task *TaskState, index int, err error) error {
	now := s.now().UTC()
	task.Operations[index].LastError = safeErrorFrom(err, now)
	task.LastCheckedAt = &now
	touch(task, now)
	if saveErr := s.state.Save(*task); saveErr != nil {
		return saveErr
	}
	return err
}

func (s *Service) blockOperation(task *TaskState, index int, reason, next string) error {
	now := s.now().UTC()
	task.Operations[index].Phase = "blocked"
	task.Status = "blocked"
	task.BlockedReason = reason
	task.RequiredNextAction = next
	touch(task, now)
	return s.state.Save(*task)
}

func (s *Service) Update(input UpdateInput) (Envelope, error) {
	if input.ProtocolVersion != "" && input.ProtocolVersion != ProtocolVersion {
		return Envelope{}, cliError("invalid_input", "unsupported protocol_version", 2)
	}
	task, err := s.loadForMutation(input.ClientTaskID, input.ExpectedRevision)
	if err != nil {
		return Envelope{}, err
	}
	if hasInFlight(*task) && (input.Status == "blocked" || input.Status == "failed") {
		return Envelope{}, cliError("operation_in_flight", "cannot make a task final while an operation is in flight", 3)
	}
	if input.RemainingWork != "" {
		task.RemainingWork = input.RemainingWork
	}
	switch input.Status {
	case "", "pending":
		if input.Status == "pending" {
			task.Status = "pending"
		}
	case "blocked":
		if input.BlockedReason == "" || input.RequiredNextAction == "" {
			return Envelope{}, cliError("invalid_input", "blocked_reason and required_next_action are required", 2)
		}
		task.Status = "blocked"
		task.BlockedReason = input.BlockedReason
		task.RequiredNextAction = input.RequiredNextAction
	case "failed":
		if input.FailureReason == "" {
			return Envelope{}, cliError("invalid_input", "failure_reason is required", 2)
		}
		task.Status = "failed"
		task.FailureReason = input.FailureReason
	default:
		return Envelope{}, cliError("invalid_input", "task-update status must be pending, blocked, or failed", 2)
	}
	touch(task, s.now().UTC())
	if err := s.state.Save(*task); err != nil {
		return Envelope{}, err
	}
	return successEnvelope("task-update", actionForTask(*task), task), nil
}

func (s *Service) Complete(input CompleteInput) (Envelope, error) {
	if input.ProtocolVersion != "" && input.ProtocolVersion != ProtocolVersion {
		return Envelope{}, cliError("invalid_input", "unsupported protocol_version", 2)
	}
	if strings.TrimSpace(input.FinalSummary) == "" {
		return Envelope{}, cliError("invalid_input", "final_summary is required", 2)
	}
	task, err := s.loadForMutation(input.ClientTaskID, input.ExpectedRevision)
	if err != nil {
		return Envelope{}, err
	}
	if hasInFlight(*task) {
		return Envelope{}, cliError("operation_in_flight", "cannot complete a task while an operation is in flight", 3)
	}
	task.Status = "completed"
	task.FinalSummary = input.FinalSummary
	task.RemainingWork = ""
	touch(task, s.now().UTC())
	if err := s.state.Save(*task); err != nil {
		return Envelope{}, err
	}
	return successEnvelope("task-complete", "completed", task), nil
}

func (s *Service) InspectTask(id string) (Envelope, error) {
	task, err := s.state.Load(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Envelope{}, cliError("state_not_found", "client task was not found", 3)
		}
		return Envelope{}, err
	}
	return successEnvelope("inspect", actionForTask(*task), task), nil
}

func (s *Service) InspectTarget(ctx context.Context, target Target) (Envelope, error) {
	resolved, err := s.client.Resolve(ctx, target)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: "inspect", Action: "none", Target: &resolved}, nil
}

func (s *Service) Policy(ctx context.Context, target Target) (Envelope, error) {
	guidance, err := s.client.Policy(ctx, target)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: "policy", Action: "none", Policy: &guidance}, nil
}

func (s *Service) Doctor(ctx context.Context) (Envelope, error) {
	if err := s.client.Health(ctx); err != nil {
		return Envelope{}, err
	}
	if _, err := s.state.List(); err != nil {
		return Envelope{}, err
	}
	warnings := s.state.LegacyWarnings()
	if s.cfg.Path != "" {
		legacyConfig := filepath.Join(filepath.Dir(s.cfg.Path), "proxyma-harness-run.json")
		if info, err := os.Stat(legacyConfig); err == nil && !info.IsDir() {
			warnings = append(warnings, "legacy v1 harness configuration found at "+legacyConfig+"; archive or remove it")
		}
	}
	return Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: "doctor", Action: "none", Warnings: warnings}, nil
}

func (s *Service) loadForMutation(id string, expected int64) (*TaskState, error) {
	if id == "" {
		return nil, cliError("invalid_input", "client_task_id is required", 2)
	}
	task, err := s.state.Load(id)
	if err != nil {
		return nil, err
	}
	if task.Revision != expected {
		return nil, cliError("state_conflict", fmt.Sprintf("expected revision %d", task.Revision), 3)
	}
	return task, nil
}

func validateSubmitInput(input SubmitInput) error {
	if input.ProtocolVersion != "" && input.ProtocolVersion != ProtocolVersion {
		return cliError("invalid_input", "unsupported protocol_version", 2)
	}
	if input.ClientTaskID == "" && strings.TrimSpace(input.UserGoal) == "" {
		return cliError("invalid_input", "user_goal is required for a new task", 2)
	}
	if input.ClientTaskID != "" && input.ExpectedRevision == nil {
		return cliError("invalid_input", "expected_revision is required for an existing task", 2)
	}
	if (strings.TrimSpace(input.Target.Hostname) == "") == (strings.TrimSpace(input.Target.IP) == "") {
		return cliError("invalid_input", "target must contain exactly one of hostname or ip", 2)
	}
	if strings.TrimSpace(input.Payload) == "" || strings.TrimSpace(input.Reasoning) == "" || strings.TrimSpace(input.HarnessName) == "" {
		return cliError("invalid_input", "payload, reasoning, and harness_name are required", 2)
	}
	return nil
}

func hashCommandRequest(request commandRequest) (string, error) {
	request.ClientOperationID = ""
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func touch(task *TaskState, at time.Time) {
	task.Revision++
	task.UpdatedAt = at
}

func hasInFlight(task TaskState) bool {
	for _, operation := range task.Operations {
		switch operation.Phase {
		case "prepared", "submitting", "submission_unknown", "submitted":
			return true
		}
	}
	return false
}

func actionForTask(task TaskState) string {
	switch task.Status {
	case "waiting_proxyma":
		return "wait"
	case "pending":
		return "continue"
	case "blocked", "failed":
		return "blocked"
	case "completed":
		return "completed"
	default:
		return "none"
	}
}

func resumeTaskChanged(before, after TaskState) bool {
	normalize := func(task TaskState) TaskState {
		task.Revision = 0
		task.UpdatedAt = time.Time{}
		task.LastCheckedAt = nil
		for index := range task.Operations {
			task.Operations[index].CheckedAt = nil
			if task.Operations[index].LastError != nil {
				task.Operations[index].LastError.At = time.Time{}
			}
		}
		return task
	}
	return !reflect.DeepEqual(normalize(before), normalize(after))
}

func safeErrorFrom(err error, at time.Time) *SafeError {
	if typed, ok := err.(*commandError); ok {
		return &SafeError{Code: typed.code, Message: typed.message, Retryable: typed.retryable, Hint: typed.hint, At: at}
	}
	return &SafeError{Code: "internal_error", Message: err.Error(), At: at}
}

func attachTask(err error, task *TaskState) error {
	if typed, ok := err.(*commandError); ok {
		copied := cloneTaskState(*task)
		typed.task = &copied
		return typed
	}
	return err
}

func successEnvelope(command, action string, task *TaskState) Envelope {
	copied := cloneTaskState(*task)
	return Envelope{ProtocolVersion: ProtocolVersion, OK: true, Command: command, Action: action, Task: &copied}
}
