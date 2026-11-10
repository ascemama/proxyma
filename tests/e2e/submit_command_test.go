package e2e

import (
	"net/http"
	"testing"
	"time"

	"proxyma/internal/tasks"
)

func TestSubmitCommandCreatedWithinTwoSeconds(t *testing.T) {
	server, _ := newHarness(t)
	response, code, elapsed := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"diagnostic","harness_name":"agent-demo"}`)
	if code != http.StatusAccepted {
		t.Fatalf("code = %d", code)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("submit took %s", elapsed)
	}
	if response.TaskID == "" || response.Status != tasks.StatusCreated {
		t.Fatalf("response = %#v", response)
	}
	_ = waitFinal(t, server, response.TaskID)
}
