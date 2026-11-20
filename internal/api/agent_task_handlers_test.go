package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/sshclient"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
)

func TestTaskStatusResponses(t *testing.T) {
	server := testServer(t)
	task, err := server.Tasks.Submit(httptest.NewRequest(http.MethodPost, "/", nil).Context(), tasks.CommandContext{
		AgentID:   testAgent.ID,
		Command:   "ssh {user}@127.0.0.1 hostname",
		Reasoning: "test",
		Target:    &tasks.Target{Hostname: "127.0.0.1", IP: "127.0.0.1"},
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		ExecutionPlan: &transcriptor.Plan{
			Kind: transcriptor.KindSSHRemoteCommand,
			Transcription: transcriptor.Metadata{
				Transcribed:   true,
				CommandFamily: transcriptor.KindSSHRemoteCommand,
				Strategy:      transcriptor.StrategyGoSSHClient,
			},
			SSHRequest: &sshclient.Request{Host: "127.0.0.1", Port: 22, Username: "demo", Password: "secret", RemoteCommand: "hostname"},
		},
		SensitiveValues: []string{"demo", "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started, err := server.Tasks.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}

	final := waitHTTPStatus(t, server, task.TaskID)
	if final.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s", final.Status)
	}
	if final.AgentID != testAgent.ID {
		t.Fatalf("agent id = %q", final.AgentID)
	}
	if final.Result == nil || final.Result.ExitStatus == nil {
		t.Fatalf("result = %#v", final.Result)
	}
}

func TestTaskStatusReturnsSafeTranscriptionMetadata(t *testing.T) {
	server := testServer(t)
	req := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/commands", strings.NewReader(`{"target":{"ip":"127.0.0.1"},"command":"ssh {user}@127.0.0.1 echo {password}","reasoning":"test","harness_name":"test"}`))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d body=%s", rec.Code, rec.Body.String())
	}

	var response CommandCreatedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if started, err := server.Tasks.StartCreated(context.Background()); err != nil || started != 1 {
		t.Fatalf("started = %d, err = %v", started, err)
	}
	final := waitHTTPStatus(t, server, response.TaskID)
	if final.Command != `ssh {user}@127.0.0.1 echo {password}` {
		t.Fatalf("command = %q", final.Command)
	}
	if final.Target == nil || final.Target.IP != "127.0.0.1" {
		t.Fatalf("target = %#v", final.Target)
	}
	if final.Transcription == nil || final.Transcription.CommandFamily != transcriptor.KindSSHRemoteCommand {
		t.Fatalf("transcription = %#v", final.Transcription)
	}
	body, err := json.Marshal(final)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"p\"", "\"u\"", "echo secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("status leaked %q in %s", forbidden, string(body))
		}
	}
}

func TestTaskStatusNotFound(t *testing.T) {
	server := testServer(t)
	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/tasks/6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTaskStatusRejectsOtherAgent(t *testing.T) {
	otherAgent := agents.Agent{ID: "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", AgentJWTSigningKey: "automation-signing-key", Enabled: true}
	server := testServer(t)
	server.Agents = agents.NewRegistry([]agents.Agent{testAgent, otherAgent})
	task, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   testAgent.ID,
		Command:   "echo done",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := newAuthenticatedRequestForAgent(t, otherAgent, http.MethodGet, "/v1/agent/tasks/"+task.TaskID, nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestTaskStatusRejectsUnownedTask(t *testing.T) {
	server := testServer(t)
	task, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		Command:   "echo done",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/tasks/"+task.TaskID, nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func waitHTTPStatus(t *testing.T, server *Server, id string) TaskStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/tasks/"+id, nil)
		rec := httptest.NewRecorder()
		start := time.Now()
		server.Routes().ServeHTTP(rec, req)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("status request took %s", elapsed)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
		}
		var task TaskStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if tasks.IsFinalStatus(task.Status) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return TaskStatusResponse{}
}
