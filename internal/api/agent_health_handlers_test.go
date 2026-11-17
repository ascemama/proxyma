package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"proxyma/internal/interaction"
	"proxyma/internal/store/sqlite"
)

func TestAgentReadOnlyEndpointsRejectMutationMethods(t *testing.T) {
	server := testServer(t)
	database, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	server.Interactions = interaction.New(database.NewInteractionStore(), interaction.Options{})
	paths := []string{
		"/v1/agent/targets/resolve?ip=127.0.0.1",
		"/v1/agent/targets/policy?ip=127.0.0.1",
		"/v1/agent/tasks/missing",
		"/v1/agent/interactions/missing",
		"/v1/health",
	}
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, path := range paths {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				req := newAuthenticatedRequest(t, method, path, nil)
				rec := httptest.NewRecorder()
				server.Routes().ServeHTTP(rec, req)
				if rec.Code != http.StatusMethodNotAllowed {
					t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
				}
			})
		}
	}
}

func TestHealth(t *testing.T) {
	server := testServer(t)
	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var response HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Capabilities) != 1 || response.Capabilities[0] != agentCommandIdempotencyCapability {
		t.Fatalf("capabilities=%#v", response.Capabilities)
	}
}
