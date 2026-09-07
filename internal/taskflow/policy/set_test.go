package policy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	baselinePolicyID = "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"
	allowPolicyID    = "37fb51da-5190-407f-97bc-9d027b9fe49a"
	denyPolicyID     = "2ea7992b-b0d4-4421-9195-ad8f805758b0"
)

func TestLoadDirReportsInvalidPolicies(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "valid.json", `{"id":"`+baselinePolicyID+`","name":"Baseline","rules":[{"id":"default","effect":"approval","always":true}]}`)
	writePolicy(t, dir, "duplicate.json", `{"id":"`+baselinePolicyID+`","name":"Duplicate Baseline","rules":[{"id":"default","effect":"approval","always":true}]}`)
	writePolicy(t, dir, "bad-json.json", `{`)
	writePolicy(t, dir, "bad-effect.json", `{"id":"8d16d77f-5066-4402-9fd6-440596a6849c","name":"Bad Effect","rules":[{"id":"x","effect":"maybe","always":true}]}`)
	writePolicy(t, dir, "bad-regex.json", `{"id":"e1c04003-4da3-44f0-a907-f40117fcacb6","name":"Bad Regex","rules":[{"id":"x","effect":"deny","remote_command_regex":"["}]}`)
	writePolicy(t, dir, "bad-id.json", `{"id":"bad-id","name":"Bad ID","rules":[{"id":"x","effect":"approval","always":true}]}`)
	writePolicy(t, dir, "missing-name.json", `{"id":"fdb64d47-ab6d-4980-a061-de62cc5b7f80","rules":[{"id":"x","effect":"approval","always":true}]}`)

	set, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if set == nil || !set.Has(baselinePolicyID) {
		t.Fatalf("set = %#v", set)
	}
	if len(diagnostics) != 6 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for _, want := range []string{"duplicate policy id", "invalid JSON", "unsupported effect", "remote_command_regex", "id must be a UUID v4", "missing required field name"} {
		if !diagnosticsContain(diagnostics, want) {
			t.Fatalf("diagnostics missing %q: %#v", want, diagnostics)
		}
	}
}

func TestEvaluateReturnsAllowApprovalAndDeny(t *testing.T) {
	enabled := true
	set := NewSet(map[string]Policy{
		baselinePolicyID: {
			ID:       baselinePolicyID,
			Name:     "Baseline",
			Priority: 10,
			Enabled:  &enabled,
			Rules: []Rule{
				{ID: "deny-dangerous", Effect: DecisionDeny, RemoteCommandRegex: `(?i)\brm\s+-rf\b`, Reason: "dangerous"},
				{ID: "allow-hostname", Effect: DecisionAllow, RemoteCommandPrefixAny: []string{"hostname"}, Reason: "readonly"},
				{ID: "approval-default", Effect: DecisionApproval, Always: true, Reason: "approval"},
			},
		},
	})

	for _, tc := range []struct {
		name    string
		command string
		want    string
	}{
		{name: "deny", command: "rm -rf /tmp/demo", want: DecisionDeny},
		{name: "allow", command: "hostname", want: DecisionAllow},
		{name: "approval", command: "systemctl status ssh", want: DecisionApproval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := set.Evaluate(context.Background(), Request{PolicyIDs: []string{baselinePolicyID}, RemoteCommand: tc.command})
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != tc.want {
				t.Fatalf("decision = %s", result.Decision)
			}
		})
	}
}

func TestEvaluateDenyOverridesHigherPriorityAllow(t *testing.T) {
	set := NewSet(map[string]Policy{
		allowPolicyID: {ID: allowPolicyID, Name: "Allow", Priority: 100, Rules: []Rule{{ID: "allow-all", Effect: DecisionAllow, Always: true}}},
		denyPolicyID:  {ID: denyPolicyID, Name: "Deny", Priority: 1, Rules: []Rule{{ID: "deny-rm", Effect: DecisionDeny, RemoteCommandRegex: `rm`}}},
	})

	result, err := set.Evaluate(context.Background(), Request{PolicyIDs: []string{allowPolicyID, denyPolicyID}, RemoteCommand: "rm -rf /tmp/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionDeny || result.PolicyID != denyPolicyID {
		t.Fatalf("result = %#v", result)
	}
}

func TestEvaluateMissingPolicyIDsDeny(t *testing.T) {
	result, err := NewSet(nil).Evaluate(context.Background(), Request{RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionDeny || !strings.Contains(result.Reason, "missing policy_ids") {
		t.Fatalf("result = %#v", result)
	}
}

func TestRootPolicySchemaAndBaselinePolicyAreValidJSON(t *testing.T) {
	root := repoRoot(t)
	for _, file := range []string{filepath.Join("schemas", "proxyma.policy.schema.json"), filepath.Join("policies", "baseline-readonly.json")} {
		text, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(text) {
			t.Fatalf("%s is not valid JSON", file)
		}
	}
}

func TestBaselinePolicyUUIDReferenceResolves(t *testing.T) {
	root := repoRoot(t)
	set, diagnostics, err := LoadDir(filepath.Join(root, "policies"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if !set.Has(baselinePolicyID) {
		t.Fatalf("baseline policy %s was not loaded", baselinePolicyID)
	}
	if diagnostics := set.ValidateReferences("baseline target", []string{baselinePolicyID}); len(diagnostics) != 0 {
		t.Fatalf("reference diagnostics = %#v", diagnostics)
	}
}

func writePolicy(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func diagnosticsContain(diagnostics []Diagnostic, want string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, want) {
			return true
		}
	}
	return false
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
