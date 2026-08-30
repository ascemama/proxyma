package tasks

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/sshclient"
	"proxyma/internal/transcriptor"
	"proxyma/internal/winrmclient"
)

func TestSubmitIdempotentReplaysAndConflicts(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{})
	command := CommandContext{AgentID: "agent-1", ClientOperationID: "op-1", ClientRequestHash: "sha256:one", Command: "echo done", Reasoning: "test"}
	first, replayed, err := manager.SubmitIdempotent(context.Background(), command)
	if err != nil || replayed {
		t.Fatalf("first submit replayed=%t err=%v", replayed, err)
	}
	second, replayed, err := manager.SubmitIdempotent(context.Background(), command)
	if err != nil || !replayed || second.TaskID != first.TaskID {
		t.Fatalf("replay task=%#v replayed=%t err=%v", second, replayed, err)
	}
	command.ClientRequestHash = "sha256:two"
	if _, _, err := manager.SubmitIdempotent(context.Background(), command); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
}

func TestSubmitIdempotentConcurrentRequestsCreateOneTask(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{})
	command := CommandContext{AgentID: "agent-1", ClientOperationID: "op-1", ClientRequestHash: "sha256:one", Command: "echo done", Reasoning: "test"}
	const count = 12
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			task, _, err := manager.SubmitIdempotent(context.Background(), command)
			if err != nil {
				errs <- err
				return
			}
			ids <- task.TaskID
		}()
	}
	wait.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("task ids differ: %q and %q", first, id)
		}
	}
	if got := len(manager.List()); got != 1 {
		t.Fatalf("task count=%d", got)
	}
}

func TestClientOperationScopeAndOptionalBehavior(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{})
	base := CommandContext{ClientOperationID: "op-1", ClientRequestHash: "sha256:one", Command: "echo done", Reasoning: "test"}
	base.AgentID = "agent-1"
	first, _, err := manager.SubmitIdempotent(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	base.AgentID = "agent-2"
	second, replayed, err := manager.SubmitIdempotent(context.Background(), base)
	if err != nil || replayed || second.TaskID == first.TaskID {
		t.Fatalf("scoped submit task=%#v replayed=%t err=%v", second, replayed, err)
	}
	withoutKey := CommandContext{Command: "echo done", Reasoning: "test"}
	third, _ := manager.Submit(context.Background(), withoutKey)
	fourth, _ := manager.Submit(context.Background(), withoutKey)
	if third.TaskID == fourth.TaskID {
		t.Fatal("requests without operation ids must create distinct tasks")
	}
}

func TestRecoverRebuildsIdempotencyIndex(t *testing.T) {
	created := time.Now().UTC()
	store := newMemoryTaskStore(Task{TaskID: "task-1", AgentID: "agent-1", ClientOperationID: "op-1", ClientRequestHash: "sha256:one", Command: "echo done", Reasoning: "test", Status: StatusCompleted, CreatedAt: created})
	manager := NewManager(store, executor.Executor{})
	if err := manager.Recover(); err != nil {
		t.Fatal(err)
	}
	task, found, err := manager.LookupOperation("agent-1", "op-1", "sha256:one")
	if err != nil || !found || task.TaskID != "task-1" {
		t.Fatalf("task=%#v found=%t err=%v", task, found, err)
	}
}

func TestRecoverRejectsDuplicateOperationIndex(t *testing.T) {
	created := time.Now().UTC()
	store := newMemoryTaskStore(
		Task{TaskID: "task-1", AgentID: "agent-1", ClientOperationID: "op-1", ClientRequestHash: "sha256:one", Command: "one", Reasoning: "test", Status: StatusCompleted, CreatedAt: created},
		Task{TaskID: "task-2", AgentID: "agent-1", ClientOperationID: "op-1", ClientRequestHash: "sha256:two", Command: "two", Reasoning: "test", Status: StatusCompleted, CreatedAt: created},
	)
	if err := NewManager(store, executor.Executor{}).Recover(); err == nil {
		t.Fatal("expected duplicate operation recovery error")
	}
}

type memoryTaskStore struct {
	records map[string]Task
}

type fakeWinRMRunner struct {
	result   winrmclient.Result
	requests []winrmclient.Request
}

func (f *fakeWinRMRunner) Run(_ context.Context, request winrmclient.Request) winrmclient.Result {
	f.requests = append(f.requests, request)
	return f.result
}

func newMemoryTaskStore(records ...Task) *memoryTaskStore {
	store := &memoryTaskStore{records: map[string]Task{}}
	for _, record := range records {
		store.records[record.TaskID] = record
	}
	return store
}

func (s *memoryTaskStore) LoadAll() ([]Task, error) {
	records := make([]Task, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	return records, nil
}

func (s *memoryTaskStore) Save(task Task) error {
	s.records[task.TaskID] = task
	return nil
}

type memoryInteractionStore struct {
	records map[string]interaction.Interaction
}

func newMemoryInteractionStore() *memoryInteractionStore {
	return &memoryInteractionStore{records: map[string]interaction.Interaction{}}
}

func (s *memoryInteractionStore) LoadAll() ([]interaction.Interaction, error) {
	records := make([]interaction.Interaction, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	return records, nil
}

func (s *memoryInteractionStore) Save(record interaction.Interaction) error {
	s.records[record.InteractionID] = record
	return nil
}

type fakeSSHRunner struct {
	result   sshclient.Result
	requests []sshclient.Request
	debug    bool
	logger   *log.Logger
}

func (f *fakeSSHRunner) Run(_ context.Context, request sshclient.Request) sshclient.Result {
	f.requests = append(f.requests, request)
	if f.debug {
		logger := f.logger
		if logger == nil {
			logger = log.Default()
		}
		logger.Printf("debug ssh effective request host=%s port=%d strategy=go_ssh_client username=%q remote_command=%q",
			request.Host,
			request.Port,
			RedactCredentialValues(request.Username, request.SensitiveValues...),
			RedactCredentialValues(request.RemoteCommand, request.SensitiveValues...),
		)
	}
	if f.result.ExitStatus == nil && f.result.Error == "" && !f.result.TimedOut {
		code := 0
		f.result.ExitStatus = &code
	}
	return f.result
}

func TestSubmitRunsTaskToCompletion(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusCreated {
		t.Fatalf("initial status = %s", task.Status)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if final.Result == nil || final.Result.ExitStatus == nil || *final.Result.ExitStatus != 0 {
		t.Fatalf("result = %#v", final.Result)
	}
}

func TestSubmitDispatchesSSHExecutionPlan(t *testing.T) {
	fake := &fakeSSHRunner{result: sshclient.Result{Stdout: "host\n"}}
	manager := NewManagerWithOptions(
		newMemoryTaskStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		ManagerOptions{SSHClient: fake},
	)
	task, err := manager.Submit(context.Background(), CommandContext{
		Command:   `ssh {user}@127.0.0.1 "hostname"`,
		Reasoning: "test",
		Target:    &Target{IP: "127.0.0.1"},
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		ExecutionPlan: &transcriptor.Plan{
			Kind: transcriptor.KindSSHRemoteCommand,
			Transcription: transcriptor.Metadata{
				Transcribed:   true,
				CommandFamily: transcriptor.KindSSHRemoteCommand,
				Strategy:      transcriptor.StrategyGoSSHClient,
			},
			SSHRequest:      &sshclient.Request{Host: "127.0.0.1", Port: 22, Username: "demo", Password: "secret", RemoteCommand: "hostname"},
			SensitiveValues: []string{"demo", "secret"},
		},
		SensitiveValues: []string{"demo", "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Command != `ssh {user}@127.0.0.1 "hostname"` {
		t.Fatalf("final command = %q", final.Command)
	}
	if final.Target == nil || final.Target.IP != "127.0.0.1" {
		t.Fatalf("target = %#v", final.Target)
	}
	if len(fake.requests) != 1 || fake.requests[0].RemoteCommand != "hostname" {
		t.Fatalf("ssh requests = %#v", fake.requests)
	}
}

func TestSubmitDispatchesWinRMExecutionPlan(t *testing.T) {
	fake := &fakeWinRMRunner{result: winrmclient.Result{Stdout: "service\n"}}
	manager := NewManagerWithOptions(newMemoryTaskStore(), executor.Executor{}, ManagerOptions{WinRMClient: fake})
	task, err := manager.Submit(context.Background(), CommandContext{
		Command: `winrm {user}@win.example Get-Service`, Reasoning: "test",
		Target:        &Target{Transport: "winrm", Hostname: "win.example"},
		Transcription: &transcriptor.Metadata{Transcribed: true, CommandFamily: transcriptor.KindWinRMRemoteCommand, Strategy: transcriptor.StrategyGoWinRMClient},
		ExecutionPlan: &transcriptor.Plan{
			Kind:         transcriptor.KindWinRMRemoteCommand,
			WinRMRequest: &winrmclient.Request{Host: "win.example", Scheme: "https", Port: 5986, Username: "Administrator", Password: "secret", PowerShell: "Get-Service"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started=%d err=%v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusCompleted || len(fake.requests) != 1 || fake.requests[0].PowerShell != "Get-Service" {
		t.Fatalf("final=%#v requests=%#v", final, fake.requests)
	}
}

func TestSubmitFailedTask(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	task, err := manager.Submit(context.Background(), CommandContext{Command: "exit 3", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusFailed {
		t.Fatalf("status = %s", final.Status)
	}
}

func TestTaskWaitsForInteractionAndResumesAfterResponse(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pending := interaction.Summary{
		InteractionID: "interaction-1",
		Kind:          interaction.KindQuestion,
		Status:        interaction.StatusPending,
		CreatedAt:     time.Now().UTC(),
	}
	if _, err := manager.WaitForInteraction(task.TaskID, pending); err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 0 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	answered := interaction.Interaction{
		InteractionID:    pending.InteractionID,
		TaskID:           task.TaskID,
		Kind:             interaction.KindQuestion,
		ExpectedResponse: interaction.ExpectedText,
		Status:           interaction.StatusAnswered,
		Response:         &interaction.Response{Answer: "continue"},
	}
	if _, applied, err := manager.ApplyInteraction(answered); err != nil || !applied {
		t.Fatalf("applied = %t, err = %v", applied, err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
}

func TestRejectedApprovalBlocksWaitingTask(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pending := interaction.Summary{InteractionID: "interaction-1", Kind: interaction.KindApprovalRequest, Status: interaction.StatusPending, CreatedAt: time.Now().UTC()}
	if _, err := manager.WaitForInteraction(task.TaskID, pending); err != nil {
		t.Fatal(err)
	}
	approved := false
	answered := interaction.Interaction{
		InteractionID:    pending.InteractionID,
		TaskID:           task.TaskID,
		Kind:             interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval,
		Status:           interaction.StatusAnswered,
		Response:         &interaction.Response{Approved: &approved},
	}
	if _, applied, err := manager.ApplyInteraction(answered); err != nil || !applied {
		t.Fatalf("applied = %t, err = %v", applied, err)
	}
	final, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if final.Status != StatusBlocked || final.Result == nil || final.Result.Error == "" {
		t.Fatalf("final = %#v", final)
	}
}

func TestApprovedApprovalMarksTaskAcceptedByUser(t *testing.T) {
	manager := NewManager(newMemoryTaskStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pending := interaction.Summary{InteractionID: "interaction-1", Kind: interaction.KindApprovalRequest, Status: interaction.StatusPending, CreatedAt: time.Now().UTC()}
	if _, err := manager.WaitForInteraction(task.TaskID, pending); err != nil {
		t.Fatal(err)
	}
	approved := true
	answered := interaction.Interaction{
		InteractionID:    pending.InteractionID,
		TaskID:           task.TaskID,
		Kind:             interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval,
		Status:           interaction.StatusAnswered,
		Response:         &interaction.Response{Approved: &approved},
	}
	accepted, applied, err := manager.ApplyInteraction(answered)
	if err != nil || !applied {
		t.Fatalf("applied = %t, err = %v", applied, err)
	}
	if accepted.Status != StatusAcceptedByUser || accepted.PendingInteraction != nil {
		t.Fatalf("accepted = %#v", accepted)
	}
	if accepted.Approval == nil || accepted.Approval.Status != ApprovalStatusApproved {
		t.Fatalf("approval = %#v", accepted.Approval)
	}
	if got := accepted.History[len(accepted.History)-1]; got.Status != StatusAcceptedByUser || got.Message != "user approved interaction" {
		t.Fatalf("history = %#v", accepted.History)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
}

func TestInteractionTimeoutBlocksWaitingTask(t *testing.T) {
	now := time.Date(2026, 5, 31, 1, 0, 0, 0, time.UTC)
	interactionService := interaction.New(newMemoryInteractionStore(), interaction.Options{
		Timeout: time.Minute,
		Now:     func() time.Time { return now },
	})
	manager := NewManagerWithOptions(
		newMemoryTaskStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		ManagerOptions{Interactions: interactionService},
	)
	manager.now = func() time.Time { return now }
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := interactionService.Request(context.Background(), interaction.Request{
		TaskID:  task.TaskID,
		Kind:    interaction.KindQuestion,
		Subject: "Question",
		Message: "Answer?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WaitForInteraction(task.TaskID, record.ToSummary()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := manager.ManageState(context.Background()); err != nil {
		t.Fatal(err)
	}
	final, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if final.Status != StatusBlocked || final.PendingInteraction != nil {
		t.Fatalf("final = %#v", final)
	}
}

func TestDebugLogsCommandExecution(t *testing.T) {
	var logs bytes.Buffer
	fake := &fakeSSHRunner{result: sshclient.Result{Stdout: "done\n"}, debug: true, logger: log.New(&logs, "", 0)}
	manager := NewManagerWithOptions(
		newMemoryTaskStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		ManagerOptions{Debug: true, Logger: log.New(&logs, "", 0), SSHClient: fake},
	)
	task, err := manager.Submit(context.Background(), CommandContext{
		Command:   `ssh {user}@127.0.0.1 "hostname"`,
		Reasoning: "test",
		Target:    &Target{IP: "127.0.0.1"},
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		ExecutionPlan: &transcriptor.Plan{
			Kind: transcriptor.KindSSHRemoteCommand,
			Transcription: transcriptor.Metadata{
				Transcribed:   true,
				CommandFamily: transcriptor.KindSSHRemoteCommand,
				Strategy:      transcriptor.StrategyGoSSHClient,
			},
			SSHRequest:      &sshclient.Request{Host: "127.0.0.1", Port: 22, Username: "demo", Password: "secret", RemoteCommand: "hostname", SensitiveValues: []string{"demo", "secret"}},
			SensitiveValues: []string{"demo", "secret"},
		},
		SensitiveValues: []string{"demo", "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitForFinal(t, manager, task.TaskID)
	if final.Status != StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}

	output := logs.String()
	for _, want := range []string{
		"debug command start",
		"task_id=" + task.TaskID,
		"target=127.0.0.1",
		"command_family=ssh_remote_command",
		"strategy=go_ssh_client",
		"command=\"ssh {user}@127.0.0.1 \\\"hostname\\\"\"",
		`debug ssh effective request host=127.0.0.1 port=22 strategy=go_ssh_client username="[REDACTED]" remote_command="hostname"`,
		"debug command finish",
		"status=completed",
		"exit_status=0",
		`debug command result task_id=` + task.TaskID,
		`stdout="done\n"`,
		`stderr=""`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing %q in:\n%s", want, output)
		}
	}
}

func TestDebugLogsKeepRequestRedactedButShowRawResult(t *testing.T) {
	var logs bytes.Buffer
	fake := &fakeSSHRunner{result: sshclient.Result{Stdout: "secret\n", Stderr: "demo\n", Error: "secret failure"}, debug: true, logger: log.New(&logs, "", 0)}
	manager := NewManagerWithOptions(
		newMemoryTaskStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		ManagerOptions{Debug: true, Logger: log.New(&logs, "", 0), SSHClient: fake},
	)
	task, err := manager.Submit(context.Background(), CommandContext{
		Command:   `ssh {user}@127.0.0.1 "echo {password}"`,
		Reasoning: "test",
		Target:    &Target{IP: "127.0.0.1"},
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		ExecutionPlan: &transcriptor.Plan{
			Kind: transcriptor.KindSSHRemoteCommand,
			Transcription: transcriptor.Metadata{
				Transcribed:   true,
				CommandFamily: transcriptor.KindSSHRemoteCommand,
				Strategy:      transcriptor.StrategyGoSSHClient,
			},
			SSHRequest:      &sshclient.Request{Host: "127.0.0.1", Port: 22, Username: "demo", Password: "secret", RemoteCommand: "echo secret", SensitiveValues: []string{"demo", "secret"}},
			SensitiveValues: []string{"demo", "secret"},
		},
		SensitiveValues: []string{"demo", "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	waitForFinal(t, manager, task.TaskID)

	output := logs.String()
	if strings.Contains(output, "echo secret") {
		t.Fatalf("debug log leaked raw remote command in:\n%s", output)
	}
	if !strings.Contains(output, `command="ssh {user}@127.0.0.1 \"echo {password}\""`) {
		t.Fatalf("debug log missing canonical command:\n%s", output)
	}
	for _, want := range []string{
		`username="[REDACTED]"`,
		`remote_command="echo [REDACTED]"`,
		`stdout="secret\n"`,
		`stderr="demo\n"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing raw result %q in:\n%s", want, output)
		}
	}
}

func TestDebugDisabledDoesNotLogCommandExecution(t *testing.T) {
	var logs bytes.Buffer
	manager := NewManagerWithOptions(
		newMemoryTaskStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		ManagerOptions{Logger: log.New(&logs, "", 0)},
	)
	task, err := manager.Submit(context.Background(), CommandContext{Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := manager.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	waitForFinal(t, manager, task.TaskID)
	if strings.Contains(logs.String(), "debug ") {
		t.Fatalf("unexpected debug logs: %s", logs.String())
	}
}

func TestRecoverMarksNonFinalAbandoned(t *testing.T) {
	now := time.Now().UTC()
	records := []Task{
		{
			TaskID:    "created",
			Command:   "echo later",
			Reasoning: "test",
			Status:    StatusCreated,
			CreatedAt: now,
			History:   []LifecycleEvent{{Status: StatusCreated, At: now}},
		},
		{
			TaskID:             "waiting",
			Command:            "echo later",
			Reasoning:          "test",
			Status:             StatusWaitingUserResponse,
			CreatedAt:          now,
			PendingInteraction: &interaction.Summary{InteractionID: "interaction-1", Kind: interaction.KindQuestion, Status: interaction.StatusPending, CreatedAt: now},
			History:            []LifecycleEvent{{Status: StatusCreated, At: now}},
		},
		{
			TaskID:    "running",
			Command:   "echo later",
			Reasoning: "test",
			Status:    StatusRunning,
			CreatedAt: now,
			History:   []LifecycleEvent{{Status: StatusCreated, At: now}},
		},
	}
	manager := NewManager(newMemoryTaskStore(records...), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	if err := manager.Recover(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"created", "running"} {
		recovered, ok := manager.Get(id)
		if !ok {
			t.Fatalf("missing recovered task %s", id)
		}
		if recovered.Status != StatusAbandoned {
			t.Fatalf("%s status = %s", id, recovered.Status)
		}
		if !IsFinalStatus(recovered.Status) {
			t.Fatalf("%s is not final", id)
		}
	}
	waiting, ok := manager.Get("waiting")
	if !ok {
		t.Fatal("missing recovered waiting task")
	}
	if waiting.Status != StatusWaitingUserResponse || waiting.PendingInteraction == nil {
		t.Fatalf("waiting = %#v", waiting)
	}
}

func waitForFinal(t *testing.T, manager *Manager, id string) *Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.Get(id)
		if ok && IsFinalStatus(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := manager.Get(id)
	t.Fatalf("task did not finish: %#v", task)
	return nil
}
