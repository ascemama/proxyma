package e2e

import (
	"net/http"
	"testing"
)

func TestUnknownTargetRejected(t *testing.T) {
	server, _ := newHarness(t)
	_, code, _ := submitCommand(t, server, `{"target":{"ip":"192.0.2.10"},"command":"ssh {user}@192.0.2.10 hostname","reasoning":"diagnostic","harness_name":"test"}`)
	if code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}
