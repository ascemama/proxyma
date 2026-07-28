package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxyma/internal/agents"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthenticationRequiredForAllEndpoints(t *testing.T) {
	server := testServer(t)
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "commands", method: http.MethodPost, path: "/v1/agent/commands"},
		{name: "tasks", method: http.MethodGet, path: "/v1/agent/tasks/missing"},
		{name: "health", method: http.MethodGet, path: "/v1/health"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			assertAuthError(t, rec, http.StatusUnauthorized, "auth_required")
		})
	}
}

func TestOldFlatRoutesAreNotWired(t *testing.T) {
	server := testServer(t)
	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/login"},
		{method: http.MethodGet, path: "/v1/session"},
		{method: http.MethodPost, path: "/v1/logout"},
		{method: http.MethodPost, path: "/v1/commands"},
		{method: http.MethodGet, path: "/v1/tasks/missing"},
		{method: http.MethodGet, path: "/v1/interactions/missing"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAuthenticationRejectsInvalidToken(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	assertAuthError(t, rec, http.StatusUnauthorized, "invalid_token")
}

func TestAuthenticationDebugLogsInvalidTokenWithoutLeakingToken(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Debug = true
	server.Logger = log.New(&logs, "", 0)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	assertAuthError(t, rec, http.StatusUnauthorized, "invalid_token")

	output := logs.String()
	if !strings.Contains(output, "debug agent auth failed") || !strings.Contains(output, "code=invalid_token") {
		t.Fatalf("missing auth debug log: %s", output)
	}
	if strings.Contains(output, "not-a-jwt") {
		t.Fatalf("debug log leaked bearer token: %s", output)
	}
}

func TestAuthenticationRejectsWrongSignature(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+signedAPITestToken(t, testAgent.ID, testAgent.ID, "wrong-signing-key", jwt.SigningMethodHS256, time.Now(), time.Now().Add(agents.MaxTokenLifetime)))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	assertAuthError(t, rec, http.StatusUnauthorized, "invalid_token")
}

func TestAuthenticationRejectsExpiredTokenWithRetryableCode(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+signedAPITestToken(t, testAgent.ID, testAgent.ID, testAgent.AgentJWTSigningKey, jwt.SigningMethodHS256, time.Now().Add(-agents.MaxTokenLifetime), time.Now().Add(-time.Second)))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	assertAuthError(t, rec, http.StatusUnauthorized, "token_expired")
	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Retryable || response.Hint == "" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAuthenticationRejectsUnknownAgent(t *testing.T) {
	server := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+signedAPITestToken(t, "unknown-agent", "unknown-agent", testAgent.AgentJWTSigningKey, jwt.SigningMethodHS256, time.Now(), time.Now().Add(agents.MaxTokenLifetime)))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	assertAuthError(t, rec, http.StatusUnauthorized, "invalid_token")
}

func TestAuthenticationRejectsDisabledAgent(t *testing.T) {
	disabled := agents.Agent{ID: "disabled-agent", AgentJWTSigningKey: "signing-key", Enabled: false}
	server := testServer(t)
	server.Agents = agents.NewRegistry([]agents.Agent{disabled})
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+signedAPITestToken(t, disabled.ID, disabled.ID, disabled.AgentJWTSigningKey, jwt.SigningMethodHS256, time.Now(), time.Now().Add(agents.MaxTokenLifetime)))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	assertAuthError(t, rec, http.StatusUnauthorized, "invalid_token")
}

func TestAuthenticationAllowsSecondDummyAgent(t *testing.T) {
	automation := agents.Agent{ID: "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", AgentJWTSigningKey: "automation-signing-key", Enabled: true}
	server := testServer(t)
	server.Agents = agents.NewRegistry([]agents.Agent{testAgent, automation})
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+signedAPITestToken(t, automation.ID, automation.ID, automation.AgentJWTSigningKey, jwt.SigningMethodHS256, time.Now(), time.Now().Add(agents.MaxTokenLifetime)))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func assertAuthError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate header")
	}
	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != code {
		t.Fatalf("code = %q response=%#v", response.Code, response)
	}
}

func signedAPITestToken(t *testing.T, kid, subject, agentJWTSigningKey string, method jwt.SigningMethod, issuedAt, expiresAt time.Time) string {
	t.Helper()
	claims := agents.Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}}
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString([]byte(agentJWTSigningKey))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
