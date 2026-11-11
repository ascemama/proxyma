package sqlite

import (
	"testing"
	"time"

	"proxyma/internal/interaction"
	"proxyma/internal/tasks"
)

func TestInteractionStoreSaveUpdateAndRecover(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	commands := database.NewCommandStore()
	if err := commands.Save(tasks.Task{TaskID: "task-1", AgentID: "agent-1", Target: &tasks.Target{ID: "target-1"}, Command: "approve", Reasoning: "test", InteractionInstanceID: "local", Status: tasks.StatusCreated, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	store := database.NewInteractionStore()
	created := time.Now().UTC().Truncate(time.Microsecond)
	record := interaction.Interaction{
		InteractionID: "interaction-1", TaskID: "task-1", InteractionInstanceID: "local", Kind: interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval, Subject: "Approve", Message: "' OR 1=1 --", HarnessName: "agent-1",
		Status: interaction.StatusPending, CreatedAt: created,
	}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	approved := true
	answeredAt := created.Add(time.Minute)
	record.Status = interaction.StatusAnswered
	record.Response = &interaction.Response{InteractionID: record.InteractionID, Approved: &approved, RespondedBy: "operator", RespondedAt: &answeredAt}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].Status != interaction.StatusAnswered || recovered[0].Response == nil || recovered[0].Response.RespondedBy != "operator" {
		t.Fatalf("recovered = %#v", recovered)
	}
}

func TestInteractionStoreEnforcesCommandForeignKey(t *testing.T) {
	database, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	record := interaction.Interaction{InteractionID: "interaction-1", TaskID: "missing-task", InteractionInstanceID: "local", Kind: interaction.KindQuestion, ExpectedResponse: interaction.ExpectedText, Subject: "Question", Message: "Message", Status: interaction.StatusPending, CreatedAt: time.Now().UTC()}
	if err := database.NewInteractionStore().Save(record); err == nil {
		t.Fatal("expected foreign-key error")
	}
}
