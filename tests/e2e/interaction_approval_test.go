package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/jsonfile"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow"
	"proxyma/internal/tasks"
)

type approvalProvider struct {
	sent      []interaction.Interaction
	responder interaction.Responder
}

func (p *approvalProvider) Send(_ interaction.Context, request interaction.Interaction, responder interaction.Responder) (*interaction.Delivery, error) {
	p.sent = append(p.sent, request)
	p.responder = responder
	return &interaction.Delivery{Provider: "test", Reference: request.InteractionID, DeliveredAt: request.CreatedAt}, nil
}

func (p *approvalProvider) Cancel(_ interaction.Context, _ interaction.Interaction) error {
	return nil
}

func TestCommandWaitsForApprovalThenRuns(t *testing.T) {
	server, provider := newApprovalHarness(t)
	response, code, elapsed := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"test approval"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if elapsed > time.Second {
		t.Fatalf("submit took %s", elapsed)
	}

	waiting := waitTaskStatus(t, server, response.TaskID, tasks.StatusWaitingUserResponse)
	if waiting.PendingInteraction == nil {
		t.Fatalf("pending interaction missing: %#v", waiting)
	}
	if len(provider.sent) != 1 {
		t.Fatalf("provider sent = %#v", provider.sent)
	}

	approved := true
	if _, err := provider.responder.Respond(context.Background(), interaction.Response{
		InteractionID: waiting.PendingInteraction.InteractionID,
		Approved:      &approved,
		RespondedBy:   "test-operator",
	}); err != nil {
		t.Fatal(err)
	}

	final := waitFinal(t, server, response.TaskID)
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if final.Approval == nil || final.Approval.Status != tasks.ApprovalStatusApproved {
		t.Fatalf("approval = %#v", final.Approval)
	}
	if !historyContainsStatus(final.History, tasks.StatusAcceptedByUser) {
		t.Fatalf("history missing %s: %#v", tasks.StatusAcceptedByUser, final.History)
	}
}

func TestRejectedCommandApprovalBlocksTask(t *testing.T) {
	server, provider := newApprovalHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"test rejection"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	waiting := waitTaskStatus(t, server, response.TaskID, tasks.StatusWaitingUserResponse)
	rejected := false
	if _, err := provider.responder.Respond(context.Background(), interaction.Response{
		InteractionID: waiting.PendingInteraction.InteractionID,
		Approved:      &rejected,
		RespondedBy:   "test-operator",
	}); err != nil {
		t.Fatal(err)
	}
	final := waitFinal(t, server, response.TaskID)
	if final.Status != tasks.StatusBlocked {
		t.Fatalf("status = %s", final.Status)
	}
}

func newApprovalHarness(t *testing.T) (*api.Server, *approvalProvider) {
	t.Helper()
	provider := &approvalProvider{}
	communicator := interaction.New(jsonfile.NewInteractionStore(t.TempDir()), interaction.Options{Provider: provider, Timeout: time.Hour})
	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("demo-password")}})
	manager := tasks.NewManagerWithOptions(
		jsonfile.NewTaskStore(t.TempDir()),
		executor.Executor{Timeout: time.Second, OutputLimit: 1 << 20},
		tasks.ManagerOptions{SSHClient: &fakeE2ESSHClient{result: sshclient.Result{Stdout: "test-host\n"}}},
	)
	ctx, cancel := context.WithCancel(context.Background())
	flow := taskflow.New(manager, communicator, taskflow.Options{RequireCommandApproval: true})
	schedulerDone := taskflow.NewScheduler(flow, 10*time.Millisecond).Start(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-schedulerDone:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not stop")
		}
	})
	return &api.Server{
		Agents:         agents.NewRegistry([]agents.Agent{testAgent}),
		Targets:        registry,
		Tasks:          manager,
		Interactions:   communicator,
		CommandTimeout: time.Second,
		OutputLimit:    1 << 20,
		KnownHostsPath: "known_hosts",
	}, provider
}

func waitTaskStatus(t *testing.T, server *api.Server, id string, status string) api.TaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, code, _ := getTask(t, server, id)
		if code != http.StatusOK {
			t.Fatalf("status code = %d", code)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _, _ := getTask(t, server, id)
	body, _ := json.Marshal(task)
	t.Fatalf("task did not reach %s: %s", status, body)
	return api.TaskStatusResponse{}
}

func historyContainsStatus(history []api.TaskHistoryEventResponse, status string) bool {
	for _, event := range history {
		if event.Status == status {
			return true
		}
	}
	return false
}
