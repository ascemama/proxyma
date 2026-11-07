package sqlite

import (
	"strings"
	"sync"
	"testing"
	"time"

	"proxyma/internal/tasks"
)

func TestCommandStoreSaveUpdateAndRecover(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewCommandStore()
	created := time.Now().UTC().Truncate(time.Microsecond)
	task := tasks.Task{
		TaskID: "task-1", ClientOperationID: "operation-1", ClientRequestHash: "hash-1",
		AgentID: "agent-1", AgentName: "Agent One", Target: &tasks.Target{ID: "target-1", Hostname: "host.example", IP: "10.0.0.1"},
		Command: "hostname", Reasoning: "inventory", InteractionInstanceID: "local", Status: tasks.StatusCreated, CreatedAt: created,
		Metadata: map[string]any{"source": "test"},
	}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	task.Status = tasks.StatusCompleted
	task.Result = &tasks.TaskResult{Stdout: "host.example\n"}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].Status != tasks.StatusCompleted || recovered[0].Target.ID != "target-1" || recovered[0].AgentName != "Agent One" {
		t.Fatalf("recovered = %#v", recovered)
	}
	if recovered[0].Result == nil || recovered[0].Result.Stdout != "host.example\n" || recovered[0].Metadata["source"] != "test" {
		t.Fatalf("recovered task data = %#v", recovered[0])
	}
}

func TestCommandStoreIdempotencyConflictAndInjectionPayload(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewCommandStore()
	base := tasks.Task{TaskID: "task-1", ClientOperationID: "operation-1", ClientRequestHash: "hash-1", AgentID: "agent-1", Command: "' OR 1=1 --", Reasoning: "test", InteractionInstanceID: "local", Status: tasks.StatusCreated, CreatedAt: time.Now().UTC()}
	if err := store.Save(base); err != nil {
		t.Fatal(err)
	}
	conflict := base
	conflict.TaskID = "task-2"
	conflict.ClientRequestHash = "different"
	if err := store.Save(conflict); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("idempotency conflict error = %v", err)
	}
	recovered, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].Command != "' OR 1=1 --" {
		t.Fatalf("injection payload changed command rows = %#v", recovered)
	}
	var tableCount int
	if err := database.sqlDB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'commands'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 1 {
		t.Fatalf("commands table count = %d", tableCount)
	}
}

func TestCommandStoreConcurrentSaves(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := database.NewCommandStore()
	var wait sync.WaitGroup
	errs := make(chan error, 16)
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errs <- store.Save(tasks.Task{TaskID: "concurrent-" + string(rune('a'+index)), AgentID: "agent", Command: "echo", Reasoning: "test", InteractionInstanceID: "local", Status: tasks.StatusCreated, CreatedAt: time.Now().UTC()})
		}(index)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 16 {
		t.Fatalf("concurrent recovered rows = %d", len(recovered))
	}
}
