package agentcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyFetchReturnsTargetGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/targets/policy" || r.URL.Query().Get("hostname") != "example.test" {
			t.Fatalf("request = %s", r.URL.String())
		}
		writeTestJSON(w, map[string]any{
			"target": map[string]any{"id": "target-1", "transport": "ssh", "os": "linux", "hostname": "example.test"},
			"policy": map[string]any{
				"id": "policy-1", "name": "Diagnostics", "description": "Safe checks", "enabled": true,
				"blacklist": []map[string]any{{"command_template": "rm -rf {path}", "reason": "destructive"}},
				"whitelist": []map[string]any{{"command_template": "ping {ipv4}", "reason": "reachability"}},
			},
		})
	}))
	defer server.Close()

	service := NewService(testConfig(t, server.URL))
	envelope, err := service.Policy(context.Background(), Target{Hostname: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "policy" || envelope.Policy == nil || envelope.Policy.Policy.ID != "policy-1" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if len(envelope.Policy.Policy.Blacklist) != 1 || len(envelope.Policy.Policy.Whitelist) != 1 {
		t.Fatalf("policy = %#v", envelope.Policy.Policy)
	}
}

func TestPolicyHelpIsExplicitlyTargetBound(t *testing.T) {
	help, ok := helpFor("policy")
	if !ok || help.Usage != "proxyma-agent policy --target HOST_OR_IP" {
		t.Fatalf("help = %#v", help)
	}
}
