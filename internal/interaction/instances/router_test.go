package instances

import (
	"context"
	"testing"

	"proxyma/internal/interaction"
)

type recordingProvider struct {
	sent     []string
	canceled []string
}

type managedRecordingProvider struct {
	recordingProvider
	tracked   []string
	untracked []string
	closed    bool
}

func (p *managedRecordingProvider) TrackPending(request interaction.Interaction, _ interaction.Responder) {
	p.tracked = append(p.tracked, request.InteractionID)
}

func (p *managedRecordingProvider) UntrackPending(request interaction.Interaction) {
	p.untracked = append(p.untracked, request.InteractionID)
}

func (p *managedRecordingProvider) Close() {
	p.closed = true
}

func (p *recordingProvider) Send(_ interaction.Context, request interaction.Interaction, _ interaction.Responder) (*interaction.Delivery, error) {
	p.sent = append(p.sent, request.InteractionID)
	return &interaction.Delivery{Provider: "recording"}, nil
}

func (p *recordingProvider) Cancel(_ interaction.Context, request interaction.Interaction) error {
	p.canceled = append(p.canceled, request.InteractionID)
	return nil
}

func TestRouterRoutesByInteractionInstanceID(t *testing.T) {
	local := &recordingProvider{}
	ops := &recordingProvider{}
	opsID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	router := NewInteractionRouter(map[string]interaction.Provider{
		"local": local,
		opsID:   ops,
	})

	if _, err := router.Send(context.Background(), interaction.Interaction{InteractionID: "one"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Send(context.Background(), interaction.Interaction{InteractionID: "two", InteractionInstanceID: opsID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := router.Cancel(context.Background(), interaction.Interaction{InteractionID: "three", InteractionInstanceID: opsID}); err != nil {
		t.Fatal(err)
	}

	if len(local.sent) != 1 || local.sent[0] != "one" {
		t.Fatalf("local sent = %#v", local.sent)
	}
	if len(ops.sent) != 1 || ops.sent[0] != "two" {
		t.Fatalf("ops sent = %#v", ops.sent)
	}
	if len(ops.canceled) != 1 || ops.canceled[0] != "three" {
		t.Fatalf("ops canceled = %#v", ops.canceled)
	}
}

func TestRouterRejectsUnknownInstance(t *testing.T) {
	router := NewInteractionRouter(map[string]interaction.Provider{"local": &recordingProvider{}})
	if _, err := router.Send(context.Background(), interaction.Interaction{InteractionInstanceID: "missing"}, nil); err == nil {
		t.Fatal("expected unknown interaction instance to be rejected")
	}
}

func TestRouterDrainsOriginalProviderAcrossReload(t *testing.T) {
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	oldProvider := &managedRecordingProvider{}
	newProvider := &managedRecordingProvider{}
	router := NewInteractionRouter(map[string]interaction.Provider{instanceID: oldProvider})
	pending := interaction.Interaction{InteractionID: "pending", InteractionInstanceID: instanceID}
	router.TrackPending(pending, nil)

	router.SetProviders(map[string]interaction.Provider{instanceID: newProvider})
	if oldProvider.closed {
		t.Fatal("original provider closed before its pending interaction drained")
	}
	if err := router.Cancel(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	if len(oldProvider.canceled) != 1 || oldProvider.canceled[0] != pending.InteractionID {
		t.Fatalf("original provider canceled = %#v", oldProvider.canceled)
	}

	router.UntrackPending(pending)
	if !oldProvider.closed {
		t.Fatal("original provider was not closed after draining")
	}
	if newProvider.closed {
		t.Fatal("replacement provider unexpectedly closed")
	}
}

func TestRouterTracksNewInteractionsOnReplacementProvider(t *testing.T) {
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	oldProvider := &managedRecordingProvider{}
	newProvider := &managedRecordingProvider{}
	router := NewInteractionRouter(map[string]interaction.Provider{instanceID: oldProvider})
	router.SetProviders(map[string]interaction.Provider{instanceID: newProvider})
	request := interaction.Interaction{InteractionID: "new", InteractionInstanceID: instanceID}

	if _, err := router.Send(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if len(newProvider.sent) != 1 || len(newProvider.tracked) != 1 {
		t.Fatalf("replacement provider sent=%#v tracked=%#v", newProvider.sent, newProvider.tracked)
	}
	if !oldProvider.closed {
		t.Fatal("idle original provider was not closed during reload")
	}
}
