package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	interactionntfy "proxyma/internal/interaction/ntfy"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/sqlite"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow"
	"proxyma/internal/tasks"
)

type fakeNtfy struct {
	server *httptest.Server
	mu     sync.Mutex
	lines  []string
	posts  []map[string]any
	nextID int
}

func newFakeNtfy(t *testing.T) *fakeNtfy {
	t.Helper()
	fake := &fakeNtfy{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			fake.posts = append(fake.posts, payload)
			fake.nextID++
			id := fmt.Sprintf("published-%d", fake.nextID)
			fake.mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"id":%q,"event":"message"}`, id)
		case r.Method == http.MethodGet && r.URL.Path == "/responses-secret/json":
			fake.mu.Lock()
			lines := append([]string(nil), fake.lines...)
			fake.lines = nil
			fake.mu.Unlock()
			for _, line := range lines {
				_, _ = fmt.Fprintln(w, line)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeNtfy) addResponse(interactionID string, approved bool) {
	response, _ := json.Marshal(map[string]any{
		"interaction_id": interactionID,
		"approved":       approved,
	})
	event, _ := json.Marshal(map[string]any{
		"id":      fmt.Sprintf("response-%d", time.Now().UnixNano()),
		"time":    time.Now().UTC().Unix(),
		"event":   "message",
		"topic":   "responses-secret",
		"message": string(response),
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, string(event))
}

func (f *fakeNtfy) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func TestNtfyApprovalRunsCommand(t *testing.T) {
	server, ntfyServer := newNtfyHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"test ntfy approval","harness_name":"test"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	waitForNtfyPost(t, ntfyServer)
	waiting := waitTaskStatus(t, server, response.TaskID, tasks.StatusWaitingUserResponse)
	if waiting.PendingInteraction == nil {
		t.Fatalf("pending interaction missing: %#v", waiting)
	}

	ntfyServer.addResponse(waiting.PendingInteraction.InteractionID, true)

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

func TestNtfyRejectionBlocksCommand(t *testing.T) {
	server, ntfyServer := newNtfyHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"test ntfy rejection","harness_name":"test"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	waitForNtfyPost(t, ntfyServer)
	waiting := waitTaskStatus(t, server, response.TaskID, tasks.StatusWaitingUserResponse)
	if waiting.PendingInteraction == nil {
		t.Fatalf("pending interaction missing: %#v", waiting)
	}

	ntfyServer.addResponse(waiting.PendingInteraction.InteractionID, false)

	final := waitFinal(t, server, response.TaskID)
	if final.Status != tasks.StatusBlocked {
		t.Fatalf("status = %s", final.Status)
	}
}

func newNtfyHarness(t *testing.T) (*api.Server, *fakeNtfy) {
	t.Helper()
	ntfyServer := newFakeNtfy(t)
	provider, err := interactionntfy.New(interactionntfy.Options{
		BaseURL:       ntfyServer.server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		PollInterval:  10 * time.Millisecond,
		PollTimeout:   time.Second,
		HTTPClient:    ntfyServer.server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	communicator := interaction.New(database.NewInteractionStore(), interaction.Options{Provider: provider, Timeout: time.Hour})
	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("demo-password")}})
	manager := tasks.NewManagerWithOptions(
		database.NewCommandStore(),
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
	}, ntfyServer
}

func waitForNtfyPost(t *testing.T, ntfyServer *fakeNtfy) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ntfyServer.postCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for ntfy publish")
}
