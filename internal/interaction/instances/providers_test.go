package instances

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxyma/internal/interaction"
)

const providerTestInstanceID = "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"

func TestBuildProvidersAlwaysIncludesLocal(t *testing.T) {
	providers, err := BuildProviders(Registry{}, ProviderRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if providers[interaction.DefaultInstanceLocal] == nil {
		t.Fatal("local provider was not built")
	}
}

func TestBuildProvidersIncludesOnlyEnabledNtfyInstances(t *testing.T) {
	enabled := validProviderTestNtfyInstance("https://ntfy.example")
	disabled := validProviderTestNtfyInstance("https://disabled.example")
	disabled.ID = "c4511253-0d20-4be4-8d3b-58dfdd6aa616"
	disabled.Enabled = false
	registry := Registry{
		enabled: map[string]InstanceDefinition{enabled.ID: enabled},
		all:     map[string]InstanceDefinition{enabled.ID: enabled, disabled.ID: disabled},
	}

	providers, err := BuildProviders(registry, ProviderRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if providers[enabled.ID] == nil {
		t.Fatal("enabled ntfy provider was not built")
	}
	if _, found := providers[disabled.ID]; found {
		t.Fatal("disabled ntfy provider was built")
	}
}

func TestBuildProvidersPassesRuntimeOptionsToNtfy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"published-message"}`)
	}))
	defer server.Close()

	instance := validProviderTestNtfyInstance(server.URL)
	registry := Registry{enabled: map[string]InstanceDefinition{instance.ID: instance}}
	var logs bytes.Buffer
	providers, err := BuildProviders(registry, ProviderRuntimeOptions{
		Debug:  true,
		Logger: log.New(&logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := providers[instance.ID].Send(context.Background(), interaction.Interaction{
		InteractionID: "interaction-one",
		Kind:          interaction.KindApprovalRequest,
		Subject:       "test",
		Message:       "test message",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Reference != "published-message" {
		t.Fatalf("delivery reference = %q", delivery.Reference)
	}
	if output := logs.String(); !strings.Contains(output, "debug ntfy request") || !strings.Contains(output, server.URL) {
		t.Fatalf("debug log did not use configured logger: %q", output)
	}
}

func TestBuildProvidersErrorIdentifiesProviderAndInstance(t *testing.T) {
	instance := validProviderTestNtfyInstance("")
	registry := Registry{enabled: map[string]InstanceDefinition{instance.ID: instance}}

	_, err := BuildProviders(registry, ProviderRuntimeOptions{})
	if err == nil {
		t.Fatal("expected invalid ntfy configuration to fail")
	}
	message := err.Error()
	if !strings.Contains(message, ProviderNtfy) || !strings.Contains(message, instance.ID) {
		t.Fatalf("error does not identify provider and instance: %q", message)
	}
}

func TestBuildProvidersRejectsUnsupportedDefinitionType(t *testing.T) {
	definition := providerTestDefinition{metadata: InstanceMetadata{
		ID:           "c4511253-0d20-4be4-8d3b-58dfdd6aa616",
		ProviderType: "teams",
		Enabled:      true,
	}}
	registry := Registry{enabled: map[string]InstanceDefinition{definition.metadata.ID: definition}}

	_, err := BuildProviders(registry, ProviderRuntimeOptions{})
	if err == nil {
		t.Fatal("expected unsupported definition type to fail")
	}
	message := err.Error()
	if !strings.Contains(message, definition.metadata.ProviderType) || !strings.Contains(message, definition.metadata.ID) {
		t.Fatalf("error does not identify provider and instance: %q", message)
	}
}

func TestFailedProviderBuildDoesNotReplaceRouterProviders(t *testing.T) {
	existing := &recordingProvider{}
	router := NewInteractionRouter(map[string]interaction.Provider{
		interaction.DefaultInstanceLocal: existing,
	})
	invalid := validProviderTestNtfyInstance("")
	registry := Registry{enabled: map[string]InstanceDefinition{invalid.ID: invalid}}

	providers, err := BuildProviders(registry, ProviderRuntimeOptions{})
	if err == nil {
		router.SetProviders(providers)
		t.Fatal("expected invalid replacement provider configuration to fail")
	}
	if _, err := router.Send(context.Background(), interaction.Interaction{InteractionID: "still-routed"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(existing.sent) != 1 || existing.sent[0] != "still-routed" {
		t.Fatalf("existing provider calls = %#v", existing.sent)
	}
}

func validProviderTestNtfyInstance(baseURL string) NtfyInstance {
	return NtfyInstance{
		ID:            providerTestInstanceID,
		Enabled:       true,
		BaseURL:       baseURL,
		RequestTopic:  "requests",
		ResponseTopic: "responses",
	}
}

type providerTestDefinition struct {
	metadata InstanceMetadata
}

func (d providerTestDefinition) Metadata() InstanceMetadata {
	return d.metadata
}
