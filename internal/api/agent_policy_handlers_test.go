package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
)

func TestAgentPolicyGuidanceIsBoundToAuthorizedTarget(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
		{
			ID: "target-a", Transport: targets.TransportSSH, OS: targets.OSLinux,
			Hostname: "alpha.example", IP: "10.0.0.1", Username: "private-a",
			PolicyID: "policy-a", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret-a"),
		},
		{
			ID: "target-b", Transport: targets.TransportSSH, OS: targets.OSLinux,
			Hostname: "beta.example", IP: "10.0.0.2", Username: "private-b",
			PolicyID: "policy-b", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret-b"),
		},
	}))
	server.Policies = policy.NewMutableEvaluator(policy.NewSet(map[string]policy.Policy{
		"policy-a": {
			ID: "policy-a", Name: "Alpha policy", Description: "Alpha guidance", Enabled: true,
			OwnerUserID: "owner-a", LLM: &policy.LLMPolicy{Enabled: true, ProfileID: "profile-secret", Prompt: "secret prompt"},
			Rules: []policy.Rule{
				{ID: "deny-a", Effect: policy.DecisionDeny, CommandTemplate: "rm -rf {path}", Reason: "destructive"},
				{ID: "allow-a", Effect: policy.DecisionAllow, CommandTemplate: "ping {ipv4}", Reason: "diagnostic"},
			},
		},
		"policy-b": {ID: "policy-b", Name: "Beta policy", Enabled: true, Rules: []policy.Rule{{ID: "allow-b", Effect: policy.DecisionAllow, CommandTemplate: "hostname"}}},
	}))

	request := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/policy?hostname=alpha.example", nil)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response AgentPolicyGuidanceResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Target.ID != "target-a" || response.Policy.ID != "policy-a" || response.Policy.Name != "Alpha policy" {
		t.Fatalf("response = %#v", response)
	}
	if len(response.Policy.Blacklist) != 1 || response.Policy.Blacklist[0].CommandTemplate != "rm -rf {path}" {
		t.Fatalf("blacklist = %#v", response.Policy.Blacklist)
	}
	if len(response.Policy.Whitelist) != 1 || response.Policy.Whitelist[0].CommandTemplate != "ping {ipv4}" {
		t.Fatalf("whitelist = %#v", response.Policy.Whitelist)
	}
	body := recorder.Body.String()
	for _, secret := range []string{"private-a", "secret-a", "owner-a", "profile-secret", "secret prompt", "deny-a", "allow-a"} {
		if strings.Contains(body, secret) {
			t.Fatalf("policy guidance leaked %q: %s", secret, body)
		}
	}
}

func TestAgentPolicyGuidanceRejectsUnauthorizedAndArbitraryPolicyRequests(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{{
		ID: "target-a", Transport: targets.TransportSSH, OS: targets.OSLinux,
		Hostname: "alpha.example", PolicyID: "policy-a", AgentIDs: []string{"another-agent"}, Auth: targets.PasswordAuth("secret"),
	}}))
	server.Policies = policy.NewMutableEvaluator(policy.NewSet(map[string]policy.Policy{
		"policy-a": {ID: "policy-a", Name: "Alpha", Enabled: true},
	}))

	for _, path := range []string{
		"/v1/agent/targets/policy?hostname=alpha.example",
		"/v1/agent/targets/policy?hostname=alpha.example&policy_id=policy-a",
	} {
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, newAuthenticatedRequest(t, http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusBadRequest {
			t.Fatalf("path %s status = %d, body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestAgentPolicyGuidanceUsesCurrentTargetAssignment(t *testing.T) {
	server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{{
		ID: "target-a", Transport: targets.TransportSSH, OS: targets.OSLinux,
		Hostname: "alpha.example", PolicyID: "policy-a", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret"),
	}}))
	server.Policies = policy.NewMutableEvaluator(policy.NewSet(map[string]policy.Policy{
		"policy-a": {ID: "policy-a", Name: "Alpha", Enabled: true},
		"policy-b": {ID: "policy-b", Name: "Beta", Enabled: true},
	}))

	get := func() AgentPolicyGuidanceResponse {
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/policy?hostname=alpha.example", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
		}
		var response AgentPolicyGuidanceResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	if first := get(); first.Policy.ID != "policy-a" {
		t.Fatalf("first policy = %#v", first.Policy)
	}
	server.setTargetRegistry(targets.NewRegistry([]targets.Target{{
		ID: "target-a", Transport: targets.TransportSSH, OS: targets.OSLinux,
		Hostname: "alpha.example", PolicyID: "policy-b", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("secret"),
	}}))
	if second := get(); second.Policy.ID != "policy-b" || second.Policy.Name != "Beta" {
		t.Fatalf("updated policy = %#v", second.Policy)
	}
}
