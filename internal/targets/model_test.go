package targets

import "testing"

func TestRegistryMatchTarget(t *testing.T) {
	registry := NewRegistry([]Target{
		{Hostname: "alpha.example", IP: "10.0.0.1", Username: "u", AgentIDs: []string{"df9ec8bb-1a22-4181-a7c8-126ad553221d"}, Auth: PasswordAuth("p")},
		{Hostname: "beta.example", IP: "10.0.0.2", Username: "u", AgentIDs: []string{"df9ec8bb-1a22-4181-a7c8-126ad553221d"}, Auth: PasswordAuth("p")},
	})

	if _, found, duplicate := registry.MatchTarget("alpha.example"); !found || duplicate {
		t.Fatalf("expected hostname match found=%t duplicate=%t", found, duplicate)
	}
	if _, found, duplicate := registry.MatchTarget("10.0.0.2"); !found || duplicate {
		t.Fatalf("expected ip match found=%t duplicate=%t", found, duplicate)
	}
	if _, found, duplicate := registry.MatchTarget("missing"); found || duplicate {
		t.Fatalf("unexpected match found=%t duplicate=%t", found, duplicate)
	}
}

func TestRegistryMatchTargetDetectsDuplicates(t *testing.T) {
	registry := NewRegistry([]Target{
		{Hostname: "dup.example", Username: "u1", AgentIDs: []string{"df9ec8bb-1a22-4181-a7c8-126ad553221d"}, Auth: PasswordAuth("p1")},
		{IP: "dup.example", Username: "u2", AgentIDs: []string{"df9ec8bb-1a22-4181-a7c8-126ad553221d"}, Auth: PasswordAuth("p2")},
	})

	if _, found, duplicate := registry.MatchTarget("dup.example"); !found || !duplicate {
		t.Fatalf("expected duplicate match found=%t duplicate=%t", found, duplicate)
	}
}

func TestRegistryMatchTargetForAgentDisambiguatesDuplicates(t *testing.T) {
	agentID := "df9ec8bb-1a22-4181-a7c8-126ad553221d"
	otherAgentID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	registry := NewRegistry([]Target{
		{Hostname: "shared.example", Username: "u1", AgentIDs: []string{agentID}, Auth: PasswordAuth("p1")},
		{IP: "shared.example", Username: "u2", AgentIDs: []string{otherAgentID}, Auth: PasswordAuth("p2")},
	})

	target, found, duplicate := registry.MatchTargetForAgent("shared.example", agentID)
	if !found || duplicate || target.Username != "u1" {
		t.Fatalf("match = %#v found=%t duplicate=%t", target, found, duplicate)
	}
	if _, found, duplicate = registry.MatchTargetForAgent("shared.example", "missing-agent"); found || duplicate {
		t.Fatalf("unexpected missing-agent match found=%t duplicate=%t", found, duplicate)
	}
}
