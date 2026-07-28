package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "codex.json", `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","display_name":"Codex","shared_secret":"secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"interaction_instance_id":"6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"}`)
	writeAgentFile(t, dir, "automation.json", `{"id":"7937dd2d-0b8a-4880-8afa-0ea60fd65a43","shared_secret":"other-secret","enabled":false,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	codex, ok := registry.Find("df9ec8bb-1a22-4181-a7c8-126ad553221d")
	if !ok {
		t.Fatal("expected agent not loaded")
	}
	if codex.InteractionInstanceID != "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8" {
		t.Fatalf("InteractionInstanceID = %q", codex.InteractionInstanceID)
	}
	if !registry.HasEnabled() {
		t.Fatal("expected enabled agent")
	}
	agent, ok := registry.Find("7937dd2d-0b8a-4880-8afa-0ea60fd65a43")
	if !ok || agent.Enabled {
		t.Fatalf("disabled agent = %#v, found=%t", agent, ok)
	}
	if agent.InteractionInstanceID != "local" {
		t.Fatalf("default InteractionInstanceID = %q", agent.InteractionInstanceID)
	}
}

func TestLoadDirRejectsInvalidAgents(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "missing id", body: `{"shared_secret":"secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`},
		{name: "missing secret", body: `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`},
		{name: "missing user ids", body: `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"secret","enabled":true}`},
		{name: "blank user id", body: `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"secret","enabled":true,"user_ids":[" "]}`},
		{name: "duplicate user id", body: `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752","fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`},
		{name: "named interaction instance id", body: `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"interaction_instance_id":"ops-phone"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgentFile(t, dir, "agent.json", tc.body)
			registry, diagnostics, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.All()) != 0 {
				t.Fatalf("registry = %#v", registry.All())
			}
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestLoadDirRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "one.json", `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`)
	writeAgentFile(t, dir, "two.json", `{"id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","shared_secret":"other-secret","enabled":true,"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"]}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.All()) != 1 {
		t.Fatalf("registry = %#v", registry.All())
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestRootAgentSchemaCoversRuntimeFields(t *testing.T) {
	root := repoRoot(t)
	schemaText, err := os.ReadFile(filepath.Join(root, "schemas", "proxyma.agent.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaText) {
		t.Fatal("proxyma.agent.schema.json is not valid JSON")
	}
	schema := string(schemaText)
	for _, required := range []string{
		"$schema",
		"id",
		"display_name",
		"shared_secret",
		"enabled",
		"user_ids",
		"interaction_instance_id",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("proxyma.agent.schema.json missing %q", required)
		}
	}
}

func writeAgentFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
