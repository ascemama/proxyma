package api

import (
	"context"
	"testing"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/sqlite"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"
)

type fakeSSHClient struct {
	result   sshclient.Result
	requests []sshclient.Request
}

type fakeWinRMClient struct {
	result   winrmclient.Result
	requests []winrmclient.Request
}

func (f *fakeWinRMClient) Run(_ context.Context, request winrmclient.Request) winrmclient.Result {
	f.requests = append(f.requests, request)
	return f.result
}

func (f *fakeSSHClient) Run(_ context.Context, request sshclient.Request) sshclient.Result {
	f.requests = append(f.requests, request)
	if f.result.ExitStatus == nil && !f.result.TimedOut && f.result.Error == "" {
		code := 0
		f.result.ExitStatus = &code
	}
	return f.result
}

func testServer(t *testing.T) *Server {
	t.Helper()
	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "u", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")}})
	return testServerWithRegistry(t, registry)
}

func testServerWithRegistry(t *testing.T, registry targets.Registry) *Server {
	t.Helper()
	fake := &fakeSSHClient{result: sshclient.Result{Stdout: "ok\n"}}
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	manager := tasks.NewManagerWithOptions(
		database.NewCommandStore(),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		tasks.ManagerOptions{SSHClient: fake},
	)
	return &Server{
		Agents:             testAgentRegistry(),
		Users:              testUserRegistry(),
		UserSessions:       users.NewSessionStore(t.TempDir()),
		BuildInfo:          BuildInfoResponse{Version: "1.2.3+4.gabcdef0", Commit: "abcdef0"},
		Targets:            registry,
		Tasks:              manager,
		CommandTimeout:     time.Second,
		OutputLimit:        1024,
		KnownHostsPath:     "known_hosts",
		InteractionHistory: database.NewHistoryStore(),
	}
}
