package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proxyma/internal/interaction"
)

type responder struct {
	responses chan interaction.Response
}

func newResponder() *responder {
	return &responder{responses: make(chan interaction.Response, 8)}
}

func (r *responder) Respond(_ interaction.Context, response interaction.Response) (*interaction.Interaction, error) {
	r.responses <- response
	return &interaction.Interaction{InteractionID: response.InteractionID, Status: interaction.StatusAnswered, Response: &response}, nil
}

func (r *responder) Cancel(_ interaction.Context, interactionID string, reason string) (*interaction.Interaction, error) {
	return &interaction.Interaction{InteractionID: interactionID, Status: interaction.StatusCanceled, CancelReason: reason}, nil
}

func TestSendPublishesApprovalNotification(t *testing.T) {
	var got publishRequest
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprint(w, `{"id":"published-id","event":"message"}`)
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		Token:         "token-secret",
		HTTPClient:    server.Client(),
		Now: func() time.Time {
			return time.Date(2026, 6, 5, 1, 2, 3, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	delivery, err := provider.Send(context.Background(), testInteraction("interaction-1"), newResponder())
	if err != nil {
		t.Fatal(err)
	}

	if delivery.Provider != ProviderName || delivery.Reference != "published-id" {
		t.Fatalf("delivery = %#v", delivery)
	}
	if auth != "Bearer token-secret" {
		t.Fatalf("Authorization = %q", auth)
	}
	if got.Topic != "requests-secret" || got.Title != "Approve Proxyma command" {
		t.Fatalf("publish request = %#v", got)
	}
	for _, want := range []string{
		"Task ID: task-1",
		"Target: 127.0.0.1",
		"Command: ssh {user}@127.0.0.1 hostname",
		"Reasoning: test",
		"Requested by: agent-demo",
	} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message missing %q:\n%s", want, got.Message)
		}
	}
	if len(got.Actions) != 2 {
		t.Fatalf("actions = %#v", got.Actions)
	}
	assertAction(t, got.Actions[0], "Approve", server.URL+"/responses-secret", true)
	assertAction(t, got.Actions[1], "Reject", server.URL+"/responses-secret", false)
	if strings.Contains(got.Actions[0].Body, "token-secret") || strings.Contains(got.Actions[1].Body, "token-secret") {
		t.Fatal("action body leaked token")
	}
}

func TestDebugLogsPublishRequestWithoutAuthorization(t *testing.T) {
	var logs bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"published-id","event":"message"}`)
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		Token:         "token-secret",
		HTTPClient:    server.Client(),
		Debug:         true,
		Logger:        log.New(&logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := provider.Send(context.Background(), testInteraction("interaction-1"), newResponder()); err != nil {
		t.Fatal(err)
	}

	output := logs.String()
	for _, want := range []string{
		"debug ntfy request method=POST",
		"url=" + server.URL,
		`"topic":"requests-secret"`,
		"ssh {user}@127.0.0.1 hostname",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing %q in:\n%s", want, output)
		}
	}
	if strings.Contains(output, "token-secret") || strings.Contains(output, "Authorization") {
		t.Fatalf("debug log leaked authorization material:\n%s", output)
	}
}

func TestDebugLogsPollRequestWithoutAuthorization(t *testing.T) {
	var logs bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "")
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		Token:         "token-secret",
		HTTPClient:    server.Client(),
		Debug:         true,
		Logger:        log.New(&logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := provider.pollOnce(context.Background(), newResponder()); err != nil {
		t.Fatal(err)
	}

	output := logs.String()
	for _, want := range []string{
		"debug ntfy request method=GET",
		server.URL + "/responses-secret/json",
		"poll=1",
		"since=all",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug log missing %q in:\n%s", want, output)
		}
	}
	if strings.Contains(output, "token-secret") || strings.Contains(output, "Authorization") {
		t.Fatalf("debug log leaked authorization material:\n%s", output)
	}
}

func TestPollConvertsApprovalResponsesAndDeduplicatesMessages(t *testing.T) {
	lines := strings.Join([]string{
		`{"id":"open-1","event":"open","topic":"responses-secret"}`,
		`{"id":"keepalive-1","event":"keepalive","topic":"responses-secret"}`,
		`{"id":"invalid-1","event":"message","topic":"responses-secret","message":"not json"}`,
		responseEvent("response-1", "interaction-1", true, "operator"),
	}, "\n") + "\n"
	var pollCount int
	var gotSince []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/responses-secret/json" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		pollCount++
		gotSince = append(gotSince, r.URL.Query().Get("since"))
		_, _ = fmt.Fprint(w, lines)
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		HTTPClient:    server.Client(),
		RespondedBy:   "ntfy-default",
	})
	if err != nil {
		t.Fatal(err)
	}
	responder := newResponder()

	if err := provider.pollOnce(context.Background(), responder); err != nil {
		t.Fatal(err)
	}
	if err := provider.pollOnce(context.Background(), responder); err != nil {
		t.Fatal(err)
	}

	if pollCount != 2 {
		t.Fatalf("pollCount = %d", pollCount)
	}
	if len(gotSince) != 2 || gotSince[0] != "all" || gotSince[1] != "response-1" {
		t.Fatalf("since values = %#v", gotSince)
	}
	response := waitResponse(t, responder)
	if response.InteractionID != "interaction-1" || response.Approved == nil || !*response.Approved || response.RespondedBy != "operator" {
		t.Fatalf("response = %#v", response)
	}
	select {
	case duplicate := <-responder.responses:
		t.Fatalf("unexpected duplicate response: %#v", duplicate)
	default:
	}
}

func TestPollUsesDefaultRespondedBy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, responseEvent("response-1", "interaction-1", false, ""))
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		HTTPClient:    server.Client(),
		RespondedBy:   "ntfy-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	responder := newResponder()

	if err := provider.pollOnce(context.Background(), responder); err != nil {
		t.Fatal(err)
	}

	response := waitResponse(t, responder)
	if response.Approved == nil || *response.Approved || response.RespondedBy != "ntfy-user" {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPFailuresDoNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope token-secret", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		Token:         "token-secret",
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(context.Background(), testInteraction("interaction-1"), newResponder())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "requests-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestPollingFollowsPendingInteractionLifecycle(t *testing.T) {
	started := make(chan struct{}, 4)
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			polls.Add(1)
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"published-id","event":"message"}`)
	}))
	t.Cleanup(server.Close)
	provider, err := New(Options{
		BaseURL:       server.URL,
		RequestTopic:  "requests-secret",
		ResponseTopic: "responses-secret",
		PollInterval:  10 * time.Millisecond,
		PollTimeout:   5 * time.Second,
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	responder := newResponder()
	first := testInteraction("interaction-1")
	second := testInteraction("interaction-2")

	provider.TrackPending(first, responder)
	waitForPollStart(t, started)
	provider.TrackPending(second, responder)
	provider.UntrackPending(first)
	if !provider.isPolling() {
		t.Fatal("polling stopped while another interaction was pending")
	}
	provider.UntrackPending(second)
	waitForPollingState(t, provider, false)
	stoppedAt := polls.Load()
	time.Sleep(3 * provider.pollInterval)
	if polls.Load() != stoppedAt {
		t.Fatalf("polling continued while idle: before=%d after=%d", stoppedAt, polls.Load())
	}

	provider.TrackPending(first, responder)
	waitForPollStart(t, started)
	provider.UntrackPending(first)
	waitForPollingState(t, provider, false)
}

func waitForPollStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for poll to start")
	}
}

func waitForPollingState(t *testing.T, provider *Provider, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if provider.isPolling() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("polling state did not become %v", want)
}

func (p *Provider) isPolling() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pollCancel != nil
}

func assertAction(t *testing.T, action publishAction, label string, url string, approved bool) {
	t.Helper()
	if action.Action != "http" || action.Label != label || action.URL != url || action.Method != http.MethodPost || !action.Clear {
		t.Fatalf("action = %#v", action)
	}
	var body approvalResponse
	if err := json.Unmarshal([]byte(action.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.InteractionID != "interaction-1" || body.Approved == nil || *body.Approved != approved {
		t.Fatalf("action body = %#v", body)
	}
}

func responseEvent(id string, interactionID string, approved bool, respondedBy string) string {
	approvedValue := approved
	response, _ := json.Marshal(approvalResponse{
		InteractionID: interactionID,
		Approved:      &approvedValue,
		RespondedBy:   respondedBy,
	})
	event, _ := json.Marshal(ntfyEvent{
		ID:      id,
		Time:    time.Date(2026, 6, 5, 1, 2, 3, 0, time.UTC).Unix(),
		Event:   "message",
		Topic:   "responses-secret",
		Message: string(response),
	})
	return string(event)
}

func testInteraction(id string) interaction.Interaction {
	return interaction.Interaction{
		InteractionID:    id,
		TaskID:           "task-1",
		Kind:             interaction.KindApprovalRequest,
		ExpectedResponse: interaction.ExpectedApproval,
		Subject:          "Approve command",
		Message:          "Run command?",
		RequestedBy:      "agent-demo",
		Metadata: map[string]any{
			"target":    "127.0.0.1",
			"command":   "ssh {user}@127.0.0.1 hostname",
			"reasoning": "test",
		},
	}
}

func waitResponse(t *testing.T, responder *responder) interaction.Response {
	t.Helper()
	select {
	case response := <-responder.responses:
		return response
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
		return interaction.Response{}
	}
}
