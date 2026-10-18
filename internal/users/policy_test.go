package users

import (
	"strings"
	"testing"

	"proxyma/internal/agents"
	"proxyma/internal/store"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
)

const (
	policyImportID       = "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"
	policyImportRuleID   = "f9d3851c-2c6d-4c1f-95e4-1c7f5a7c2bc8"
	policyImportOtherID  = "c1b7b3a8-2b0f-42a0-93cf-8d1b890ec4c6"
	policyImportTargetID = "8dc60324-b3bb-4d19-9aa6-0e149e4938b8"
	policyOtherTargetID  = "7f6c20a9-e5ca-44dd-9e68-52c0e7b52e49"
	policyOnlyTargetID   = "3b9d9f55-4495-4b02-bb55-58fdc5f8ad4f"
)

func TestImportRuleListAppendsDuplicatesAndRejectsConflictsAtomically(t *testing.T) {
	paths := testPaths(t)
	existing := policy.Policy{
		ID:          policyImportID,
		OwnerUserID: testUserID,
		Name:        "Diagnostics",
		Enabled:     true,
		Rules: []policy.Rule{{
			ID:              policyImportRuleID,
			Effect:          policy.DecisionAllow,
			CommandTemplate: "hostname",
		}},
	}
	if err := store.New(paths.PolicyDir).WriteJSON(existing.ID, existing); err != nil {
		t.Fatal(err)
	}

	result, err := ImportRuleList(paths, existing.ID, RuleListDocument{
		Version: 1,
		Blacklist: []RuleListEntry{{
			CommandTemplate: "rm -rf {path}",
			Reason:          "destructive filesystem operation",
		}},
		Whitelist: []RuleListEntry{
			{CommandTemplate: "hostname", Reason: "duplicate"},
			{CommandTemplate: "ping {ipv4}", Reason: "reachability"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 2 || len(result.Skipped) != 1 || len(result.Conflicts) != 0 {
		t.Fatalf("import result = %#v", result)
	}
	if len(result.Policy.Rules) != 3 {
		t.Fatalf("rules after import = %#v", result.Policy.Rules)
	}

	before := result.Policy
	conflict, err := ImportRuleList(paths, existing.ID, RuleListDocument{
		Version: 1,
		Blacklist: []RuleListEntry{{
			CommandTemplate: "hostname",
			Reason:          "opposite effect",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflict error = %v", err)
	}
	if len(conflict.Conflicts) != 1 {
		t.Fatalf("conflicts = %#v", conflict.Conflicts)
	}
	if len(conflict.Policy.Rules) != len(before.Rules) {
		t.Fatalf("conflicting import changed policy: before=%d after=%d", len(before.Rules), len(conflict.Policy.Rules))
	}
	var stored policy.Policy
	readJSON(t, store.New(paths.PolicyDir).RecordPath(existing.ID), &stored)
	if len(stored.Rules) != len(before.Rules) {
		t.Fatalf("persisted rules after conflict = %#v", stored.Rules)
	}
}

func TestAssignPolicyTargetsReplacesOnePolicyAndEnforcesOwnership(t *testing.T) {
	paths := testPaths(t)
	writeUserRecord(t, paths, storedTestUser(testUserID, "Alice"))
	writeUserRecord(t, paths, storedTestUser(testOtherID, "Bob"))
	writeAgentRecord(t, paths, agents.Agent{ID: testAgentID, AgentJWTSigningKey: "signing-key", Enabled: true, UserIDs: []string{testUserID}})
	writePolicyRecord(t, paths, policy.Policy{
		ID:          policyImportID,
		OwnerUserID: testUserID,
		Name:        "Managed policy",
		Enabled:     true,
		Rules:       []policy.Rule{{ID: policyImportRuleID, Effect: policy.DecisionAllow, CommandTemplate: "hostname"}},
	})
	writePolicyRecord(t, paths, policy.Policy{
		ID:          policyImportOtherID,
		OwnerUserID: testUserID,
		Name:        "Fallback policy",
		Enabled:     true,
		Rules:       []policy.Rule{{ID: "9a5f3db7-7c11-40cd-9050-bef6a5ff0bc3", Effect: policy.DecisionDeny, CommandTemplate: "hostname"}},
	})
	writeTargetRecord(t, paths, targets.Target{
		ID:       policyImportTargetID,
		Hostname: "owned.example",
		Username: "demo",
		PolicyID: policyImportOtherID,
		AgentIDs: []string{testAgentID},
		UserIDs:  []string{testUserID},
		Auth:     targets.PasswordAuth("password"),
	})
	writeTargetRecord(t, paths, targets.Target{
		ID:       policyOtherTargetID,
		Hostname: "other.example",
		Username: "demo",
		PolicyID: policyImportOtherID,
		AgentIDs: []string{testAgentID},
		UserIDs:  []string{testOtherID},
		Auth:     targets.PasswordAuth("password"),
	})
	writeTargetRecord(t, paths, targets.Target{
		ID:       policyOnlyTargetID,
		Hostname: "only-policy.example",
		Username: "demo",
		PolicyID: policyImportID,
		AgentIDs: []string{testAgentID},
		UserIDs:  []string{testUserID},
		Auth:     targets.PasswordAuth("password"),
	})

	if err := AssignPolicyTargets(paths, policyImportID, testUserID, false, []string{policyImportTargetID, policyOnlyTargetID}); err != nil {
		t.Fatal(err)
	}
	var owned targets.Target
	readJSON(t, store.New(paths.TargetDir).RecordPath(policyImportTargetID), &owned)
	if owned.PolicyID != policyImportID {
		t.Fatalf("owned target policy = %q", owned.PolicyID)
	}

	if err := AssignPolicyTargets(paths, policyImportID, testUserID, false, []string{policyOtherTargetID}); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign target assignment error = %v", err)
	}
	if err := AssignPolicyTargets(paths, policyImportID, testUserID, false, nil); err != nil {
		t.Fatalf("empty assignment should leave targets unchanged: %v", err)
	}
}

func TestCanModifyPolicyAllowsOwnerOrAdminOnly(t *testing.T) {
	item := policy.Policy{OwnerUserID: testUserID}
	if !CanModifyPolicy(item, testUserID, false) {
		t.Fatal("owner was not allowed to modify policy")
	}
	if CanModifyPolicy(item, testOtherID, false) {
		t.Fatal("other user was allowed to modify policy")
	}
	if !CanModifyPolicy(item, testOtherID, true) {
		t.Fatal("administrator was not allowed to modify policy")
	}
}

func writePolicyRecord(t *testing.T, paths Paths, item policy.Policy) {
	t.Helper()
	if err := store.New(paths.PolicyDir).WriteJSON(item.ID, item); err != nil {
		t.Fatal(err)
	}
}
