package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
)

type testTaskStore struct {
	saved []tasks.Task
}

func (s *testTaskStore) LoadAll() ([]tasks.Task, error) { return s.saved, nil }

func (s *testTaskStore) Save(task tasks.Task) error {
	s.saved = append(s.saved, task)
	return nil
}

func TestIndexingTaskStorePersistsBeforeBestEffortIndex(t *testing.T) {
	base := &testTaskStore{}
	indexer := &recordingIndexer{err: errors.New("sqlite unavailable")}
	agentRegistry := agents.NewRegistry([]agents.Agent{{ID: "agent-1", UserIDs: []string{"user-1"}}})
	store := NewIndexingTaskStore(base, indexer, agentRegistry, targets.NewRegistry(nil), nil)
	task := tasks.Task{TaskID: "task-1", AgentID: "agent-1", Command: "show version", Status: tasks.StatusCreated, CreatedAt: time.Now().UTC()}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	if len(base.saved) != 1 {
		t.Fatalf("saved tasks = %d", len(base.saved))
	}
	if indexer.commands != 1 {
		t.Fatalf("indexed commands = %d", indexer.commands)
	}
}

func TestBuildIndexedCommandUsesSafeResultProjection(t *testing.T) {
	exitStatus := 0
	indexed := BuildIndexedCommand(tasks.Task{
		TaskID:    "task-safe-result",
		AgentID:   "agent-1",
		Command:   "show version",
		Status:    tasks.StatusCompleted,
		CreatedAt: time.Now().UTC(),
		Result: &tasks.TaskResult{
			Stdout:     "raw stdout must not be indexed",
			Stderr:     "raw stderr must not be indexed",
			ExitStatus: &exitStatus,
		},
	}, agents.Agent{ID: "agent-1"}, targets.Registry{})
	if indexed.Item.Result != "success" {
		t.Fatalf("result = %q", indexed.Item.Result)
	}
	if indexed.Item.Command != "show version" {
		t.Fatalf("raw command output leaked into projection: %#v", indexed.Item)
	}
}

func TestBuildIndexedCommandRecordsTargetVisibilitySource(t *testing.T) {
	targetRegistry := targets.NewRegistry([]targets.Target{{
		ID:       "target-1",
		Hostname: "server.example",
		UserIDs:  []string{"target-user"},
	}})
	indexed := BuildIndexedCommand(tasks.Task{
		TaskID:    "task-target-source",
		AgentID:   "agent-1",
		Target:    &tasks.Target{Hostname: "server.example"},
		Command:   "hostname",
		Status:    tasks.StatusCompleted,
		CreatedAt: time.Now().UTC(),
	}, agents.Agent{ID: "agent-1", UserIDs: []string{"agent-user"}}, targetRegistry)
	if indexed.Item.TargetID != "target-1" {
		t.Fatalf("target id = %q", indexed.Item.TargetID)
	}
	if len(indexed.AgentUserIDs) != 1 || indexed.AgentUserIDs[0] != "agent-user" {
		t.Fatalf("agent users = %#v", indexed.AgentUserIDs)
	}
	if len(indexed.TargetUserIDs) != 1 || indexed.TargetUserIDs[0] != "target-user" {
		t.Fatalf("target users = %#v", indexed.TargetUserIDs)
	}
}

type recordingIndexer struct {
	commands int
	err      error
}

func (i *recordingIndexer) UpsertCommand(_ context.Context, _ IndexedCommand) error {
	i.commands++
	return i.err
}

func (i *recordingIndexer) UpsertInteraction(_ context.Context, _ IndexedInteraction) error {
	return nil
}

func (i *recordingIndexer) DeleteAll(_ context.Context) error { return nil }
