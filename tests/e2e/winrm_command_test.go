package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/executor"
	"proxyma/internal/store/jsonfile"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow"
	"proxyma/internal/tasks"
	"proxyma/internal/winrmclient"
)

type fakeE2EWinRMClient struct {
	requests []winrmclient.Request
}

func (f *fakeE2EWinRMClient) Run(_ context.Context, request winrmclient.Request) winrmclient.Result {
	f.requests = append(f.requests, request)
	code := 0
	return winrmclient.Result{Stdout: "Running\n", ExitStatus: &code}
}

func TestSubmitAndPollWinRMCommand(t *testing.T) {
	fake := &fakeE2EWinRMClient{}
	target := targets.Target{
		Transport: targets.TransportWinRM, Hostname: "win.example", Username: "Administrator",
		AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret"),
		WinRM: &targets.WinRM{Scheme: "https", Port: 5986},
	}
	manager := tasks.NewManagerWithOptions(jsonfile.NewTaskStore(t.TempDir()), executor.Executor{}, tasks.ManagerOptions{WinRMClient: fake})
	ctx, cancel := context.WithCancel(context.Background())
	done := taskflow.NewScheduler(taskflow.New(manager, nil, taskflow.Options{RequireCommandApproval: false}), 10*time.Millisecond).Start(ctx)
	t.Cleanup(func() {
		cancel()
		<-done
		manager.Wait()
	})
	server := &api.Server{Agents: agents.NewRegistry([]agents.Agent{testAgent}), Targets: targets.NewRegistry([]targets.Target{target}), Tasks: manager, CommandTimeout: time.Second, OutputLimit: 1024}
	response, status, _ := submitCommand(t, server, `{"target":{"hostname":"win.example"},"command":"winrm {user}@win.example Get-Service","reasoning":"diagnostic"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	task := waitFinal(t, server, response.TaskID)
	if task.Result == nil || task.Result.Stdout != "Running\n" || len(fake.requests) != 1 || fake.requests[0].PowerShell != "Get-Service" {
		t.Fatalf("task=%#v requests=%#v", task, fake.requests)
	}
}
