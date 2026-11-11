package clientcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"proxyma/internal/store"

	"github.com/google/uuid"
)

var requestHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type StateStore struct {
	root      string
	active    *store.FileStore
	blocked   *store.FileStore
	closed    *store.FileStore
	journal   *store.FileStore
	summaries string
}

const (
	activeStateDir  = "active"
	blockedStateDir = "blocked"
	closedStateDir  = "closed"
	legacyStateDir  = "tasks"
)

type stateLock struct {
	file *os.File
}

func NewStateStore(root string) *StateStore {
	return &StateStore{
		root:      root,
		active:    store.New(filepath.Join(root, activeStateDir)),
		blocked:   store.New(filepath.Join(root, blockedStateDir)),
		closed:    store.New(filepath.Join(root, closedStateDir)),
		journal:   store.New(filepath.Join(root, ".journal")),
		summaries: filepath.Join(root, "summaries"),
	}
}

func (s *StateStore) Lock(timeout time.Duration) (*stateLock, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, ".proxyma-client.lock")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, lockErr := tryLockFile(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, lockErr
		}
		if locked {
			_ = file.Truncate(0)
			_, _ = file.Seek(0, 0)
			_, _ = fmt.Fprintf(file, "pid=%d\nacquired_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = file.Sync()
			return &stateLock{file: file}, nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, &commandError{code: "state_locked", message: "another proxyma-client process holds the state lock", retryable: true, exitCode: 3}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (l *stateLock) Close() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		if err := unlockFile(l.file); err != nil {
			_ = l.file.Close()
			return err
		}
		return l.file.Close()
	}
	return nil
}

func (s *StateStore) Recover() error {
	if err := s.checkLegacyLayout(); err != nil {
		return err
	}
	paths, err := s.journal.ListJSONFiles()
	if err != nil {
		return err
	}
	for _, path := range paths {
		var next TaskState
		if err := decodeStateFile(path, &next); err != nil {
			return &commandError{code: "state_corrupt", message: err.Error(), exitCode: 3}
		}
		if err := validateTaskState(next); err != nil {
			return &commandError{code: "state_corrupt", message: fmt.Sprintf("invalid journal %s: %v", path, err), exitCode: 3}
		}
		if recordIDFromPath(path) != next.ClientTaskID {
			return &commandError{code: "state_corrupt", message: fmt.Sprintf("journal filename does not match client_task_id in %s", path), exitCode: 3}
		}
		candidates, err := s.readCandidates(next.ClientTaskID)
		if err != nil {
			return err
		}
		candidates = append(candidates, stateCandidate{path: path, task: next})
		chosen, err := chooseCandidate(candidates)
		if err != nil {
			return err
		}
		target, err := s.storeForStatus(chosen.task.Status)
		if err != nil {
			return err
		}
		if err := target.WriteJSON(chosen.task.ClientTaskID, chosen.task); err != nil {
			return err
		}
		if err := s.removeTaskCopies(chosen.task.ClientTaskID, target.RecordPath(chosen.task.ClientTaskID)); err != nil {
			return err
		}
		if err := s.writeSummary(chosen.task); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *StateStore) Save(task TaskState) error {
	if err := validateTaskState(task); err != nil {
		return err
	}
	current, err := s.Load(task.ClientTaskID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if current == nil {
		if task.Revision != 1 {
			return &commandError{code: "state_conflict", message: "a new task must start at revision 1", exitCode: 3}
		}
	} else if task.Revision != current.Revision+1 {
		return &commandError{code: "state_conflict", message: fmt.Sprintf("next revision must be %d", current.Revision+1), exitCode: 3}
	}
	if err := s.journal.WriteJSON(task.ClientTaskID, task); err != nil {
		return err
	}
	target, err := s.storeForStatus(task.Status)
	if err != nil {
		return err
	}
	if err := target.WriteJSON(task.ClientTaskID, task); err != nil {
		return err
	}
	if err := s.removeTaskCopies(task.ClientTaskID, target.RecordPath(task.ClientTaskID)); err != nil {
		return err
	}
	if err := s.writeSummary(task); err != nil {
		return err
	}
	return s.journal.DeleteJSON(task.ClientTaskID)
}

func (s *StateStore) Load(id string) (*TaskState, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, &commandError{code: "invalid_input", message: "client_task_id must be a UUID", exitCode: 2}
	}
	candidates, err := s.readCandidates(id)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, os.ErrNotExist
	}
	chosen, err := chooseCandidate(candidates)
	if err != nil {
		return nil, err
	}
	return &chosen.task, nil
}

func (s *StateStore) ListActive() ([]TaskState, error) {
	return s.listStore(s.active, activeStateDir)
}

func (s *StateStore) ListBlocked() ([]TaskState, error) {
	return s.listStore(s.blocked, blockedStateDir)
}

func (s *StateStore) List() ([]TaskState, error) {
	if err := s.checkLegacyLayout(); err != nil {
		return nil, err
	}
	items := make([]TaskState, 0)
	for _, state := range []struct {
		store *store.FileStore
		dir   string
	}{{s.active, activeStateDir}, {s.blocked, blockedStateDir}, {s.closed, closedStateDir}} {
		part, err := s.listStore(state.store, state.dir)
		if err != nil {
			return nil, err
		}
		items = append(items, part...)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

func (s *StateStore) listStore(taskStore *store.FileStore, stateDir string) ([]TaskState, error) {
	paths, err := taskStore.ListJSONFiles()
	if err != nil {
		return nil, err
	}
	items := make([]TaskState, 0, len(paths))
	for _, path := range paths {
		var task TaskState
		if err := decodeStateFile(path, &task); err != nil {
			return nil, &commandError{code: "state_corrupt", message: err.Error(), exitCode: 3}
		}
		if err := validateTaskState(task); err != nil {
			return nil, &commandError{code: "state_corrupt", message: fmt.Sprintf("invalid task %s: %v", path, err), exitCode: 3}
		}
		if recordIDFromPath(path) != task.ClientTaskID || stateDirForStatus(task.Status) != stateDir {
			return nil, &commandError{code: "state_corrupt", message: fmt.Sprintf("task filename does not match client_task_id in %s", path), exitCode: 3}
		}
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

type stateCandidate struct {
	path string
	task TaskState
}

func (s *StateStore) readCandidates(id string) ([]stateCandidate, error) {
	var candidates []stateCandidate
	for _, taskStore := range []*store.FileStore{s.active, s.blocked, s.closed} {
		path := taskStore.RecordPath(id)
		var task TaskState
		if err := decodeStateFile(path, &task); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if err := validateTaskState(task); err != nil {
			return nil, &commandError{code: "state_corrupt", message: fmt.Sprintf("invalid task %s: %v", id, err), exitCode: 3}
		}
		if stateDirForStatus(task.Status) != filepath.Base(filepath.Dir(path)) {
			return nil, &commandError{code: "state_corrupt", message: fmt.Sprintf("task %s is in the wrong state directory", id), exitCode: 3}
		}
		candidates = append(candidates, stateCandidate{path: path, task: task})
	}
	return candidates, nil
}

func chooseCandidate(candidates []stateCandidate) (stateCandidate, error) {
	if len(candidates) == 0 {
		return stateCandidate{}, os.ErrNotExist
	}
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.task.Revision > chosen.task.Revision {
			chosen = candidate
		}
	}
	for _, candidate := range candidates {
		if candidate.task.Revision == chosen.task.Revision && !reflect.DeepEqual(candidate.task, chosen.task) {
			return stateCandidate{}, &commandError{code: "state_corrupt", message: fmt.Sprintf("conflicting copies of task %s", chosen.task.ClientTaskID), exitCode: 3}
		}
	}
	return chosen, nil
}

func (s *StateStore) removeTaskCopies(id, keepPath string) error {
	for _, taskStore := range []*store.FileStore{s.active, s.blocked, s.closed} {
		path := taskStore.RecordPath(id)
		if path == keepPath {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *StateStore) storeForStatus(status string) (*store.FileStore, error) {
	switch stateDirForStatus(status) {
	case activeStateDir:
		return s.active, nil
	case blockedStateDir:
		return s.blocked, nil
	case closedStateDir:
		return s.closed, nil
	default:
		return nil, fmt.Errorf("invalid task status %q", status)
	}
}

func stateDirForStatus(status string) string {
	switch status {
	case "pending", "waiting_proxyma":
		return activeStateDir
	case "blocked":
		return blockedStateDir
	case "completed", "failed":
		return closedStateDir
	default:
		return ""
	}
}

func (s *StateStore) checkLegacyLayout() error {
	paths, err := filepath.Glob(filepath.Join(s.root, legacyStateDir, "*.json"))
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		return &commandError{code: "state_layout", message: fmt.Sprintf("unsupported flat task state layout; move or remove JSON files under %s", filepath.Join(s.root, legacyStateDir)), exitCode: 3}
	}
	return nil
}

func (s *StateStore) LegacyWarnings() []string {
	var warnings []string
	for _, name := range []string{"pending", "done", "completed", "failed", legacyStateDir} {
		matches, _ := filepath.Glob(filepath.Join(s.root, name, "*.json"))
		if len(matches) > 0 {
			warnings = append(warnings, fmt.Sprintf("legacy v1 state files found under %s; archive or remove them", filepath.Join(s.root, name)))
		}
	}
	return warnings
}

func validateTaskState(task TaskState) error {
	if task.SchemaVersion != StateVersion {
		return fmt.Errorf("unsupported schema_version %q", task.SchemaVersion)
	}
	if task.ClientTaskID == "" || task.Revision < 1 || strings.TrimSpace(task.UserGoal) == "" || task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		return fmt.Errorf("task identity, revision, goal, and timestamps are required")
	}
	if _, err := uuid.Parse(task.ClientTaskID); err != nil {
		return fmt.Errorf("client_task_id must be a UUID")
	}
	switch task.Status {
	case "pending", "waiting_proxyma", "completed", "blocked", "failed":
	default:
		return fmt.Errorf("invalid task status %q", task.Status)
	}
	if task.Status == "blocked" && (strings.TrimSpace(task.BlockedReason) == "" || strings.TrimSpace(task.RequiredNextAction) == "") {
		return fmt.Errorf("blocked tasks require blocked_reason and required_next_action")
	}
	if task.Status == "failed" && strings.TrimSpace(task.FailureReason) == "" {
		return fmt.Errorf("failed tasks require failure_reason")
	}
	if task.Status == "completed" && strings.TrimSpace(task.FinalSummary) == "" {
		return fmt.Errorf("completed tasks require final_summary")
	}
	operationIDs := make(map[string]struct{}, len(task.Operations))
	for _, operation := range task.Operations {
		if operation.OperationID == "" || operation.RequestHash == "" || operation.PreparedAt.IsZero() {
			return fmt.Errorf("operation identity, request hash, and prepared_at are required")
		}
		if _, err := uuid.Parse(operation.OperationID); err != nil {
			return fmt.Errorf("operation_id must be a UUID")
		}
		if _, exists := operationIDs[operation.OperationID]; exists {
			return fmt.Errorf("duplicate operation_id %q", operation.OperationID)
		}
		operationIDs[operation.OperationID] = struct{}{}
		if !requestHashPattern.MatchString(operation.RequestHash) {
			return fmt.Errorf("request_hash must be a lowercase SHA-256 digest")
		}
		if (strings.TrimSpace(operation.Target.Hostname) == "") == (strings.TrimSpace(operation.Target.IP) == "") {
			return fmt.Errorf("operation target must contain exactly one of hostname or ip")
		}
		if strings.TrimSpace(operation.Payload) == "" || strings.TrimSpace(operation.CanonicalCommand) == "" || strings.TrimSpace(operation.Reasoning) == "" || strings.TrimSpace(operation.HarnessName) == "" {
			return fmt.Errorf("operation payload, canonical command, reasoning, and requester are required")
		}
		switch operation.PayloadKind {
		case "posix_shell", "windows_shell", "powershell":
		default:
			return fmt.Errorf("invalid payload_kind %q", operation.PayloadKind)
		}
		expectedHash, err := hashCommandRequest(commandRequest{
			Command:     operation.CanonicalCommand,
			Reasoning:   operation.Reasoning,
			Target:      operation.Target,
			HarnessName: operation.HarnessName,
			Metadata:    operation.Metadata,
		})
		if err != nil || expectedHash != operation.RequestHash {
			return fmt.Errorf("request_hash does not match the persisted operation request")
		}
		switch operation.Phase {
		case "prepared", "submitting", "submission_unknown", "submitted", "completed", "blocked":
		default:
			return fmt.Errorf("invalid operation phase %q", operation.Phase)
		}
	}
	return nil
}

func decodeStateFile(path string, target *TaskState) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decodeStrictJSON(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func recordIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func (s *StateStore) writeSummary(task TaskState) error {
	if err := os.MkdirAll(s.summaries, 0o700); err != nil {
		return err
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# Proxyma task %s\n\n", task.ClientTaskID)
	fmt.Fprintf(&builder, "- Status: `%s`\n- Revision: `%d`\n- Goal: %s\n- Remaining work: %s\n", task.Status, task.Revision, task.UserGoal, task.RemainingWork)
	if task.BlockedReason != "" {
		fmt.Fprintf(&builder, "- Blocked reason: %s\n- Required next action: %s\n", task.BlockedReason, task.RequiredNextAction)
	}
	if task.FinalSummary != "" {
		fmt.Fprintf(&builder, "\n## Final summary\n\n%s\n", task.FinalSummary)
	}
	fmt.Fprintf(&builder, "\n## Operations\n")
	for _, operation := range task.Operations {
		fmt.Fprintf(&builder, "\n- `%s`: phase `%s`, Proxyma status `%s`\n", operation.OperationID, operation.Phase, operation.ProxymaStatus)
	}
	path := filepath.Join(s.summaries, task.ClientTaskID+".md")
	tmp, err := os.CreateTemp(s.summaries, ".tmp-*.md")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(builder.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	}
	_ = os.Remove(path)
	return os.Rename(tmpPath, path)
}

func cloneTaskState(task TaskState) TaskState {
	data, _ := json.Marshal(task)
	var copied TaskState
	_ = json.Unmarshal(data, &copied)
	return copied
}
