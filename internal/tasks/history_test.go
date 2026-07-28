package tasks

import (
	"testing"
	"time"

	"proxyma/internal/executor"
)

func TestLifecycleHistoryRecordsTransitions(t *testing.T) {
	manager := NewManager(nil, executor.Executor{})
	now := time.Date(2026, 5, 14, 1, 2, 3, 0, time.UTC)
	manager.now = func() time.Time { return now }
	task := &Task{TaskID: "t", Status: StatusRunning}
	manager.transition(task, StatusCompleted, "done", &TaskResult{Truncated: false})
	if len(task.History) != 1 {
		t.Fatalf("history length = %d", len(task.History))
	}
	if task.History[0].Status != StatusCompleted || task.History[0].Message != "done" {
		t.Fatalf("history = %#v", task.History)
	}
	if task.CompletedAt == nil || !task.CompletedAt.Equal(now) {
		t.Fatalf("completed_at = %#v", task.CompletedAt)
	}
}
