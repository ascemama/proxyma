package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxyma/internal/agents"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/users"
)

func TestValidatePolicyReferencesRejectsUnknownTargetPolicy(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Hostname: "target.example", PolicyIDs: []string{"missing"}},
	})
	policies := policy.NewSet(map[string]policy.Policy{
		"baseline": {ID: "baseline", Rules: []policy.Rule{{ID: "default", Effect: policy.DecisionApproval, Always: true}}},
	})

	diagnostics := validatePolicyReferences(registry, policies)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, `unknown policy id "missing"`) {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}

func TestValidateAgentInteractionReferencesRejectsUnknownInstance(t *testing.T) {
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}, InteractionInstanceID: "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"},
	})

	diagnostics := validateAgentInteractionReferences(agentRegistry, interactioninstances.Registry{})
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, `unknown or disabled interaction instance id "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"`) {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}

func TestValidateAgentInteractionReferencesAcceptsDefaultLocal(t *testing.T) {
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}},
	})

	if diagnostics := validateAgentInteractionReferences(agentRegistry, interactioninstances.Registry{}); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateAgentInteractionReferencesAcceptsEnabledNtfyInstance(t *testing.T) {
	root := t.TempDir()
	ntfyDir := filepath.Join(root, "ntfy")
	if err := os.MkdirAll(ntfyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ntfyDir, "ops.json"), []byte(`{
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "creator_user_id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, diagnostics, err := interactioninstances.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("load diagnostics = %#v", diagnostics)
	}
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}, InteractionInstanceID: "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"},
	})

	if diagnostics := validateAgentInteractionReferences(agentRegistry, registry); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateAgentInteractionReferencesRejectsDisabledInstance(t *testing.T) {
	root := t.TempDir()
	ntfyDir := filepath.Join(root, "ntfy")
	if err := os.MkdirAll(ntfyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ntfyDir, "disabled.json"), []byte(`{
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "creator_user_id": "fcd2f3a9-d546-44f4-837b-0c7aa4219752",
  "enabled": false,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, diagnostics, err := interactioninstances.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("load diagnostics = %#v", diagnostics)
	}
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}, InteractionInstanceID: "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"},
	})

	if diagnostics := validateAgentInteractionReferences(agentRegistry, registry); len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateInteractionCreatorReferencesRejectsUnknownUser(t *testing.T) {
	root := t.TempDir()
	ntfyDir := filepath.Join(root, "ntfy")
	if err := os.MkdirAll(ntfyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ntfyDir, "ops.json"), []byte(`{
  "id": "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
  "creator_user_id": "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "requests",
  "response_topic": "responses",
  "poll_interval": "2s",
  "poll_timeout": "3s"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, diagnostics, err := interactioninstances.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("load diagnostics = %#v", diagnostics)
	}
	userRegistry := users.NewRegistry([]users.User{
		{ID: "fcd2f3a9-d546-44f4-837b-0c7aa4219752", Username: "demo-admin", Name: "Demo Admin"},
	})

	creatorDiagnostics := validateInteractionCreatorReferences(registry, userRegistry)
	if len(creatorDiagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", creatorDiagnostics)
	}
	if !strings.Contains(creatorDiagnostics[0].Message, `unknown creator user id "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"`) {
		t.Fatalf("diagnostic = %#v", creatorDiagnostics[0])
	}
}

func TestValidateUserReferencesRejectsUnknownTargetUser(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Hostname: "target.example", UserIDs: []string{"missing-user"}},
	})
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}},
	})
	userRegistry := users.NewRegistry([]users.User{
		{ID: "fcd2f3a9-d546-44f4-837b-0c7aa4219752", Name: "Demo Admin"},
	})

	diagnostics := validateUserReferences(registry, agentRegistry, userRegistry)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, `unknown user id "missing-user"`) {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}

func TestValidateUserReferencesRejectsUnknownAgentUser(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Hostname: "target.example", UserIDs: []string{"fcd2f3a9-d546-44f4-837b-0c7aa4219752"}},
	})
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{"missing-user"}},
	})
	userRegistry := users.NewRegistry([]users.User{
		{ID: "fcd2f3a9-d546-44f4-837b-0c7aa4219752", Name: "Demo Admin"},
	})

	diagnostics := validateUserReferences(registry, agentRegistry, userRegistry)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, `unknown user id "missing-user"`) {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}

func TestValidateAgentReferencesRejectsUnknownTargetAgent(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Hostname: "target.example", AgentIDs: []string{"missing-agent"}},
	})
	agentRegistry := agents.NewRegistry([]agents.Agent{
		{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", AgentJWTSigningKey: "signing-key", Enabled: true},
	})

	diagnostics := validateAgentReferences(registry, agentRegistry)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, `unknown agent id "missing-agent"`) {
		t.Fatalf("diagnostic = %#v", diagnostics[0])
	}
}
