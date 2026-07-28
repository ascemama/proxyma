package e2e

import (
	"net/http"
	"testing"
)

func TestAuditReadyRecordAfterCompletion(t *testing.T) {
	server, _ := newHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"audit reason","requested_by":"agent"}`)
	if code != http.StatusAccepted {
		t.Fatalf("submit code = %d", code)
	}
	task := waitFinal(t, server, response.TaskID)
	if task.Target == nil || task.Command == "" || task.Reasoning == "" || task.RequestedBy == "" {
		t.Fatalf("missing audit fields: %#v", task)
	}
	if len(task.History) < 3 {
		t.Fatalf("history too short: %#v", task.History)
	}
	if task.Result == nil || task.CompletedAt == nil {
		t.Fatalf("missing final result/timestamp: %#v", task)
	}
}
