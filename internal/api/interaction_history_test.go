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

func (s *fakeInteractionHistoryStore) ListHistory(_ context.Context, query history.HistoryQuery) (history.HistoryPage, error) {
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
		PolicyJustification:   "The command only reads version information.",
		CreatedAt:             time.Now().UTC(),
		TaskCreatedAt:         time.Now().UTC(),
	}}}}
	server.InteractionHistory = store
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?entry_type=interaction&agent_id="+agent.ID, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.query.CurrentUserID != user.ID || store.query.CurrentUserIsAdmin || store.query.AgentID != agent.ID {
		t.Fatalf("query = %#v", store.query)
	}
	var response UserHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].InteractionID != "interaction-1" {
		t.Fatalf("response = %#v", response)
	}
	if response.Items[0].PolicyJustification != "The command only reads version information." {
		t.Fatalf("policy justification = %q", response.Items[0].PolicyJustification)
	}
}

func TestUserInteractionHistoryEndpointRejectsCreatorFilterForNonAdmin(t *testing.T) {
	server := testServer(t)
	user := testUser
	user.Admin = false
	server.Users = users.NewRegistry([]users.User{user})
	server.InteractionHistory = &fakeInteractionHistoryStore{}
	cookie := interactionHistorySessionCookie(t, server, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?interaction_instance_creator_user_id="+user.ID, nil)
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

	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?agent_id="+agent.ID, nil)
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

	req := httptest.NewRequest(http.MethodGet, "/v1/user/history?command_pattern=show.*&command_pattern_mode=regex", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLegacyHistoryEndpointsAreRemoved(t *testing.T) {
	server := testServer(t)
	user := testUser
	server.Users = users.NewRegistry([]users.User{user})
	cookie := interactionHistorySessionCookie(t, server, user.ID)
	for _, path := range []string{"/v1/user/command-history", "/v1/user/interaction-history"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
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
