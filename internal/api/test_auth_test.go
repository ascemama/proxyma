package api

import (
	"io"
	"net/http"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/users"
)

var testAgent = agents.Agent{
	ID:                 "df9ec8bb-1a22-4181-a7c8-126ad553221d",
	DisplayName:        "Codex Local Harness",
	AgentJWTSigningKey: "test-signing-key",
	Enabled:            true,
}

func testAgentRegistry() agents.Registry {
	return agents.NewRegistry([]agents.Agent{testAgent})
}

var testUser = users.User{
	ID:         "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
	Username:   "demo-admin",
	Name:       "Demo Admin",
	Admin:      true,
	Status:     users.StatusActive,
	PasswdHash: "$argon2id$v=19$m=19456,t=2,p=1$WGeElL1uhb1hV8gVYptHmg$rvzcxwJQV6VK9gqH/GnHKWGB3t+40DX8XUYLkr8iiGE",
}

func testUserRegistry() users.Registry {
	return users.NewRegistry([]users.User{testUser})
}

func newAuthenticatedRequest(t *testing.T, method string, target string, body io.Reader) *http.Request {
	t.Helper()
	req := httpRequest(method, target, body)
	authenticateTestRequest(t, req)
	return req
}

func newAuthenticatedRequestForAgent(t *testing.T, agent agents.Agent, method string, target string, body io.Reader) *http.Request {
	t.Helper()
	req := httpRequest(method, target, body)
	authenticateRequestForAgent(t, req, agent)
	return req
}

func httpRequest(method string, target string, body io.Reader) *http.Request {
	return mustNewRequest(method, target, body)
}

func mustNewRequest(method string, target string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		panic(err)
	}
	return req
}

func authenticateTestRequest(t *testing.T, req *http.Request) {
	t.Helper()
	authenticateRequestForAgent(t, req, testAgent)
}

func authenticateRequestForAgent(t *testing.T, req *http.Request, agent agents.Agent) {
	t.Helper()
	token, err := agents.IssueToken(agent, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
}
