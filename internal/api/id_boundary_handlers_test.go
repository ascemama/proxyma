package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testPolicyID = "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"

func TestUUIDBackedPathIDsAreRejectedBeforeLookup(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	tests := []struct {
		name string
		path string
		verb string
		code string
	}{
		{name: "user", path: "/v1/user/users/not-a-uuid", verb: http.MethodPut, code: "invalid_user"},
		{name: "activation link user", path: "/v1/user/users/not-a-uuid/activation-link", verb: http.MethodPost, code: "invalid_user"},
		{name: "password reset user", path: "/v1/user/users/not-a-uuid/password-reset-link", verb: http.MethodPost, code: "invalid_user"},
		{name: "agent", path: "/v1/user/agents/not-a-uuid", verb: http.MethodPut, code: "invalid_agent"},
		{name: "target", path: "/v1/user/targets/not-a-uuid", verb: http.MethodPut, code: "invalid_target"},
		{name: "policy", path: "/v1/user/policies/not-a-uuid", verb: http.MethodGet, code: "invalid_policy"},
		{name: "LLM profile", path: "/v1/user/llm-profiles/not-a-uuid", verb: http.MethodGet, code: "invalid_user"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := userTargetMutationRequest(t, server, test.verb, test.path, cookie, session.CSRFToken, `{}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), test.code) {
				t.Fatalf("body = %s, missing code %q", rec.Body.String(), test.code)
			}
		})
	}

	taskRequest := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/tasks/not-a-uuid", nil)
	taskRecorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(taskRecorder, taskRequest)
	if taskRecorder.Code != http.StatusBadRequest || !strings.Contains(taskRecorder.Body.String(), "invalid_task") {
		t.Fatalf("task status = %d body=%s", taskRecorder.Code, taskRecorder.Body.String())
	}
}

func TestUUIDBackedBodyAndQueryReferencesAreRejected(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	tests := []struct {
		name string
		path string
		verb string
		body string
		code string
	}{
		{
			name: "target policy id",
			path: "/v1/user/targets",
			verb: http.MethodPost,
			body: `{"policy_id":"not-a-uuid"}`,
			code: "invalid_target",
		},
		{
			name: "target agent ids",
			path: "/v1/user/targets",
			verb: http.MethodPost,
			body: `{"agent_ids":["not-a-uuid"]}`,
			code: "invalid_target",
		},
		{
			name: "credential test target id",
			path: "/v1/user/targets/test-credentials",
			verb: http.MethodPost,
			body: `{"target_id":"not-a-uuid"}`,
			code: "invalid_target",
		},
		{
			name: "policy target ids",
			path: "/v1/user/policies/" + testPolicyID + "/targets",
			verb: http.MethodPut,
			body: `{"target_ids":["not-a-uuid"]}`,
			code: "invalid_policy",
		},
		{
			name: "policy evaluation target id",
			path: "/v1/user/policies/" + testPolicyID + "/evaluate",
			verb: http.MethodPost,
			body: `{"target_id":"not-a-uuid","command":"hostname"}`,
			code: "invalid_policy",
		},
		{
			name: "policy LLM profile id",
			path: "/v1/user/policies",
			verb: http.MethodPost,
			body: `{"name":"bad profile reference","llm":{"profile_id":"not-a-uuid"}}`,
			code: "invalid_policy",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := userTargetMutationRequest(t, server, test.verb, test.path, cookie, session.CSRFToken, test.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), test.code) {
				t.Fatalf("body = %s, missing code %q", rec.Body.String(), test.code)
			}
		})
	}
}

func TestInteractionCreatorAndHistoryIDsAreValidated(t *testing.T) {
	server, _ := testInteractionMutationServer(t)
	cookie, session := loginSession(t, server)

	interaction := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/interactions", cookie, session.CSRFToken, `{"creator_user_id":"not-a-uuid","type":"ntfy"}`)
	if interaction.Code != http.StatusBadRequest || !strings.Contains(interaction.Body.String(), "invalid_interaction") {
		t.Fatalf("interaction status = %d body=%s", interaction.Code, interaction.Body.String())
	}

	for _, query := range []string{
		"agent_id=not-a-uuid",
		"interaction_instance_creator_user_id=not-a-uuid",
	} {
		rec := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/history?"+query, cookie, "", "")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_history_query") {
			t.Fatalf("query %q status = %d body=%s", query, rec.Code, rec.Body.String())
		}
	}
}
