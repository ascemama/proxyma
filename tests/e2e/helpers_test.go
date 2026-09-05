package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/executor"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/jsonfile"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow"
	"proxyma/internal/tasks"
)

const quickSSHCommand = `ssh -o BatchMode=yes -o ConnectTimeout=1 {user}@127.0.0.1 hostname`
const quickPlaceholderSSHCommand = `ssh -o BatchMode=yes -o ConnectTimeout=1 {user}@127.0.0.1 'echo {password}'`

var testAgent = agents.Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "test-signing-key", Enabled: true}

type fakeE2ESSHClient struct {
	result sshclient.Result
}

func (f *fakeE2ESSHClient) Run(_ context.Context, _ sshclient.Request) sshclient.Result {
	if f.result.ExitStatus == nil && f.result.Error == "" && !f.result.TimedOut {
		code := 0
		f.result.ExitStatus = &code
	}
	return f.result
}

func newHarness(t *testing.T) (*api.Server, string) {
	t.Helper()
	taskDir := t.TempDir()
	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("demo-password")}})
	manager := tasks.NewManagerWithOptions(
		jsonfile.NewTaskStore(taskDir),
		executor.Executor{Timeout: time.Second, OutputLimit: 1 << 20},
		tasks.ManagerOptions{SSHClient: &fakeE2ESSHClient{result: sshclient.Result{Stdout: "test-host\n"}}},
	)
	ctx, cancel := context.WithCancel(context.Background())
	flow := taskflow.New(manager, nil, taskflow.Options{RequireCommandApproval: false})
	schedulerDone := taskflow.NewScheduler(flow, 10*time.Millisecond).Start(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-schedulerDone:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not stop")
		}
		manager.Wait()
	})
	return &api.Server{Agents: agents.NewRegistry([]agents.Agent{testAgent}), Targets: registry, Tasks: manager, CommandTimeout: time.Second, OutputLimit: 1 << 20, KnownHostsPath: "known_hosts"}, taskDir
}

func submitCommand(t *testing.T, server *api.Server, body string) (api.CommandCreatedResponse, int, time.Duration) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/commands", bytes.NewBufferString(body))
	authenticateRequest(t, req)
	rec := httptest.NewRecorder()
	start := time.Now()
	server.Routes().ServeHTTP(rec, req)
	elapsed := time.Since(start)
	var response api.CommandCreatedResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	return response, rec.Code, elapsed
}

func getTask(t *testing.T, server *api.Server, id string) (api.TaskStatusResponse, int, time.Duration) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/agent/tasks/"+id, nil)
	authenticateRequest(t, req)
	rec := httptest.NewRecorder()
	start := time.Now()
	server.Routes().ServeHTTP(rec, req)
	elapsed := time.Since(start)
	var task api.TaskStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	return task, rec.Code, elapsed
}

func authenticateRequest(t *testing.T, req *http.Request) {
	t.Helper()
	token, err := agents.IssueToken(testAgent, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func waitFinal(t *testing.T, server *api.Server, id string) api.TaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, code, elapsed := getTask(t, server, id)
		if code != http.StatusOK {
			t.Fatalf("status code = %d", code)
		}
		if elapsed > time.Second {
			t.Fatalf("status polling took %s", elapsed)
		}
		if tasks.IsFinalStatus(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not reach final state")
	return api.TaskStatusResponse{}
}
