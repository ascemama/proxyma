package sqlitehistory

import (
	"context"
	"testing"
	"time"

	"proxyma/internal/interaction/history"
)

func TestStoreFiltersByVisibleUsersNotCreator(t *testing.T) {
	store, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	if err := store.UpsertInteraction(context.Background(), indexedHistoryItem("int-1", "agent-a", created, []string{"visible-user"}, "creator-user")); err != nil {
		t.Fatal(err)
	}

	creatorPage, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserID:      "creator-user",
		CurrentUserIsAdmin: false,
		CommandPatternMode: history.CommandPatternLiteral,
		Limit:              50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(creatorPage.Items) != 0 {
		t.Fatalf("creator should not see row through creator metadata: %#v", creatorPage.Items)
	}

	visiblePage, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserID:      "visible-user",
		CurrentUserIsAdmin: false,
		CommandPatternMode: history.CommandPatternLiteral,
		Limit:              50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(visiblePage.Items) != 1 || visiblePage.Items[0].InteractionID != "int-1" {
		t.Fatalf("visible user page = %#v", visiblePage.Items)
	}

	adminCreatorPage, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserIsAdmin:               true,
		InteractionInstanceCreatorUserID: "creator-user",
		CommandPatternMode:               history.CommandPatternLiteral,
		Limit:                            50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(adminCreatorPage.Items) != 1 || adminCreatorPage.Items[0].InteractionID != "int-1" {
		t.Fatalf("admin creator filter page = %#v", adminCreatorPage.Items)
	}
}

func TestStoreFiltersAndPaginates(t *testing.T) {
	store, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	rows := []history.IndexedInteraction{
		indexedHistoryItem("int-3", "agent-a", base.Add(3*time.Minute), []string{"user-1"}, "creator-1"),
		indexedHistoryItem("int-2", "agent-b", base.Add(2*time.Minute), []string{"user-1"}, "creator-1"),
		indexedHistoryItem("int-1", "agent-a", base.Add(time.Minute), []string{"user-1"}, "creator-1"),
	}
	rows[0].Item.Status = "answered"
	rows[0].Item.Command = "show running-config"
	rows[1].Item.Status = "expired"
	rows[1].Item.Command = "reload"
	rows[2].Item.Status = "answered"
	rows[2].Item.Command = "show version"
	for _, row := range rows {
		if err := store.UpsertInteraction(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}

	page, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserID:      "user-1",
		CurrentUserIsAdmin: false,
		AgentID:            "agent-a",
		Status:             []string{"answered"},
		CommandPattern:     "show",
		CommandPatternMode: history.CommandPatternLiteral,
		Limit:              1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].InteractionID != "int-3" || page.NextCursor == "" {
		t.Fatalf("first page = %#v next=%q", page.Items, page.NextCursor)
	}
	next, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserID:      "user-1",
		CurrentUserIsAdmin: false,
		AgentID:            "agent-a",
		Status:             []string{"answered"},
		CommandPattern:     "show",
		CommandPatternMode: history.CommandPatternLiteral,
		Limit:              1,
		Cursor:             page.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].InteractionID != "int-1" {
		t.Fatalf("next page = %#v", next.Items)
	}
}

func TestStoreRegexCommandPattern(t *testing.T) {
	store, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	row := indexedHistoryItem("int-1", "agent-a", time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC), []string{"user-1"}, "creator-1")
	row.Item.Command = "systemctl restart nginx"
	if err := store.UpsertInteraction(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListInteractions(context.Background(), history.InteractionQuery{
		CurrentUserID:      "user-1",
		CurrentUserIsAdmin: false,
		AgentID:            "agent-a",
		CommandPattern:     `systemctl\s+restart`,
		CommandPatternMode: history.CommandPatternRegex,
		Limit:              50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("regex page = %#v", page.Items)
	}
}

func indexedHistoryItem(id, agentID string, created time.Time, visibleUsers []string, creatorUserID string) history.IndexedInteraction {
	approved := true
	return history.IndexedInteraction{
		Item: history.InteractionHistoryItem{
			InteractionID:                    id,
			TaskID:                           "task-" + id,
			AgentID:                          agentID,
			AgentName:                        agentID,
			InteractionInstanceID:            "instance-1",
			InteractionInstanceCreatorUserID: creatorUserID,
			Provider:                         "ntfy",
			Kind:                             "approval_request",
			ExpectedResponse:                 "approval",
			Status:                           "answered",
			Subject:                          "Approve command",
			Message:                          "Approve command execution",
			RequestedBy:                      "operator",
			Approved:                         &approved,
			RespondedBy:                      "operator",
			Target:                           "server-1",
			TargetHostname:                   "server-1",
			Command:                          "show version",
			CreatedAt:                        created,
			TaskCreatedAt:                    created.Add(-time.Minute),
		},
		VisibleUserIDs: visibleUsers,
		RecordJSON:     `{}`,
	}
}
