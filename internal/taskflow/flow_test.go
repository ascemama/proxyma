package taskflow

import (
	"context"
	"testing"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/store/jsonfile"
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
	if provider.sent[0].Metadata["policy_id"] != "baseline" || provider.sent[0].Metadata["policy_rule_id"] != "approval-default" || provider.sent[0].Metadata["policy_reason"] != "needs approval" {
		t.Fatalf("metadata = %#v", provider.sent[0].Metadata)
	}
}

func newFlowHarness(t *testing.T, providerEnabled bool) (*tasks.Manager, *interaction.Service, *recordingProvider) {
	t.Helper()
	provider := &recordingProvider{}
	var configured interaction.Provider
	if providerEnabled {
		configured = provider
	}
	communicator := interaction.New(jsonfile.NewInteractionStore(t.TempDir()), interaction.Options{Provider: configured, Timeout: time.Hour})
	manager := tasks.NewManager(jsonfile.NewTaskStore(t.TempDir()), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	return manager, communicator, provider
}

func submitTask(t *testing.T, manager *tasks.Manager) *tasks.Task {
	t.Helper()
	task, err := manager.Submit(context.Background(), tasks.CommandContext{Command: "echo done", Reasoning: "test", RequestedBy: "agent-demo", InteractionInstanceID: "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"})
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
		RequestedBy: "agent-demo",
		Target:      &tasks.Target{Hostname: "target.example", PolicyIDs: []string{"baseline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func testPolicySet() *policy.Set {
	return policy.NewSet(map[string]policy.Policy{
		"baseline": {
			ID:       "baseline",
			Priority: 10,
			Rules: []policy.Rule{
				{ID: "deny-dangerous", Effect: policy.DecisionDeny, RemoteCommandRegex: `rm\s+-rf`, Reason: "dangerous"},
				{ID: "allow-hostname", Effect: policy.DecisionAllow, RemoteCommandPrefixAny: []string{"hostname"}, Reason: "readonly"},
				{ID: "approval-default", Effect: policy.DecisionApproval, Always: true, Reason: "needs approval"},
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
