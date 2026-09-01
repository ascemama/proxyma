package agentcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryCommandHelpDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("PROXYMA_HARNESS_CONFIG", "")
	t.Chdir(t.TempDir())
	commands := []string{"resume", "submit", "poll", "task-update", "task-complete", "inspect", "doctor", "version", "help"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{command, "--help"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			var envelope Envelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if !envelope.OK || envelope.Help == nil || envelope.Help.Usage == "" {
				t.Fatalf("envelope=%#v", envelope)
			}
		})
	}
}

func TestGlobalHelpDoesNotRequireConfiguration(t *testing.T) {
	t.Setenv("PROXYMA_HARNESS_CONFIG", "")
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Help == nil || len(envelope.Help.Commands) == 0 {
		t.Fatalf("envelope=%#v", envelope)
	}
}

func TestHelpCommandReturnsCommandHelp(t *testing.T) {
	t.Setenv("PROXYMA_HARNESS_CONFIG", "")
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"help", "task-complete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Help == nil || !strings.Contains(envelope.Help.Usage, "task-complete") {
		t.Fatalf("envelope=%#v", envelope)
	}
}

func TestTaskCompleteHelpDefinesExpectedRevision(t *testing.T) {
	help, ok := helpFor("task-complete")
	if !ok {
		t.Fatal("task-complete help missing")
	}
	var expected bool
	for _, field := range help.InputFields {
		if field.Name == "revision" {
			t.Fatal("help must not advertise revision as an input field")
		}
		if field.Name == "expected_revision" && field.Required {
			expected = true
		}
	}
	if !expected {
		t.Fatal("required expected_revision field missing")
	}
}

func TestPollParserReportsUnsupportedRevision(t *testing.T) {
	if id, err := parseSingleFlag("poll", []string{"--task", "task-id"}, "task"); err != nil || id != "task-id" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	_, err := parseSingleFlag("poll", []string{"--task", "task-id", "--revision", "3"}, "task")
	typed, ok := err.(*commandError)
	if !ok || !strings.Contains(typed.message, "revision") || !strings.Contains(typed.hint, "poll --help") {
		t.Fatalf("error=%#v", err)
	}
}

func TestTaskCompleteFlagsPointToCommandHelp(t *testing.T) {
	_, err := parseInputFlag("task-complete", []string{"--task", "task-id", "--revision", "5", "--summary", "done"})
	typed, ok := err.(*commandError)
	if !ok || !strings.Contains(typed.message, "task") || !strings.Contains(typed.hint, "task-complete --help") {
		t.Fatalf("error=%#v", err)
	}
}

func TestInvalidJSONInputPointsToCommandHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complete.json")
	if err := os.WriteFile(path, []byte(`{"client_task_id":"c20e42a9-fb6f-4517-a201-639280b55444","revision":5,"final_summary":"done"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var input CompleteInput
	err := decodeInput("task-complete", path, &input)
	typed, ok := err.(*commandError)
	if !ok || !strings.Contains(typed.message, `unknown field "revision"`) || !strings.Contains(typed.hint, "task-complete --help") {
		t.Fatalf("error=%#v", err)
	}
}

func TestUsageErrorsHaveMeaningfulTimestampAndHelpHint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d", code)
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error == nil || envelope.Error.At.IsZero() || !strings.Contains(envelope.Error.Hint, "--help") {
		t.Fatalf("error=%#v", envelope.Error)
	}
}
