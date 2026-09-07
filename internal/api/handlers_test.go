package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/sshclient"
	"proxyma/internal/store/jsonfile"
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

func TestSubmitCommandValidation(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{name: "missing command", body: `{"target":{"ip":"127.0.0.1"},"reasoning":"test"}`, status: http.StatusBadRequest},
		{name: "missing reasoning", body: `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname"}`, status: http.StatusBadRequest},
		{name: "missing target", body: `{"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`, status: http.StatusBadRequest},
		{name: "empty target", body: `{"target":{},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`, status: http.StatusBadRequest},
		{name: "unknown target", body: `{"target":{"ip":"192.0.2.10"},"command":"ssh {user}@192.0.2.10 hostname","reasoning":"test"}`, status: http.StatusNotFound},
	}
	server := testServer(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestResolveTarget(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "linux.example", IP: "10.0.0.10", Username: "linux", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		{Transport: targets.TransportSSH, OS: targets.OSWindows, Hostname: "win-ssh.example", IP: "10.0.0.11", Username: "windows", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		{Transport: targets.TransportWinRM, OS: targets.OSWindows, Hostname: "winrm.example", IP: "10.0.0.12", Username: "Administrator", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p"), WinRM: &targets.WinRM{Scheme: "https", Port: 5986}},
	})
	server := testServerWithRegistry(t, registry)

	cases := []struct {
		name        string
		path        string
		wantStatus  int
		wantPrefix  string
		wantPayload string
		wantOS      string
	}{
		{name: "ssh linux", path: "/v1/agent/targets/resolve?ip=10.0.0.10", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@10.0.0.10 ", wantPayload: "posix_shell", wantOS: targets.OSLinux},
		{name: "ssh windows", path: "/v1/agent/targets/resolve?hostname=win-ssh.example", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@win-ssh.example ", wantPayload: "windows_shell", wantOS: targets.OSWindows},
		{name: "winrm windows", path: "/v1/agent/targets/resolve?ip=10.0.0.12", wantStatus: http.StatusOK, wantPrefix: "winrm {user}@10.0.0.12 ", wantPayload: "powershell", wantOS: targets.OSWindows},
		{name: "matching hostname and ip", path: "/v1/agent/targets/resolve?hostname=linux.example&ip=10.0.0.10", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@10.0.0.10 ", wantPayload: "posix_shell", wantOS: targets.OSLinux},
		{name: "missing target", path: "/v1/agent/targets/resolve", wantStatus: http.StatusBadRequest},
		{name: "unknown target", path: "/v1/agent/targets/resolve?ip=192.0.2.10", wantStatus: http.StatusNotFound},
		{name: "hostname ip conflict", path: "/v1/agent/targets/resolve?hostname=linux.example&ip=10.0.0.11", wantStatus: http.StatusBadRequest},
		{name: "unsupported parameter", path: "/v1/agent/targets/resolve?target=linux.example", wantStatus: http.StatusBadRequest},
		{name: "repeated parameter", path: "/v1/agent/targets/resolve?ip=10.0.0.10&ip=10.0.0.10", wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newAuthenticatedRequest(t, http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var response ResolveTargetResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Target.OS != tc.wantOS || response.CanonicalCommand.Prefix != tc.wantPrefix || response.CanonicalCommand.PayloadKind != tc.wantPayload {
				t.Fatalf("response = %#v", response)
			}
			if strings.Contains(rec.Body.String(), "Administrator") || strings.Contains(rec.Body.String(), `"auth"`) || strings.Contains(rec.Body.String(), "5986") {
				t.Fatalf("response leaked unsafe target metadata: %s", rec.Body.String())
			}
		})
	}
}

func TestResolveTargetHidesUnauthorizedTargetsAndRejectsDuplicates(t *testing.T) {
	t.Run("unauthorized target", func(t *testing.T) {
		server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
			{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "linux.example", IP: "10.0.0.10", Username: "linux", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p")},
		}))
		req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?ip=10.0.0.10", nil)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		var unauthorized ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &unauthorized); err != nil {
			t.Fatal(err)
		}

		unknownReq := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?ip=192.0.2.55", nil)
		unknownRec := httptest.NewRecorder()
		server.Routes().ServeHTTP(unknownRec, unknownReq)
		if unknownRec.Code != http.StatusNotFound {
			t.Fatalf("unknown status = %d, want %d; body=%s", unknownRec.Code, http.StatusNotFound, unknownRec.Body.String())
		}
		var unknown ErrorResponse
		if err := json.Unmarshal(unknownRec.Body.Bytes(), &unknown); err != nil {
			t.Fatal(err)
		}
		if unauthorized != unknown {
			t.Fatalf("unauthorized response differs from unknown: unauthorized=%#v unknown=%#v", unauthorized, unknown)
		}
	})

	t.Run("duplicate authorized target", func(t *testing.T) {
		server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
			{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "dup.example", Username: "linux", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
			{Transport: targets.TransportSSH, OS: targets.OSWindows, IP: "dup.example", Username: "windows", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		}))
		req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?hostname=dup.example", nil)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
}

func TestAgentReadOnlyEndpointsRejectMutationMethods(t *testing.T) {
	server := testServer(t)
	server.Interactions = interaction.New(jsonfile.NewInteractionStore(t.TempDir()), interaction.Options{})
	paths := []string{
		"/v1/agent/targets/resolve?ip=127.0.0.1",
		"/v1/agent/tasks/missing",
		"/v1/agent/interactions/missing",
		"/v1/health",
	}
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, path := range paths {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				req := newAuthenticatedRequest(t, method, path, nil)
				rec := httptest.NewRecorder()
				server.Routes().ServeHTTP(rec, req)
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
				}
			})
		}
	}
}

func TestSubmitCanonicalSSHValidation(t *testing.T) {
	cases := []struct {
		name   string
		server *Server
		body   string
		status int
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:   "canonical command created with target ip",
			server: testServer(t),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name:   "canonical command created with target hostname",
			server: testServer(t),
			body:   `{"target":{"hostname":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name: "duplicate target match rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, IP: "127.0.0.1", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name: "duplicate target for different agent accepted",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, IP: "127.0.0.1", Username: "u2", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name: "target hostname and ip conflict rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "alpha.example", IP: "10.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, Hostname: "beta.example", IP: "10.0.0.2", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"hostname":"alpha.example","ip":"10.0.0.2"},"command":"ssh {user}@alpha.example hostname","reasoning":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name: "command target conflict rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "alpha.example", IP: "10.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, Hostname: "beta.example", IP: "10.0.0.2", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"hostname":"alpha.example"},"command":"ssh {user}@beta.example hostname","reasoning":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "helper syntax rejected",
			server: testServer(t),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"sshpass -p secret ssh demo@127.0.0.1 hostname","reasoning":"test"}`,
			status: http.StatusBadRequest,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var response ErrorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Code == "" || !response.Retryable || response.Hint == "" {
					t.Fatalf("unexpected transcriptor error payload: %#v", response)
				}
			},
		},
		{
			name:   "bare command rejected",
			server: testServer(t),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ls /home","reasoning":"test"}`,
			status: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			tc.server.Routes().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusAccepted {
				var response CommandCreatedResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if started, err := tc.server.Tasks.StartCreated(context.Background()); err != nil || started != 1 {
					t.Fatalf("started = %d, err = %v", started, err)
				}
				_ = waitHTTPStatus(t, tc.server, response.TaskID)
			}
			if tc.check != nil {
				tc.check(t, rec)
			}
			if strings.Contains(rec.Body.String(), "demo-password") || strings.Contains(rec.Body.String(), "secret") {
				t.Fatalf("response leaked credential: %s", rec.Body.String())
			}
		})
	}
}

func TestPrepareCommandUsesMatchedTargetAuthType(t *testing.T) {
	cases := []struct {
		name        string
		target      targets.Target
		wantAuth    string
		wantPass    string
		wantKeyPath string
	}{
		{
			name:     "password target",
			target:   targets.Target{Transport: targets.TransportSSH, Hostname: "target.example", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret")},
			wantAuth: sshclient.AuthTypePassword,
			wantPass: "secret",
		},
		{
			name:        "private key target",
			target:      targets.Target{Transport: targets.TransportSSH, Hostname: "target.example", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PrivateKeyAuth("secrets/private_keys/demo-key")},
			wantAuth:    sshclient.AuthTypePrivateKey,
			wantKeyPath: "secrets/private_keys/demo-key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{tc.target}))
			request := tasks.CommandContext{
				Target:    &tasks.Target{Hostname: "target.example"},
				Command:   "ssh {user}@target.example hostname",
				Reasoning: "test",
			}

			if err := server.prepareCommand(&request, testAgent); err != nil {
				t.Fatal(err)
			}
			if request.ExecutionPlan == nil || request.ExecutionPlan.SSHRequest == nil {
				t.Fatalf("execution plan = %#v", request.ExecutionPlan)
			}
			sshRequest := request.ExecutionPlan.SSHRequest
			if sshRequest.AuthType != tc.wantAuth || sshRequest.Password != tc.wantPass || sshRequest.PrivateKeyPath != tc.wantKeyPath {
				t.Fatalf("ssh request auth = type %q password %q key %q", sshRequest.AuthType, sshRequest.Password, sshRequest.PrivateKeyPath)
			}
			if sshRequest.Username != "demo" || sshRequest.RemoteCommand != "hostname" {
				t.Fatalf("ssh request = %#v", sshRequest)
			}
		})
	}
}

func TestPrepareCommandBuildsWinRMPlan(t *testing.T) {
	target := targets.Target{
		Transport: targets.TransportWinRM, OS: targets.OSWindows, Hostname: "win.example", Username: "Administrator",
		AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret"),
		WinRM: &targets.WinRM{Scheme: "https", Port: 5986},
	}
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{target}))
	request := tasks.CommandContext{
		Target: &tasks.Target{Hostname: "win.example"}, Command: `winrm {user}@win.example Get-ChildItem "C:\Program Files"`, Reasoning: "test",
	}
	if err := server.prepareCommand(&request, testAgent); err != nil {
		t.Fatal(err)
	}
	if request.ExecutionPlan == nil || request.ExecutionPlan.WinRMRequest == nil {
		t.Fatalf("execution plan = %#v", request.ExecutionPlan)
	}
	got := request.ExecutionPlan.WinRMRequest
	if got.Scheme != "https" || got.Port != 5986 || got.Username != "Administrator" || got.Password != "secret" || got.PowerShell != `Get-ChildItem "C:\Program Files"` {
		t.Fatalf("WinRM request = %#v", got)
	}
	if request.Target == nil || request.Target.OS != targets.OSWindows {
		t.Fatalf("prepared target = %#v", request.Target)
	}
}

func TestPrepareCommandRejectsPasswordPlaceholderForPrivateKeyTarget(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
		{Transport: targets.TransportSSH, Hostname: "target.example", Username: "demo", AgentIDs: []string{testAgent.ID}, Auth: targets.PrivateKeyAuth("secrets/private_keys/demo-key")},
	}))
	request := tasks.CommandContext{
		Target:    &tasks.Target{Hostname: "target.example"},
		Command:   "ssh {user}@target.example echo {password}",
		Reasoning: "test",
	}

	err := server.prepareCommand(&request, testAgent)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "matched target cannot resolve {password}") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestSubmitCommandCreated(t *testing.T) {
	server := testServer(t)
	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	start := time.Now()
	server.Routes().ServeHTTP(rec, req)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("submit took %s", elapsed)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var response CommandCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.TaskID == "" || response.Status != tasks.StatusCreated {
		t.Fatalf("response = %#v", response)
	}
	task, ok := server.Tasks.Get(response.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if task.Status != tasks.StatusCreated {
		t.Fatalf("stored status = %s", task.Status)
	}
	if task.AgentID != testAgent.ID {
		t.Fatalf("stored agent id = %q", task.AgentID)
	}
	if started, err := server.Tasks.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	_ = waitHTTPStatus(t, server, response.TaskID)
}

func TestSubmitCommandIdempotencyReplayAndConflict(t *testing.T) {
	server := testServer(t)
	operationID := "c7b2de27-a00f-4c6c-bb90-b4cc80d1628f"
	body := `{"client_operation_id":"` + operationID + `","target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`
	submit := func(payload string) (int, CommandCreatedResponse, ErrorResponse) {
		req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		var created CommandCreatedResponse
		var apiErr ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
		return rec.Code, created, apiErr
	}
	status, first, _ := submit(body)
	if status != http.StatusAccepted || first.TaskID == "" || first.Replayed || first.ClientOperationID != operationID {
		t.Fatalf("first status=%d response=%#v", status, first)
	}
	statusRequest := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/tasks/"+first.TaskID, nil)
	statusRecorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(statusRecorder, statusRequest)
	var taskStatus TaskStatusResponse
	if statusRecorder.Code != http.StatusOK || json.Unmarshal(statusRecorder.Body.Bytes(), &taskStatus) != nil || taskStatus.ClientOperationID != operationID {
		t.Fatalf("task status code=%d response=%#v body=%s", statusRecorder.Code, taskStatus, statusRecorder.Body.String())
	}
	server.Targets = targets.NewRegistry(nil)
	status, replay, _ := submit(body)
	if status != http.StatusAccepted || replay.TaskID != first.TaskID || !replay.Replayed {
		t.Fatalf("replay status=%d response=%#v", status, replay)
	}
	conflictBody := strings.Replace(body, `"reasoning":"test"`, `"reasoning":"different"`, 1)
	status, _, apiErr := submit(conflictBody)
	if status != http.StatusConflict || apiErr.Code != "idempotency_conflict" {
		t.Fatalf("conflict status=%d error=%#v", status, apiErr)
	}
}

func TestSubmitCommandRejectsInvalidOperationID(t *testing.T) {
	server := testServer(t)
	body := `{"client_operation_id":"not-a-uuid","target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_client_operation_id") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDebugLogsRequestAndCreatedCommand(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Debug = true
	server.Logger = log.New(&logs, "", 0)

	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","requested_by":"codex","metadata":{"b":2,"a":1}}`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var response CommandCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if started, err := server.Tasks.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	_ = waitHTTPStatus(t, server, response.TaskID)

	output := logs.String()
	for _, want := range []string{
		`debug command request body={"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","requested_by":"codex","metadata":{"b":2,"a":1}}`,
		"debug command created",
		"target=127.0.0.1",
		"command_family=ssh_remote_command",
		"strategy=go_ssh_client",
		"requested_by=\"codex\"",
		"metadata_keys=a,b",
		"debug http request method=POST path=/v1/agent/commands",
		"status=202",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing %q in:\n%s", want, output)
		}
	}
}

func TestDebugLogsMalformedJSONBody(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Debug = true
	server.Logger = log.New(&logs, "", 0)

	body := bytes.NewBufferString(`{"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}

	output := logs.String()
	if !strings.Contains(output, `debug command request body={"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"`) {
		t.Fatalf("debug log missing malformed body in:\n%s", output)
	}
	if strings.Contains(output, "debug command created") {
		t.Fatalf("unexpected created-command log for malformed JSON:\n%s", output)
	}
}

func TestDebugLogsCommandRejectionReason(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Debug = true
	server.Logger = log.New(&logs, "", 0)

	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ls /home","reasoning":"test"}`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}

	output := logs.String()
	if !strings.Contains(output, "debug command request rejected status=400") {
		t.Fatalf("debug log missing rejection reason in:\n%s", output)
	}
}

func TestDebugDisabledDoesNotLogRequests(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Logger = log.New(&logs, "", 0)

	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if logs.Len() != 0 {
		t.Fatalf("unexpected logs: %s", logs.String())
	}
}

func TestHealth(t *testing.T) {
	server := testServer(t)
	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var response HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Capabilities) != 1 || response.Capabilities[0] != agentCommandIdempotencyCapability {
		t.Fatalf("capabilities=%#v", response.Capabilities)
	}
}

func TestSubmitCommandRejectsUnauthorizedAgentForTarget(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
		{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "u", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p")},
	}))
	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	registry := targets.NewRegistry([]targets.Target{{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "u", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")}})
	return testServerWithRegistry(t, registry)
}

func testServerWithRegistry(t *testing.T, registry targets.Registry) *Server {
	t.Helper()
	fake := &fakeSSHClient{result: sshclient.Result{Stdout: "ok\n"}}
	manager := tasks.NewManagerWithOptions(
		jsonfile.NewTaskStore(t.TempDir()),
		executor.Executor{Timeout: time.Second, OutputLimit: 1024},
		tasks.ManagerOptions{SSHClient: fake},
	)
	return &Server{
		Agents:         testAgentRegistry(),
		Users:          testUserRegistry(),
		UserSessions:   users.NewSessionStore(t.TempDir()),
		BuildInfo:      BuildInfoResponse{Version: "1.2.3+4.gabcdef0", Commit: "abcdef0"},
		Targets:        registry,
		Tasks:          manager,
		CommandTimeout: time.Second,
		OutputLimit:    1024,
		KnownHostsPath: "known_hosts",
	}
}
