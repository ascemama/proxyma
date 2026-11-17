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

	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
)

func TestSubmitCommandValidation(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{name: "missing command", body: `{"target":{"ip":"127.0.0.1"},"reasoning":"test","harness_name":"test"}`, status: http.StatusBadRequest},
		{name: "missing reasoning", body: `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","harness_name":"test"}`, status: http.StatusBadRequest},
		{name: "missing target", body: `{"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`, status: http.StatusBadRequest},
		{name: "missing harness name", body: `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test"}`, status: http.StatusBadRequest},
		{name: "obsolete requested by field", body: `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test","requested_by":"legacy"}`, status: http.StatusBadRequest},
		{name: "empty target", body: `{"target":{},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`, status: http.StatusBadRequest},
		{name: "unknown target", body: `{"target":{"ip":"192.0.2.10"},"command":"ssh {user}@192.0.2.10 hostname","reasoning":"test","harness_name":"test"}`, status: http.StatusNotFound},
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

func TestSubmitCommandRejectsOversizedRequestBody(t *testing.T) {
	server := testServer(t)
	body := strings.NewReader(strings.Repeat("x", int(maxRequestBodyBytes)+1))
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
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
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name:   "canonical command created with target hostname",
			server: testServer(t),
			body:   `{"target":{"hostname":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name: "duplicate target match rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, IP: "127.0.0.1", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name: "duplicate target for different agent accepted",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "127.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, IP: "127.0.0.1", Username: "u2", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusAccepted,
		},
		{
			name: "target hostname and ip conflict rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "alpha.example", IP: "10.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, Hostname: "beta.example", IP: "10.0.0.2", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"hostname":"alpha.example","ip":"10.0.0.2"},"command":"ssh {user}@alpha.example hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name: "command target conflict rejected",
			server: testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
				{Transport: targets.TransportSSH, Hostname: "alpha.example", IP: "10.0.0.1", Username: "u1", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p1")},
				{Transport: targets.TransportSSH, Hostname: "beta.example", IP: "10.0.0.2", Username: "u2", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p2")},
			})),
			body:   `{"target":{"hostname":"alpha.example"},"command":"ssh {user}@beta.example hostname","reasoning":"test","harness_name":"test"}`,
			status: http.StatusBadRequest,
		},
		{
			name:   "helper syntax rejected",
			server: testServer(t),
			body:   `{"target":{"ip":"127.0.0.1"},"command":"sshpass -p secret ssh demo@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`,
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
			body:   `{"target":{"ip":"127.0.0.1"},"command":"ls /home","reasoning":"test","harness_name":"test"}`,
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
				Target:      &tasks.Target{Hostname: "target.example"},
				Command:     "ssh {user}@target.example hostname",
				Reasoning:   "test",
				HarnessName: "test",
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
		Target: &tasks.Target{Hostname: "win.example"}, Command: `winrm {user}@win.example Get-ChildItem "C:\Program Files"`, Reasoning: "test", HarnessName: "test",
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
		Target:      &tasks.Target{Hostname: "target.example"},
		Command:     "ssh {user}@target.example echo {password}",
		Reasoning:   "test",
		HarnessName: "test",
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
	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`)
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
	body := `{"client_operation_id":"` + operationID + `","target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`
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
	body := `{"client_operation_id":"not-a-uuid","target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`
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

	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"codex","metadata":{"b":2,"a":1}}`)
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
		`debug command request body={"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"codex","metadata":{"b":2,"a":1}}`,
		"debug command created",
		"target=127.0.0.1",
		"command_family=ssh_remote_command",
		"strategy=go_ssh_client",
		"harness_name=\"codex\"",
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

	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ls /home","reasoning":"test","harness_name":"test"}`)
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

func TestSubmitCommandRejectsUnauthorizedAgentForTarget(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
		{Transport: targets.TransportSSH, Hostname: "127.0.0.1", IP: "127.0.0.1", Username: "u", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p")},
	}))
	body := bytes.NewBufferString(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 hostname","reasoning":"test","harness_name":"test"}`)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", body)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
