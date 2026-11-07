package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"proxyma/internal/agents"
	"proxyma/internal/interaction/history"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
	"strings"
	"testing"
)

func TestUserCommandHistoryEndpointFiltersVisibleTasksAndRedactsSecrets(t *testing.T) {
	server := testLoginServer(t, false)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	ownedAgent := agents.Agent{
		ID:                 testAgent.ID,
		DisplayName:        "Owned Agent",
		AgentJWTSigningKey: "owned-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{testUser.ID},
	}
	otherAgent := agents.Agent{
		ID:                 "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:        "Other Agent",
		AgentJWTSigningKey: "other-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{ownedAgent, otherAgent})
	ownedTarget := targets.Target{
		ID:       "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname: "owned.example",
		IP:       "10.0.0.1",
		Username: "owned-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{ownedAgent.ID, otherAgent.ID},
		UserIDs:  []string{testUser.ID},
		Auth:     targets.PasswordAuth("owned-target-password"),
	}
	otherTarget := targets.Target{
		ID:       "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname: "other.example",
		IP:       "10.0.0.2",
		Username: "other-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{otherAgent.ID},
		UserIDs:  []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"},
		Auth:     targets.PasswordAuth("other-target-password"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{ownedTarget, otherTarget})

	visibleByAgent, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   ownedAgent.ID,
		AgentName: ownedAgent.DisplayName,
		Target:    &tasks.Target{ID: otherTarget.ID, Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example hostname",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	visibleByTarget, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		AgentName: otherAgent.DisplayName,
		Target:    &tasks.Target{ID: ownedTarget.ID, Hostname: ownedTarget.Hostname, IP: ownedTarget.IP},
		Command:   "ssh {user}@owned.example uptime",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		AgentName: otherAgent.DisplayName,
		Target:    &tasks.Target{ID: otherTarget.ID, Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example whoami",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	cookie := loginCookie(t, server)
	indexTasksForHistory(t, server)
	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?entry_type=command&limit=10", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if !containsHistoryTask(response.Items, visibleByAgent.TaskID) || !containsHistoryTask(response.Items, visibleByTarget.TaskID) {
		t.Fatalf("visible tasks missing from response = %#v", response)
	}
	if containsHistoryTask(response.Items, hidden.TaskID) {
		t.Fatalf("hidden task leaked in response = %#v", response)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-signing-key", "other-agent-signing-key", "owned-target-password", "other-target-password", "owned-user", "other-user"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("history response leaked %q: %s", leaked, body)
		}
	}
}

func TestUserCommandHistoryEndpointAdminSeesAllTasksAndRedactsSecrets(t *testing.T) {
	server := testLoginServer(t, false)
	ownedAgent := agents.Agent{
		ID:                 testAgent.ID,
		DisplayName:        "Owned Agent",
		AgentJWTSigningKey: "owned-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{testUser.ID},
	}
	otherAgent := agents.Agent{
		ID:                 "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:        "Other Agent",
		AgentJWTSigningKey: "other-agent-signing-key",
		Enabled:            true,
		UserIDs:            []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{ownedAgent, otherAgent})
	ownedTarget := targets.Target{
		ID:       "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname: "owned.example",
		IP:       "10.0.0.1",
		Username: "owned-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{ownedAgent.ID, otherAgent.ID},
		UserIDs:  []string{testUser.ID},
		Auth:     targets.PasswordAuth("owned-target-password"),
	}
	otherTarget := targets.Target{
		ID:       "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname: "other.example",
		IP:       "10.0.0.2",
		Username: "other-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{otherAgent.ID},
		UserIDs:  []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"},
		Auth:     targets.PasswordAuth("other-target-password"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{ownedTarget, otherTarget})

	visibleByAgent, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   ownedAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example hostname",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	visibleByTarget, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: ownedTarget.Hostname, IP: ownedTarget.IP},
		Command:   "ssh {user}@owned.example uptime",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example whoami",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	cookie := loginCookie(t, server)
	indexTasksForHistory(t, server)
	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?entry_type=command&limit=10", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 3 {
		t.Fatalf("response = %#v", response)
	}
	for _, taskID := range []string{visibleByAgent.TaskID, visibleByTarget.TaskID, hidden.TaskID} {
		if !containsHistoryTask(response.Items, taskID) {
			t.Fatalf("task %s missing from response = %#v", taskID, response)
		}
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-signing-key", "other-agent-signing-key", "owned-target-password", "other-target-password", "owned-user", "other-user"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("history response leaked %q: %s", leaked, body)
		}
	}
}

func TestHistoryLimitCapsAtMaximum(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?limit=9999", nil)
	limit, err := interactionHistoryLimit(req.URL.Query())
	if err != nil {
		t.Fatal(err)
	}
	if limit != history.MaxLimit {
		t.Fatalf("limit = %d", limit)
	}
	if history.MaxLimit != 100 {
		t.Fatalf("history.MaxLimit = %d", history.MaxLimit)
	}
}
