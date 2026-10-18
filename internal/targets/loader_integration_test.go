package targets

import "testing"

func TestLoadDirMixedTargets(t *testing.T) {
	dir := t.TempDir()
	writeTarget(t, dir, "a.json", `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"a.example","ip":"10.0.0.1","username":"root","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"secret"}}`)
	writeTarget(t, dir, "b.json", `{"id":"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5","hostname":"b.example","ip":"10.0.0.2","username":"root","policy_id":"baseline","agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],"user_ids":["fcd2f3a9-d546-44f4-837b-0c7aa4219752"],"auth":{"type":"password","password":"secret"}}`)
	writeTarget(t, dir, "bad.json", `{"id":"8dc60324-b3bb-4d19-9aa6-0e149e4938b8","hostname":"bad.example"}`)

	registry, diagnostics, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, _ := registry.MatchTarget("a.example"); !found {
		t.Fatal("target a was not loaded")
	}
	if _, found, _ := registry.MatchTarget("10.0.0.2"); !found {
		t.Fatal("target b was not loaded")
	}
	if _, found, _ := registry.MatchTarget("bad.example"); found {
		t.Fatal("invalid target was loaded")
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}
