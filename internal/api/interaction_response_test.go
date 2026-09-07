package api

import (
	"strings"
	"testing"

	"proxyma/internal/agents"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/users"
)

func TestUserInteractionsResponseRejectsUnsupportedDefinition(t *testing.T) {
	definition := apiTestInteractionDefinition{metadata: interactioninstances.InstanceMetadata{
		ID:           "c4511253-0d20-4be4-8d3b-58dfdd6aa616",
		ProviderType: "teams",
		Enabled:      true,
	}}

	_, err := userInteractionsResponse(
		[]interactioninstances.InstanceDefinition{definition},
		users.NewRegistry(nil),
		agents.NewRegistry(nil),
	)
	if err == nil {
		t.Fatal("expected unsupported definition to fail")
	}
	if message := err.Error(); !strings.Contains(message, definition.metadata.ProviderType) || !strings.Contains(message, definition.metadata.ID) {
		t.Fatalf("error does not identify provider and instance: %q", message)
	}
}

type apiTestInteractionDefinition struct {
	metadata interactioninstances.InstanceMetadata
}

func (d apiTestInteractionDefinition) Metadata() interactioninstances.InstanceMetadata {
	return d.metadata
}
