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
	testOwnerID      = "fcd2f3a9-d546-44f4-837b-0c7aa4219752"
)

func TestLoadDirReportsInvalidPolicies(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, "valid.json", `{"id":"`+baselinePolicyID+`","owner_user_id":"`+testOwnerID+`","name":"Baseline","enabled":true,"rules":[{"id":"default","effect":"deny","command_template":"shutdown"}]}`)
	writePolicy(t, dir, "duplicate.json", `{"id":"`+baselinePolicyID+`","owner_user_id":"`+testOwnerID+`","name":"Duplicate Baseline","enabled":true,"rules":[]}`)
	writePolicy(t, dir, "bad-json.json", `{`)
	writePolicy(t, dir, "bad-effect.json", `{"id":"8d16d77f-5066-4402-9fd6-440596a6849c","owner_user_id":"`+testOwnerID+`","name":"Bad Effect","enabled":true,"rules":[{"id":"x","effect":"maybe","command_template":"hostname"}]}`)
	writePolicy(t, dir, "bad-template.json", `{"id":"e1c04003-4da3-44f0-a907-f40117fcacb6","owner_user_id":"`+testOwnerID+`","name":"Bad Template","enabled":true,"rules":[{"id":"x","effect":"deny","command_template":"echo {unknown}"}]}`)
	writePolicy(t, dir, "bad-id.json", `{"id":"bad-id","owner_user_id":"`+testOwnerID+`","name":"Bad ID","enabled":true,"rules":[]}`)
	writePolicy(t, dir, "missing-owner.json", `{"id":"fdb64d47-ab6d-4980-a061-de62cc5b7f80","name":"Missing owner","enabled":true,"rules":[]}`)

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
	for _, want := range []string{"duplicate policy id", "invalid JSON", "unsupported effect", "unsupported placeholder", "id must be a UUID v4", "missing required field owner_user_id"} {
		if !diagnosticsContain(diagnostics, want) {
			t.Fatalf("diagnostics missing %q: %#v", want, diagnostics)
		}
	}
}

func TestEvaluateDenyAllowAndApprovalFallback(t *testing.T) {
	set := NewSet(map[string]Policy{
		baselinePolicyID: {
			ID: baselinePolicyID, OwnerUserID: testOwnerID, Name: "Baseline", Enabled: true,
			Rules: []Rule{
				{ID: "deny-dangerous", Effect: DecisionDeny, CommandTemplate: "rm -rf {path}", Reason: "dangerous"},
				{ID: "allow-hostname", Effect: DecisionAllow, CommandTemplate: "hostname", Reason: "readonly"},
			},
		},
	})
	for _, tc := range []struct {
		name, command, want string
	}{
		{name: "deny", command: "rm -rf /tmp/demo", want: DecisionDeny},
		{name: "allow", command: "hostname", want: DecisionAllow},
		{name: "approval", command: "systemctl status ssh", want: DecisionApproval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := set.Evaluate(context.Background(), Request{PolicyID: baselinePolicyID, RemoteCommand: tc.command})
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != tc.want {
				t.Fatalf("decision = %s", result.Decision)
			}
		})
	}
}

func TestEvaluateUsesOnlyTheSelectedPolicy(t *testing.T) {
	set := NewSet(map[string]Policy{
		allowPolicyID: {ID: allowPolicyID, OwnerUserID: testOwnerID, Name: "Allow", Enabled: true, Rules: []Rule{{ID: "allow-hostname", Effect: DecisionAllow, CommandTemplate: "hostname"}}},
		denyPolicyID:  {ID: denyPolicyID, OwnerUserID: testOwnerID, Name: "Deny", Enabled: true, Rules: []Rule{{ID: "deny-hostname", Effect: DecisionDeny, CommandTemplate: "hostname"}}},
	})
	result, err := set.Evaluate(context.Background(), Request{PolicyID: allowPolicyID, RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionAllow || result.PolicyID != allowPolicyID {
		t.Fatalf("result = %#v", result)
	}
}

func TestEvaluateMissingPolicyIDDeny(t *testing.T) {
	result, err := NewSet(nil).Evaluate(context.Background(), Request{RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionDeny || !strings.Contains(result.Reason, "missing policy_id") {
		t.Fatalf("result = %#v", result)
	}
}

func TestPolicySchemaAndBaselineExampleAreValidJSON(t *testing.T) {
	root := repoRoot(t)
	for _, file := range []string{filepath.Join("schemas", "proxyma.policy.schema.json"), filepath.Join("docs", "examples", "policy-baseline-readonly.json")} {
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
	baseline, err := loadFile(filepath.Join(root, "docs", "examples", "policy-baseline-readonly.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(&baseline); err != nil {
		t.Fatal(err)
	}
	set := NewSet(map[string]Policy{baseline.ID: baseline})
	if !set.Has(baselinePolicyID) {
		t.Fatalf("baseline policy %s was not loaded", baselinePolicyID)
	}
	if diagnostics := set.ValidateReference("baseline target", baselinePolicyID); len(diagnostics) != 0 {
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
