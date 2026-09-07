package e2e

import (
	"net/http"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/api"
	"proxyma/internal/executor"
	"proxyma/internal/sshclient"
	"proxyma/internal/store"
	"proxyma/internal/store/jsonfile"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
)

func TestCompletedTaskSurvivesRestart(t *testing.T) {
	server, taskDir := newHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"diagnostic"}`)
	if code != http.StatusAccepted {
		t.Fatalf("submit code = %d", code)
	}
	_ = waitFinal(t, server, response.TaskID)

	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("demo-password")}})
	manager := tasks.NewManagerWithOptions(
		jsonfile.NewTaskStore(taskDir),
		executor.Executor{Timeout: time.Second, OutputLimit: 1 << 20},
		tasks.ManagerOptions{SSHClient: &fakeE2ESSHClient{result: sshclient.Result{Stdout: "test-host\n"}}},
	)
	if err := manager.Recover(); err != nil {
		t.Fatal(err)
	}
	restarted := &api.Server{Agents: agents.NewRegistry([]agents.Agent{testAgent}), Targets: registry, Tasks: manager, CommandTimeout: time.Second, OutputLimit: 1 << 20, KnownHostsPath: "known_hosts"}
	task, status, _ := getTask(t, restarted, response.TaskID)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !tasks.IsFinalStatus(task.Status) || task.Result == nil {
		t.Fatalf("task = %#v", task)
	}
}

func TestNonFinalTasksBecomeAbandonedAfterRestart(t *testing.T) {
	_, taskDir := newHarness(t)
	fileStore := store.New(taskDir)
	now := time.Now().UTC()
	task := tasks.Task{
		TaskID:    "created-task",
		AgentID:   testAgent.ID,
		Command:   quickSSHCommand,
		Reasoning: "diagnostic",
		Status:    tasks.StatusCreated,
		CreatedAt: now,
		History:   []tasks.LifecycleEvent{{Status: tasks.StatusCreated, At: now, Message: "task created"}},
	}
	if err := fileStore.WriteJSON(task.TaskID, task); err != nil {
		t.Fatal(err)
	}

	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("demo-password")}})
	manager := tasks.NewManagerWithOptions(
		jsonfile.WrapTaskStore(fileStore),
		executor.Executor{Timeout: time.Second, OutputLimit: 1 << 20},
		tasks.ManagerOptions{SSHClient: &fakeE2ESSHClient{result: sshclient.Result{Stdout: "test-host\n"}}},
	)
	if err := manager.Recover(); err != nil {
		t.Fatal(err)
	}
	restarted := &api.Server{Agents: agents.NewRegistry([]agents.Agent{testAgent}), Targets: registry, Tasks: manager, CommandTimeout: time.Second, OutputLimit: 1 << 20, KnownHostsPath: "known_hosts"}
	recovered, status, _ := getTask(t, restarted, task.TaskID)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if recovered.Status != tasks.StatusAbandoned {
		t.Fatalf("task = %#v", recovered)
	}
}
