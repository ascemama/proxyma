package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxyma/internal/store"
	"proxyma/internal/tasks"
	"proxyma/internal/transcriptor"
)

func TestPersistedTaskContainsAuditReadyFields(t *testing.T) {
	fs := store.New(t.TempDir())
	now := time.Now().UTC()
	exit := 0
	task := tasks.Task{
		TaskID:      "audit",
		AgentID:     "df9ec8bb-1a22-4181-a7c8-126ad553221d",
		Target:      &tasks.Target{Hostname: "demo-target", IP: "127.0.0.1"},
		Command:     `ssh {user}@127.0.0.1 "hostname"`,
		Reasoning:   "verify audit",
		HarnessName: "agent",
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		Status:      tasks.StatusCompleted,
		CreatedAt:   now,
		CompletedAt: &now,
		Result:      &tasks.TaskResult{Stdout: "audit\n", Stderr: "", ExitStatus: &exit, Truncated: false},
		History:     []tasks.LifecycleEvent{{Status: tasks.StatusCreated, At: now}, {Status: tasks.StatusCompleted, At: now}},
	}
	if err := fs.WriteJSON(task.TaskID, task); err != nil {
		t.Fatal(err)
	}
	var recovered tasks.Task
	if err := fs.ReadJSON(task.TaskID, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.AgentID == "" || recovered.Command == "" || recovered.Reasoning == "" || recovered.Target == nil || len(recovered.History) != 2 || recovered.Result == nil {
		t.Fatalf("not audit ready: %#v", recovered)
	}
}

func TestPersistedTaskOmitsExecutionPlanAndSensitiveValues(t *testing.T) {
	dir := t.TempDir()
	fs := store.New(dir)
	now := time.Now().UTC()
	task := tasks.Task{
		TaskID:          "transcribed",
		AgentID:         "df9ec8bb-1a22-4181-a7c8-126ad553221d",
		Target:          &tasks.Target{IP: "127.0.0.1"},
		Command:         `ssh {user}@127.0.0.1 "echo {password}"`,
		SensitiveValues: []string{"demo", "secret"},
		Reasoning:       "verify transcriptor audit",
		Status:          tasks.StatusCompleted,
		CreatedAt:       now,
		CompletedAt:     &now,
		Transcription: &transcriptor.Metadata{
			Transcribed:   true,
			CommandFamily: transcriptor.KindSSHRemoteCommand,
			Strategy:      transcriptor.StrategyGoSSHClient,
		},
		ExecutionPlan: &transcriptor.Plan{Kind: transcriptor.KindSSHRemoteCommand, SensitiveValues: []string{"demo", "secret"}},
		History:       []tasks.LifecycleEvent{{Status: tasks.StatusCreated, At: now}, {Status: tasks.StatusCompleted, At: now}},
	}
	if err := fs.WriteJSON(task.TaskID, task); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "transcribed.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"secret", "demo", "ExecutionPlan", "SensitiveValues"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persisted task leaked %q in:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, "{password}") || !strings.Contains(text, "transcription") {
		t.Fatalf("persisted task missing canonical audit data:\n%s", text)
	}
}
