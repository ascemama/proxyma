package sqlitehistory

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"proxyma/internal/interaction/history"
)

func TestStoreListsCommandsAndInteractionsTogether(t *testing.T) {
	store, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	if err := store.UpsertCommand(context.Background(), history.IndexedCommand{
		Item: history.HistoryItem{
			HistoryID:      "command:task-1",
			EntryType:      history.EntryTypeCommand,
			TaskID:         "task-1",
			AgentID:        "agent-a",
			Status:         "completed",
			Command:        "systemctl restart nginx",
			Result:         "success",
			Target:         "server-1",
			TargetHostname: "server-1",
			CreatedAt:      base.Add(2 * time.Minute),
			TaskCreatedAt:  base,
		},
		VisibleUserIDs: []string{"user-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertInteraction(context.Background(), indexedHistoryItem("int-1", "agent-a", base.Add(time.Minute), []string{"user-1"}, "creator")); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].EntryType != history.EntryTypeCommand || page.Items[1].EntryType != history.EntryTypeInteraction {
		t.Fatalf("mixed page = %#v", page.Items)
	}
	textPage, err := store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-1", Text: "nginx", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(textPage.Items) != 1 || textPage.Items[0].HistoryID != "command:task-1" {
		t.Fatalf("text page = %#v", textPage.Items)
	}
}

func TestStoreMigratesLegacyInteractionSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
create table interaction_history (
	interaction_id text primary key, task_id text, agent_id text, agent_name text,
	interaction_instance_id text, interaction_instance_creator_user_id text, provider text,
	kind text, expected_response text, status text, subject text, message text,
	requested_by text, approved integer, answer text, responded_by text, cancel_reason text,
	target_hostname text, target_ip text, command text, created_at text, expires_at text,
	responded_at text, task_created_at text, record_json text
);
create table interaction_history_visible_users (interaction_id text, user_id text, primary key (interaction_id, user_id));
create virtual table interaction_history_fts using fts5(interaction_id unindexed, subject, message, command, requested_by, target_hostname, target_ip);
insert into interaction_history values ('int-legacy', 'task-legacy', 'agent-a', 'Agent A', 'local', 'creator', 'local', 'approval_request', 'approval', 'answered', 'Legacy', 'Legacy message', 'operator', 1, 'yes', 'operator', '', 'server-1', '', 'show version', '2026-06-25T10:00:00Z', '', '2026-06-25T10:01:00Z', '2026-06-25T09:59:00Z', '{}');
insert into interaction_history_visible_users values ('int-legacy', 'user-1');
insert into interaction_history_fts values ('int-legacy', 'Legacy', 'Legacy message', 'show version', 'operator', 'server-1', '');`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].HistoryID != "interaction:int-legacy" || page.Items[0].EntryType != history.EntryTypeInteraction {
		t.Fatalf("migrated page = %#v", page.Items)
	}
}

func TestStoreMigratesV2VisibilitySources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history-v2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
create table history_entries (
	history_id text primary key, entry_type text not null, interaction_id text,
	task_id text, agent_id text, agent_name text, interaction_instance_id text,
	interaction_instance_creator_user_id text, provider text, kind text,
	expected_response text, status text not null, subject text, message text,
	requested_by text, approved integer, answer text, responded_by text,
	cancel_reason text, target_hostname text, target_ip text, command text not null default '',
	result text, policy_justification text, created_at text not null, completed_at text,
	expires_at text, responded_at text, task_created_at text, record_json text
);
create table history_entries_visible_users (history_id text not null, user_id text not null, primary key (history_id, user_id));
create virtual table history_entries_fts using fts5(history_id unindexed, entry_type unindexed, subject, message, command, result, requested_by, target_hostname, target_ip);
insert into history_entries(history_id, entry_type, task_id, agent_id, status, command, created_at)
values ('command:v2', 'command', 'v2', 'agent-v2', 'completed', 'hostname', '2026-06-25T10:00:00Z');
insert into history_entries_visible_users values ('command:v2', 'user-v2');
pragma user_version = 2;`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`pragma user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d", version)
	}
	var sourceType, sourceID string
	if err := store.db.QueryRow(`select source_type, source_id from history_entries_visible_users where history_id = 'command:v2'`).Scan(&sourceType, &sourceID); err != nil {
		t.Fatal(err)
	}
	if sourceType != "agent" || sourceID != "agent-v2" {
		t.Fatalf("migrated visibility source = %s/%s", sourceType, sourceID)
	}
}

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

func TestStoreSyncsAgentAndTargetVisibilityWithoutDuplicateRows(t *testing.T) {
	store, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	command := history.IndexedCommand{
		Item: history.HistoryItem{
			HistoryID:     "command:task-visibility",
			EntryType:     history.EntryTypeCommand,
			TaskID:        "task-visibility",
			AgentID:       "agent-a",
			TargetID:      "target-a",
			Status:        "completed",
			Command:       "hostname",
			CreatedAt:     created,
			TaskCreatedAt: created,
		},
		AgentUserIDs:  []string{"user-agent"},
		TargetUserIDs: []string{"user-target", "user-agent"},
	}
	if err := store.UpsertCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	interaction := indexedHistoryItem("int-visibility", "agent-a", created.Add(time.Minute), []string{"user-agent"}, "creator")
	interaction.Item.TargetID = "target-a"
	if err := store.UpsertInteraction(context.Background(), interaction); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("agent-visible rows = %#v", page.Items)
	}

	if err := store.SyncAgentVisibility(context.Background(), "agent-a", []string{"user-new-agent"}); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].EntryType != history.EntryTypeCommand {
		t.Fatalf("target-only command after agent sync = %#v", page.Items)
	}
	page, err = store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-new-agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("new agent-visible rows = %#v", page.Items)
	}

	if err := store.SyncTargetVisibility(context.Background(), "target-a", nil); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("removed target user still sees history = %#v", page.Items)
	}
	page, err = store.ListHistory(context.Background(), history.HistoryQuery{CurrentUserID: "user-new-agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("agent user lost rows after target sync = %#v", page.Items)
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
