package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/executor"
	"proxyma/internal/interaction"
	"proxyma/internal/store/sqlite"
	"proxyma/internal/tasks"
)

func TestInteractionStatusAndAgentResponseRemoval(t *testing.T) {
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	interactionService := interaction.New(database.NewInteractionStore(), interaction.Options{Timeout: time.Hour})
	manager := tasks.NewManager(database.NewCommandStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	server := &Server{
		Agents:       testAgentRegistry(),
		Tasks:        manager,
		Interactions: interactionService,
	}
	task, err := manager.Submit(context.Background(), tasks.CommandContext{AgentID: testAgent.ID, Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := interactionService.Request(context.Background(), interaction.Request{
		TaskID:  task.TaskID,
		Kind:    interaction.KindApprovalRequest,
		Subject: "Approve command",
		Message: "Run command?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WaitForInteraction(task.TaskID, record.ToSummary()); err != nil {
		t.Fatal(err)
	}

	getReq := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/interactions/"+record.InteractionID, nil)
	getRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got InteractionResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.InteractionID != record.InteractionID || got.Status != interaction.StatusPending {
		t.Fatalf("got = %#v", got)
	}

	postReq := newAuthenticatedRequest(t, http.MethodPost, "/v1/agent/interactions/"+record.InteractionID+"/responses", bytes.NewBufferString(`{"approved":true,"responded_by":"operator"}`))
	postRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusNotFound {
		t.Fatalf("post status = %d body=%s", postRec.Code, postRec.Body.String())
	}
	unchanged, err := interactionService.Get(context.Background(), record.InteractionID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != interaction.StatusPending || unchanged.Response != nil {
		t.Fatalf("interaction changed = %#v", unchanged)
	}
	waiting, ok := manager.Get(task.TaskID)
	if !ok {
		t.Fatal("missing task")
	}
	if waiting.Status != tasks.StatusWaitingUserResponse || waiting.PendingInteraction == nil || waiting.Approval == nil || waiting.Approval.Status != tasks.ApprovalStatusPending {
		t.Fatalf("waiting = %#v", waiting)
	}
}

func TestInteractionRejectsOtherAgent(t *testing.T) {
	otherAgent := agents.Agent{ID: "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", AgentJWTSigningKey: "automation-signing-key", Enabled: true}
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	interactionService := interaction.New(database.NewInteractionStore(), interaction.Options{Timeout: time.Hour})
	manager := tasks.NewManager(database.NewCommandStore(), executor.Executor{Timeout: time.Second, OutputLimit: 1024})
	server := &Server{
		Agents:       agents.NewRegistry([]agents.Agent{testAgent, otherAgent}),
		Tasks:        manager,
		Interactions: interactionService,
	}
	task, err := manager.Submit(context.Background(), tasks.CommandContext{AgentID: testAgent.ID, Command: "echo done", Reasoning: "test"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := interactionService.Request(context.Background(), interaction.Request{
		TaskID:  task.TaskID,
		Kind:    interaction.KindApprovalRequest,
		Subject: "Approve command",
		Message: "Run command?",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := newAuthenticatedRequestForAgent(t, otherAgent, http.MethodGet, "/v1/agent/interactions/"+record.InteractionID, nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestInteractionNotFound(t *testing.T) {
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server := &Server{Agents: testAgentRegistry(), Interactions: interaction.New(database.NewInteractionStore(), interaction.Options{})}
	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/interactions/missing", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
