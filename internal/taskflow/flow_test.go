package taskflow

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/store/sqlite"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
)

type recordingProvider struct {
	sent []interaction.Interaction
}

func (p *recordingProvider) Send(_ interaction.Context, request interaction.Interaction, _ interaction.Responder) (*interaction.Delivery, error) {
	p.sent = append(p.sent, request)
	return &interaction.Delivery{Provider: "test", Reference: request.InteractionID, DeliveredAt: request.CreatedAt}, nil
}

func (p *recordingProvider) Cancel(_ interaction.Context, _ interaction.Interaction) error {
	return nil
}

func TestFlowRequestsApprovalForCreatedTask(t *testing.T) {
	manager, communicator, provider := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: true})

	started, err := flow.StartCreated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started != 0 {
		t.Fatalf("started = %d", started)
	}
	waiting, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if waiting.Status != tasks.StatusWaitingUserResponse || waiting.PendingInteraction == nil {
		t.Fatalf("waiting = %#v", waiting)
	}
	if waiting.Approval == nil || waiting.Approval.Status != tasks.ApprovalStatusPending {
		t.Fatalf("approval = %#v", waiting.Approval)
	}
	if len(provider.sent) != 1 || provider.sent[0].Metadata["command"] != "echo done" {
		t.Fatalf("sent = %#v", provider.sent)
	}
	if provider.sent[0].InteractionInstanceID != "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8" {
		t.Fatalf("InteractionInstanceID = %q", provider.sent[0].InteractionInstanceID)
	}
}

func TestFlowDoesNotStartApprovalTaskAcrossRepeatedPasses(t *testing.T) {
	manager, communicator, provider := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: true})

	if started, err := flow.StartCreated(context.Background()); err != nil || started != 0 {
		t.Fatalf("initial started = %d err=%v", started, err)
	}
	for range 3 {
		if started, err := flow.StartCreated(context.Background()); err != nil || started != 0 {
			t.Fatalf("repeated started = %d err=%v", started, err)
		}
		if _, err := flow.ManageState(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	waiting, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if waiting.Status != tasks.StatusWaitingUserResponse || waiting.Approval == nil || waiting.Approval.Status != tasks.ApprovalStatusPending {
		t.Fatalf("waiting = %#v", waiting)
	}
	if len(provider.sent) != 1 {
		t.Fatalf("approval requests = %#v", provider.sent)
	}
}

func TestFlowStartsApprovedTaskOnce(t *testing.T) {
	manager, communicator, _ := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: true})
	if _, err := flow.StartCreated(context.Background()); err != nil {
		t.Fatal(err)
	}
	waiting, _ := manager.Get(task.TaskID)
	approved := true
	if _, err := communicator.Respond(context.Background(), interaction.Response{InteractionID: waiting.PendingInteraction.InteractionID, Approved: &approved}); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.ManageState(context.Background()); err != nil {
		t.Fatal(err)
	}
	final := waitFinal(t, manager, task.TaskID)
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if final.Approval == nil || final.Approval.Status != tasks.ApprovalStatusApproved {
		t.Fatalf("approval = %#v", final.Approval)
	}
	if started, err := flow.StartCreated(context.Background()); err != nil || started != 0 {
		t.Fatalf("started = %d err=%v", started, err)
	}
}

func TestFlowRejectedApprovalBlocksTask(t *testing.T) {
	manager, communicator, _ := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: true})
	if _, err := flow.StartCreated(context.Background()); err != nil {
		t.Fatal(err)
	}
	waiting, _ := manager.Get(task.TaskID)
	approved := false
	if _, err := communicator.Respond(context.Background(), interaction.Response{InteractionID: waiting.PendingInteraction.InteractionID, Approved: &approved}); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.ManageState(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked, _ := manager.Get(task.TaskID)
	if blocked.Status != tasks.StatusBlocked {
		t.Fatalf("status = %s", blocked.Status)
	}
	if blocked.Approval == nil || blocked.Approval.Status != tasks.ApprovalStatusRejected {
		t.Fatalf("approval = %#v", blocked.Approval)
	}
}

func TestFlowCanceledApprovalBlocksTask(t *testing.T) {
	manager, communicator, _ := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: true})
	if _, err := flow.StartCreated(context.Background()); err != nil {
		t.Fatal(err)
	}
	waiting, _ := manager.Get(task.TaskID)
	if _, err := communicator.Cancel(context.Background(), waiting.PendingInteraction.InteractionID, "local console input closed"); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.ManageState(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked, _ := manager.Get(task.TaskID)
	if blocked.Status != tasks.StatusBlocked || blocked.Result == nil || blocked.Result.Error != "local console input closed" {
		t.Fatalf("blocked = %#v", blocked)
	}
	if blocked.Approval == nil || blocked.Approval.Status != tasks.ApprovalStatusCanceled {
		t.Fatalf("approval = %#v", blocked.Approval)
	}
}

func TestFlowStartsDirectlyWhenApprovalDisabled(t *testing.T) {
	manager, communicator, provider := newFlowHarness(t, true)
	task := submitTask(t, manager)
	flow := New(manager, communicator, Options{RequireCommandApproval: false})
	started, err := flow.StartCreated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("started = %d", started)
	}
	final := waitFinal(t, manager, task.TaskID)
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if len(provider.sent) != 0 {
		t.Fatalf("sent = %#v", provider.sent)
	}
}

func TestFlowStartsAllowedPolicyTaskDirectly(t *testing.T) {
	manager, communicator, provider := newFlowHarness(t, true)
	task := submitPolicyTask(t, manager, "hostname")
	flow := New(manager, communicator, Options{Policies: testPolicySet()})

	started, err := flow.StartCreated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("started = %d", started)
	}
	final := waitFinal(t, manager, task.TaskID)
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if final.Policy == nil || final.Policy.Decision != policy.DecisionAllow || final.Policy.RuleID != "allow-hostname" {
		t.Fatalf("policy = %#v", final.Policy)
	}
	if len(provider.sent) != 0 {
		t.Fatalf("sent = %#v", provider.sent)
	}
}

func TestFlowBlocksDeniedPolicyTaskWithoutInteraction(t *testing.T) {
	manager, _, provider := newFlowHarness(t, true)
	task := submitPolicyTask(t, manager, "rm -rf /tmp/demo")
	flow := New(manager, nil, Options{Policies: testPolicySet()})

	started, err := flow.StartCreated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started != 0 {
		t.Fatalf("started = %d", started)
	}
	blocked, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if blocked.Status != tasks.StatusBlocked || blocked.Result == nil || blocked.Result.Error != "dangerous" {
		t.Fatalf("blocked = %#v", blocked)
	}
	if blocked.Policy == nil || blocked.Policy.Decision != policy.DecisionDeny {
		t.Fatalf("policy = %#v", blocked.Policy)
	}
	if len(provider.sent) != 0 {
		t.Fatalf("sent = %#v", provider.sent)
	}
}

func TestFlowApprovalPolicyIncludesMetadata(t *testing.T) {
	manager, communicator, provider := newFlowHarness(t, true)
	task := submitPolicyTask(t, manager, "systemctl status ssh")
	flow := New(manager, communicator, Options{Policies: testPolicySet()})

	started, err := flow.StartCreated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if started != 0 {
		t.Fatalf("started = %d", started)
	}
	waiting, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if waiting.Status != tasks.StatusWaitingUserResponse || waiting.Policy == nil || waiting.Policy.Decision != policy.DecisionApproval {
		t.Fatalf("waiting = %#v", waiting)
	}
	if len(provider.sent) != 1 {
		t.Fatalf("sent = %#v", provider.sent)
	}
	if provider.sent[0].Metadata["policy_id"] != "" || provider.sent[0].Metadata["policy_rule_id"] != "" || provider.sent[0].Metadata["policy_reason"] != "no policy rule matched command" || provider.sent[0].Metadata["policy_justification"] != "" {
		t.Fatalf("metadata = %#v", provider.sent[0].Metadata)
	}
}

func TestFlowLogsPolicyDecisionsWithoutCommandContent(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		decision string
		policyID string
		ruleID   string
	}{
		{name: "allow", command: "hostname", decision: policy.DecisionAllow, policyID: "baseline", ruleID: "allow-hostname"},
		{name: "deny", command: "rm -rf /tmp/demo", decision: policy.DecisionDeny, policyID: "baseline", ruleID: "deny-dangerous"},
		{name: "approval", command: "systemctl status ssh", decision: policy.DecisionApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			manager, communicator, _ := newFlowHarness(t, true)
			task := submitPolicyTask(t, manager, tt.command)
			flow := New(manager, communicator, Options{Policies: testPolicySet(), Debug: true, Logger: log.New(&logs, "", 0)})

			if _, err := flow.StartCreated(context.Background()); err != nil {
				t.Fatal(err)
			}
			output := logs.String()
			for _, field := range []string{"policy decision=" + tt.decision, "task_id=" + task.TaskID} {
				if !strings.Contains(output, field) {
					t.Fatalf("log output %q does not contain %q", output, field)
				}
			}
			if tt.policyID != "" && (!strings.Contains(output, "policy_id="+tt.policyID) || !strings.Contains(output, "rule_id="+tt.ruleID)) {
				t.Fatalf("log output %q does not identify matching policy rule", output)
			}
			if tt.name != "allow" && strings.Contains(output, tt.command) {
				t.Fatalf("log output contains command: %q", output)
			}
			for _, stage := range []string{"blacklist", "whitelist", "llm"} {
				if !strings.Contains(output, "policy step="+stage) {
					t.Fatalf("log output %q does not contain %s policy step", output, stage)
				}
			}
			if tt.name == "allow" && !strings.Contains(output, "policy step=blacklist result=not_matched") ||
				tt.name == "deny" && !strings.Contains(output, "policy step=blacklist result=matched") ||
				tt.name == "approval" && !strings.Contains(output, "policy step=blacklist result=not_matched") {
				t.Fatalf("log output has unexpected blacklist result: %q", output)
			}
			if tt.name == "allow" && !strings.Contains(output, "policy step=whitelist result=matched") ||
				tt.name != "allow" && !strings.Contains(output, "policy step=whitelist result=not_matched") {
				t.Fatalf("log output has unexpected whitelist result: %q", output)
			}
			if !strings.Contains(output, "policy step=llm result=skipped") {
				t.Fatalf("log output has unexpected LLM result: %q", output)
			}
		})
	}
}

func TestFlowDoesNotLogPersistedPolicyDecisionTwice(t *testing.T) {
	var logs bytes.Buffer
	manager, communicator, _ := newFlowHarness(t, true)
	task := submitPolicyTask(t, manager, "hostname")
	flow := New(manager, communicator, Options{Policies: testPolicySet(), Logger: log.New(&logs, "", 0)})

	if _, err := flow.StartCreated(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstLog := logs.String()
	if count := strings.Count(firstLog, "policy decision=allow"); count != 1 {
		t.Fatalf("allow log count after first pass = %d, output=%q", count, firstLog)
	}
	if _, err := flow.StartCreated(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(logs.String(), "policy decision=allow"); count != 1 {
		t.Fatalf("allow log count after repeated pass = %d, output=%q", count, logs.String())
	}
	if task == nil {
		t.Fatal("missing submitted task")
	}
}

func newFlowHarness(t *testing.T, providerEnabled bool) (*tasks.Manager, *interaction.Service, *recordingProvider) {
	t.Helper()
	provider := &recordingProvider{}
	var configured interaction.Provider
	if providerEnabled {
		configured = provider
	}
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	communicator := interaction.New(database.NewInteractionStore(), interaction.Options{Provider: configured, Timeout: time.Hour})
	manager := tasks.NewManager(database.NewCommandStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	return manager, communicator, provider
}

func submitTask(t *testing.T, manager *tasks.Manager) *tasks.Task {
	t.Helper()
	task, err := manager.Submit(context.Background(), tasks.CommandContext{Command: "echo done", Reasoning: "test", HarnessName: "agent-demo", InteractionInstanceID: "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func submitPolicyTask(t *testing.T, manager *tasks.Manager, command string) *tasks.Task {
	t.Helper()
	task, err := manager.Submit(context.Background(), tasks.CommandContext{
		Command:     command,
		Reasoning:   "test",
		HarnessName: "agent-demo",
		Target:      &tasks.Target{Hostname: "target.example", PolicyID: "baseline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func testPolicySet() *policy.Set {
	return policy.NewSet(map[string]policy.Policy{
		"baseline": {
			ID:      "baseline",
			Enabled: true,
			Rules: []policy.Rule{
				{ID: "deny-dangerous", Effect: policy.DecisionDeny, CommandTemplate: "rm -rf {path}", Reason: "dangerous"},
				{ID: "allow-hostname", Effect: policy.DecisionAllow, CommandTemplate: "hostname", Reason: "readonly"},
			},
		},
	})
}

func waitFinal(t *testing.T, manager *tasks.Manager, id string) *tasks.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := manager.Get(id)
		if ok && tasks.IsFinalStatus(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := manager.Get(id)
	t.Fatalf("task did not finish: %#v", task)
	return nil
}
