package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDebugDisabledDoesNotLogRequests(t *testing.T) {
	var logs bytes.Buffer
	server := testServer(t)
	server.Logger = log.New(&logs, "", 0)

	req := newAuthenticatedRequest(t, http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if logs.Len() != 0 {
		t.Fatalf("unexpected logs: %s", logs.String())
	}
}
