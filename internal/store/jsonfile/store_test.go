package jsonfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/store"
	"proxyma/internal/tasks"
)

func TestTaskStoreLoadAllStartsEmpty(t *testing.T) {
	store := NewTaskStore(t.TempDir())
	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v", records)
	}
}

func TestTaskStoreSaveLoadAllAndReplace(t *testing.T) {
	dir := t.TempDir()
	store := NewTaskStore(dir)
	now := time.Date(2026, 6, 4, 8, 9, 10, 0, time.FixedZone("UTC+2", 2*60*60))
	first := tasks.Task{
		TaskID:    "task-1",
		Command:   "echo first",
		Reasoning: "test",
		Status:    tasks.StatusCreated,
		CreatedAt: now,
		History:   []tasks.LifecycleEvent{{Status: tasks.StatusCreated, At: now}},
	}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Command = "echo second"
	second.Status = tasks.StatusCompleted
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].Command != "echo second" || records[0].Status != tasks.StatusCompleted {
		t.Fatalf("record = %#v", records[0])
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Name() != "20260604-060910-task-1.json" {
		t.Fatalf("filename = %q", entries[0].Name())
	}
}

func TestInteractionStoreSaveLoadAllMultipleRecords(t *testing.T) {
	store := NewInteractionStore(t.TempDir())
	now := time.Now().UTC()
	records := []interaction.Interaction{
		{InteractionID: "interaction-1", Kind: interaction.KindQuestion, ExpectedResponse: interaction.ExpectedText, Subject: "One", Message: "First?", Status: interaction.StatusPending, CreatedAt: now},
		{InteractionID: "interaction-2", Kind: interaction.KindApprovalRequest, ExpectedResponse: interaction.ExpectedApproval, Subject: "Two", Message: "Second?", Status: interaction.StatusAnswered, CreatedAt: now},
	}
	for _, record := range records {
		if err := store.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded = %#v", loaded)
	}
	seen := map[string]bool{}
	for _, record := range loaded {
		seen[record.InteractionID] = true
	}
	for _, want := range []string{"interaction-1", "interaction-2"} {
		if !seen[want] {
			t.Fatalf("missing %s in %#v", want, loaded)
		}
	}
}

func TestInteractionStoreUsesTimestampPrefixedFileName(t *testing.T) {
	dir := t.TempDir()
	store := NewInteractionStore(dir)
	now := time.Date(2026, 6, 4, 9, 10, 11, 0, time.UTC)
	record := interaction.Interaction{
		InteractionID:    "interaction-1",
		Kind:             interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval,
		Subject:          "Approve",
		Message:          "Run?",
		Status:           interaction.StatusPending,
		CreatedAt:        now,
	}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Name() != "20260604-091011-interaction-1.json" {
		t.Fatalf("filename = %q", entries[0].Name())
	}
}

func TestTaskStoreRemovesLegacyRawIDFileOnSave(t *testing.T) {
	dir := t.TempDir()
	files := store.New(dir)
	task := tasks.Task{
		TaskID:    "task-1",
		Command:   "echo ok",
		Reasoning: "test",
		Status:    tasks.StatusCreated,
		CreatedAt: time.Date(2026, 6, 4, 9, 10, 11, 0, time.UTC),
	}
	if err := files.WriteJSON(task.TaskID, task); err != nil {
		t.Fatal(err)
	}
	if err := WrapTaskStore(files).Save(task); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "20260604-091011-task-1.json" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestTaskStoreReportsCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTaskStore(dir).LoadAll(); err == nil {
		t.Fatal("expected corrupt record error")
	}
}

func TestTaskStoreSanitizesPathLikeIDs(t *testing.T) {
	dir := t.TempDir()
	store := NewTaskStore(dir)
	task := tasks.Task{TaskID: `nested/task\id`, Command: "echo ok", Reasoning: "test", Status: tasks.StatusCreated, CreatedAt: time.Date(2026, 6, 4, 9, 10, 11, 0, time.UTC)}
	if err := store.Save(task); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if strings.ContainsAny(entries[0].Name(), `/\`) {
		t.Fatalf("unsanitized file name = %q", entries[0].Name())
	}
	if !strings.HasPrefix(entries[0].Name(), "20260604-091011-") {
		t.Fatalf("filename missing timestamp prefix = %q", entries[0].Name())
	}
}
