package e2e

import (
	"net/http"
	"testing"
)

func TestSubmitAndPollSshCommand(t *testing.T) {
	server, _ := newHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickSSHCommand+`","reasoning":"diagnostic","harness_name":"test"}`)
	if code != http.StatusAccepted {
		t.Fatalf("submit code = %d", code)
	}
	task := waitFinal(t, server, response.TaskID)
	if task.Result == nil {
		t.Fatalf("result = %#v", task.Result)
	}
}
