package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/interaction/history"
	"proxyma/internal/tasks"
)

func TestHistoryStoreFiltersSearchesAndPaginatesCanonicalRows(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	commands := database.NewCommandStore()
	interactions := database.NewInteractionStore()
	base := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC)
	for index, command := range []string{"uname -a", "systemctl status nginx", "systemctl restart nginx"} {
		id := "task-" + string(rune('1'+index))
		if err := commands.Save(tasks.Task{TaskID: id, AgentID: "agent-1", AgentName: "Agent One", Target: &tasks.Target{ID: "target-1", Hostname: "server.example"}, Command: command, Reasoning: "inventory", InteractionInstanceID: "local", Status: tasks.StatusCompleted, CreatedAt: base.Add(time.Duration(index) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := interactions.Save(interaction.Interaction{InteractionID: "interaction-1", TaskID: "task-2", InteractionInstanceID: "local", Kind: interaction.KindApprovalRequest, ExpectedResponse: interaction.ExpectedApproval, Subject: "Approve nginx", Message: "Approve nginx restart", HarnessName: "agent-1", Status: interaction.StatusAnswered, CreatedAt: base.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	store := database.NewHistoryStore()
	page, err := store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{Admin: true}, EntryType: history.EntryTypeCommand, Text: "nginx", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Command != "systemctl restart nginx" || page.Items[1].Command != "systemctl status nginx" {
		t.Fatalf("FTS page = %#v", page)
	}

	page, err = store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{AgentIDs: []string{"agent-1"}}, EntryType: history.EntryTypeCommand, CommandPattern: "systemctl * nginx", CommandPatternMode: history.CommandPatternGlob, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("glob page = %#v", page)
	}
	next, err := store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{AgentIDs: []string{"agent-1"}}, EntryType: history.EntryTypeCommand, CommandPattern: "systemctl * nginx", CommandPatternMode: history.CommandPatternGlob, Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].HistoryID == page.Items[0].HistoryID {
		t.Fatalf("cursor page = %#v", next)
	}

	regexPage, err := store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{Admin: true}, EntryType: history.EntryTypeCommand, CommandPattern: `^uname`, CommandPatternMode: history.CommandPatternRegex, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(regexPage.Items) != 1 || regexPage.Items[0].Command != "uname -a" {
		t.Fatalf("regex page = %#v", regexPage)
	}
}

func TestHistoryStoreVisibilityAndSQLInjectionPayloads(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	commands := database.NewCommandStore()
	for _, task := range []tasks.Task{
		{TaskID: "owned", AgentID: "agent-owned", Target: &tasks.Target{ID: "target-hidden"}, Command: "safe command", Reasoning: "test", InteractionInstanceID: "local", Status: tasks.StatusCompleted, CreatedAt: time.Now().UTC()},
		{TaskID: "hidden", AgentID: "agent-hidden", Target: &tasks.Target{ID: "target-hidden"}, Command: "admin-only", Reasoning: "test", InteractionInstanceID: "local", Status: tasks.StatusCompleted, CreatedAt: time.Now().UTC().Add(time.Minute)},
	} {
		if err := commands.Save(task); err != nil {
			t.Fatal(err)
		}
	}
	store := database.NewHistoryStore()
	for _, payload := range []string{"' OR 1=1 --", `"; DROP TABLE commands; --`, `admin'/*`} {
		page, err := store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{AgentIDs: []string{payload}}, Text: payload, Limit: 10})
		if err != nil {
			t.Fatalf("payload %q: %v", payload, err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("payload %q returned rows: %#v", payload, page.Items)
		}
	}
	var count int
	if err := database.sqlDB().QueryRow(`SELECT COUNT(*) FROM commands`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("commands changed after injection payloads: %d", count)
	}
	if !strings.Contains(string(ftsQuery(`" OR 1=1 --`)), `""`) {
		t.Fatal("FTS query was not quoted")
	}
	page, err := store.ListHistory(context.Background(), history.HistoryQuery{Visibility: history.VisibilityScope{AgentIDs: []string{"agent-owned"}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TaskID != "owned" {
		t.Fatalf("visibility page = %#v", page)
	}
}
