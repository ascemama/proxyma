package local

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"proxyma/internal/interaction"
)

type responder struct {
	responses chan interaction.Response
	cancels   chan string
}

func newResponder() *responder {
	return &responder{
		responses: make(chan interaction.Response, 8),
		cancels:   make(chan string, 8),
	}
}

func (r *responder) Respond(_ interaction.Context, response interaction.Response) (*interaction.Interaction, error) {
	r.responses <- response
	return &interaction.Interaction{InteractionID: response.InteractionID, Status: interaction.StatusAnswered, Response: &response}, nil
}

func (r *responder) Cancel(_ interaction.Context, interactionID string, reason string) (*interaction.Interaction, error) {
	r.cancels <- reason
	return &interaction.Interaction{InteractionID: interactionID, Status: interaction.StatusCanceled, CancelReason: reason}, nil
}

func TestProviderApprovesYes(t *testing.T) {
	var output bytes.Buffer
	provider := NewWithIO(strings.NewReader("yes\n"), &output)
	provider.now = func() time.Time {
		return time.Date(2026, 6, 4, 1, 0, 0, 0, time.UTC)
	}
	responder := newResponder()

	delivery, err := provider.Send(context.Background(), testInteraction("interaction-1"), responder)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Provider != ProviderName || delivery.Reference != "interaction-1" {
		t.Fatalf("delivery = %#v", delivery)
	}
	response := waitResponse(t, responder)
	if response.Approved == nil || !*response.Approved || response.RespondedBy != "local-console" {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(output.String(), "Proxyma command approval required") || !strings.Contains(output.String(), "Command: ssh {user}@127.0.0.1 hostname") {
		t.Fatalf("unexpected prompt:\n%s", output.String())
	}
}

func TestProviderRejectsNo(t *testing.T) {
	var output bytes.Buffer
	provider := NewWithIO(strings.NewReader("n\n"), &output)
	responder := newResponder()

	if _, err := provider.Send(context.Background(), testInteraction("interaction-1"), responder); err != nil {
		t.Fatal(err)
	}
	response := waitResponse(t, responder)
	if response.Approved == nil || *response.Approved {
		t.Fatalf("response = %#v", response)
	}
}

func TestProviderRepromptsInvalidInput(t *testing.T) {
	var output bytes.Buffer
	provider := NewWithIO(strings.NewReader("maybe\ny\n"), &output)
	responder := newResponder()

	if _, err := provider.Send(context.Background(), testInteraction("interaction-1"), responder); err != nil {
		t.Fatal(err)
	}
	response := waitResponse(t, responder)
	if response.Approved == nil || !*response.Approved {
		t.Fatalf("response = %#v", response)
	}
	if !strings.Contains(output.String(), "Please answer y or n.") {
		t.Fatalf("prompt did not reprompt:\n%s", output.String())
	}
}

func TestProviderCancelsOnEOF(t *testing.T) {
	var output bytes.Buffer
	provider := NewWithIO(strings.NewReader(""), &output)
	responder := newResponder()

	if _, err := provider.Send(context.Background(), testInteraction("interaction-1"), responder); err != nil {
		t.Fatal(err)
	}
	reason := waitCancel(t, responder)
	if reason != "local console input closed" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestProviderSerializesPrompts(t *testing.T) {
	var output bytes.Buffer
	provider := NewWithIO(strings.NewReader("y\nn\n"), &output)
	responder := newResponder()

	if _, err := provider.Send(context.Background(), testInteraction("interaction-1"), responder); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Send(context.Background(), testInteraction("interaction-2"), responder); err != nil {
		t.Fatal(err)
	}
	first := waitResponse(t, responder)
	second := waitResponse(t, responder)
	if first.InteractionID != "interaction-1" || first.Approved == nil || !*first.Approved {
		t.Fatalf("first = %#v", first)
	}
	if second.InteractionID != "interaction-2" || second.Approved == nil || *second.Approved {
		t.Fatalf("second = %#v", second)
	}
}

func testInteraction(id string) interaction.Interaction {
	return interaction.Interaction{
		InteractionID:    id,
		TaskID:           "task-1",
		Kind:             interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval,
		Subject:          "Approve command",
		Message:          "Run command?",
		HarnessName:      "agent-demo",
		Metadata: map[string]any{
			"target":    "127.0.0.1",
			"command":   "ssh {user}@127.0.0.1 hostname",
			"reasoning": "test",
		},
	}
}

func waitResponse(t *testing.T, responder *responder) interaction.Response {
	t.Helper()
	select {
	case response := <-responder.responses:
		return response
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
		return interaction.Response{}
	}
}

func waitCancel(t *testing.T, responder *responder) string {
	t.Helper()
	select {
	case reason := <-responder.cancels:
		return reason
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancel")
		return ""
	}
}
