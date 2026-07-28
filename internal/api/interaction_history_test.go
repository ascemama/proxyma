package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/interaction/history"
	"proxyma/internal/users"
)

type fakeInteractionHistoryStore struct {
	query history.InteractionQuery
	page  history.InteractionPage
}

func (s *fakeInteractionHistoryStore) ListInteractions(_ context.Context, query history.InteractionQuery) (history.InteractionPage, error) {
	s.query = query
	return s.page, nil
}

func TestUserInteractionHistoryEndpointUsesAgentVisibility(t *testing.T) {
	server := testServer(t)
	user := testUser
	user.Admin = false
	server.Users = users.NewRegistry([]users.User{user})
	agent := testAgent
	agent.UserIDs = []string{user.ID}
	server.Agents = agents.NewRegistry([]agents.Agent{agent})
	store := &fakeInteractionHistoryStore{page: history.InteractionPage{Items: []history.InteractionHistoryItem{{
		InteractionID:         "interaction-1",
		TaskID:                "task-1",
		AgentID:               agent.ID,
		InteractionInstanceID: "local",
		Kind:                  "approval_request",
		ExpectedResponse:      "approval",
		Status:                "answered",
		Subject:               "Approve command",
		Message:               "Approve command",
		Command:               "show version",
		CreatedAt:             time.Now().UTC(),
		TaskCreatedAt:         time.Now().UTC(),
	}}}}
	server.InteractionHistory = store
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/interaction-history?agent_id="+agent.ID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.query.CurrentUserID != user.ID || store.query.CurrentUserIsAdmin || store.query.AgentID != agent.ID {
		t.Fatalf("query = %#v", store.query)
	}
	var response UserInteractionHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].InteractionID != "interaction-1" {
		t.Fatalf("response = %#v", response)
	}
}

func TestUserInteractionHistoryEndpointRejectsCreatorFilterForNonAdmin(t *testing.T) {
	server := testServer(t)
	user := testUser
	user.Admin = false
	server.Users = users.NewRegistry([]users.User{user})
	server.InteractionHistory = &fakeInteractionHistoryStore{}
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/interaction-history?interaction_instance_creator_user_id="+user.ID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserInteractionHistoryEndpointRejectsInvisibleAgentFilter(t *testing.T) {
	server := testServer(t)
	user := testUser
	user.Admin = false
	server.Users = users.NewRegistry([]users.User{user})
	agent := testAgent
	agent.UserIDs = []string{"different-user"}
	server.Agents = agents.NewRegistry([]agents.Agent{agent})
	server.InteractionHistory = &fakeInteractionHistoryStore{}
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/interaction-history?agent_id="+agent.ID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserInteractionHistoryEndpointRejectsBroadRegex(t *testing.T) {
	server := testServer(t)
	user := testUser
	user.Admin = false
	server.Users = users.NewRegistry([]users.User{user})
	server.InteractionHistory = &fakeInteractionHistoryStore{}
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/interaction-history?command_pattern=show.*&command_pattern_mode=regex", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func interactionHistorySessionCookie(t *testing.T, server *Server, userID string) *http.Cookie {
	t.Helper()
	_, cookieValue, _, err := server.UserSessions.Create(userID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: userSessionCookieName, Value: cookieValue}
}
