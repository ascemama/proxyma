package e2e

import (
	"net/http"
	"testing"

	"proxyma/internal/transcriptor"
)

func TestPlaceholderCommandCreatedAndStoredSafely(t *testing.T) {
	server, _ := newHarness(t)
	response, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"`+quickPlaceholderSSHCommand+`","reasoning":"verify substitution","harness_name":"e2e"}`)
	if code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	task := waitFinal(t, server, response.TaskID)
	if task.Command != quickPlaceholderSSHCommand {
		t.Fatalf("command = %q", task.Command)
	}
	if task.Transcription == nil || task.Transcription.CommandFamily != transcriptor.KindSSHRemoteCommand {
		t.Fatalf("transcription = %#v", task.Transcription)
	}
}

func TestBareCommandRejected(t *testing.T) {
	server, _ := newHarness(t)
	_, code, _ := submitCommand(t, server, `{"target":{"ip":"127.0.0.1"},"command":"echo unchanged","reasoning":"verify regression","harness_name":"e2e"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
}
