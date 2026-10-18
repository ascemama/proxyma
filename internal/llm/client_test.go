package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDecisionIsStrict(t *testing.T) {
	for _, test := range []struct {
		name     string
		payload  string
		decision string
		valid    bool
	}{
		{name: "approved", payload: `{"decision":"approved"}`, decision: DecisionApproved, valid: true},
		{name: "denied", payload: `{"decision":"denied"}`, decision: DecisionDenied, valid: true},
		{name: "approval", payload: `{"decision":"require approval"}`, decision: DecisionRequireApproval, valid: true},
		{name: "unknown", payload: `{"decision":"maybe"}`, decision: DecisionRequireApproval},
		{name: "extra text", payload: `approved`, decision: DecisionRequireApproval},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := parseDecision([]byte(test.payload))
			if result.Decision != test.decision || result.Valid != test.valid {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestOpenAICompatibleReviewUsesJSONDecision(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body["messages"].([]any)[1].(map[string]any)["content"].(string), "hostname") {
			t.Fatal("command was not sent to provider")
		}
		if body["max_completion_tokens"] != float64(256) {
			t.Fatalf("OpenAI request used unexpected token limit: %#v", body["max_completion_tokens"])
		}
		if body["reasoning_effort"] != "minimal" {
			t.Fatalf("OpenAI request used unexpected reasoning effort: %#v", body["reasoning_effort"])
		}
		if _, present := body["temperature"]; present {
			t.Fatal("OpenAI request should use the model default temperature")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"decision\":\"approved\"}"}}]}`))
	}))
	defer server.Close()
	profile := Profile{ID: "profile-1", OwnerUserID: "owner-1", Name: "test", Provider: ProviderOpenAI, Model: "test-model", BaseURL: server.URL, APIKey: "test-key", TimeoutSeconds: 5, Enabled: true}
	catalog := &Catalog{profiles: map[string]Profile{profile.ID: profile}, client: server.Client()}
	var logs bytes.Buffer
	catalog.ConfigureDebug(true, log.New(&logs, "", 0))
	result, err := catalog.Review(context.Background(), profile.ID, ReviewRequest{Command: "hostname", Prompt: "read-only"})
	if err != nil || !result.Valid || result.Decision != DecisionApproved {
		t.Fatalf("result = %#v err=%v", result, err)
	}
	if !strings.Contains(logs.String(), `prompt="read-only"`) || !strings.Contains(logs.String(), `decision=approved`) {
		t.Fatalf("missing debug details: %s", logs.String())
	}
	if strings.Contains(logs.String(), "test-key") || strings.Contains(logs.String(), "hostname") {
		t.Fatalf("debug log leaked secret or command: %s", logs.String())
	}
}
