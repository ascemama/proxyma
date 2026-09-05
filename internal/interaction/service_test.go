package interaction

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	records map[string]Interaction
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: map[string]Interaction{}}
}

func (s *memoryStore) LoadAll() ([]Interaction, error) {
	records := make([]Interaction, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	return records, nil
}

func (s *memoryStore) Save(record Interaction) error {
	s.records[record.InteractionID] = record
	return nil
}

type recordingProvider struct {
	sent      []Interaction
	canceled  []string
	tracked   []string
	untracked []string
	closed    bool
}

func (p *recordingProvider) Send(_ Context, request Interaction, _ Responder) (*Delivery, error) {
	p.sent = append(p.sent, request)
	return &Delivery{Provider: "test", Reference: request.InteractionID, DeliveredAt: request.CreatedAt}, nil
}

func (p *recordingProvider) Cancel(_ Context, request Interaction) error {
	p.canceled = append(p.canceled, request.InteractionID)
	return nil
}

func (p *recordingProvider) TrackPending(request Interaction, _ Responder) {
	p.tracked = append(p.tracked, request.InteractionID)
}

func (p *recordingProvider) UntrackPending(request Interaction) {
	p.untracked = append(p.untracked, request.InteractionID)
}

func (p *recordingProvider) Close() {
	p.closed = true
}

func TestServiceRequestRespondAndRecover(t *testing.T) {
	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	provider := &recordingProvider{}
	store := newMemoryStore()
	service := New(store, Options{
		Provider: provider,
		Timeout:  time.Hour,
		Now:      func() time.Time { return now },
	})

	record, err := service.Request(context.Background(), Request{
		TaskID:                "task-1",
		InteractionInstanceID: "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8",
		Kind:                  KindApprovalRequest,
		Subject:               "Approve command",
		Message:               "Run hostname?",
		RequestedBy:           "agent-demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusPending || record.ExpectedResponse != ExpectedApproval {
		t.Fatalf("record = %#v", record)
	}
	if record.ExpiresAt == nil || !record.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires_at = %#v", record.ExpiresAt)
	}
	if record.InteractionInstanceID != "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8" {
		t.Fatalf("InteractionInstanceID = %q", record.InteractionInstanceID)
	}
	if len(provider.sent) != 1 || provider.sent[0].InteractionID != record.InteractionID {
		t.Fatalf("provider sent = %#v", provider.sent)
	}
	if len(provider.tracked) != 1 || provider.tracked[0] != record.InteractionID {
		t.Fatalf("provider tracked = %#v", provider.tracked)
	}

	approved := true
	answered, err := service.Respond(context.Background(), Response{
		InteractionID: record.InteractionID,
		Approved:      &approved,
		RespondedBy:   "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != StatusAnswered || answered.Response == nil || answered.Response.Approved == nil || !*answered.Response.Approved {
		t.Fatalf("answered = %#v", answered)
	}
	if len(provider.untracked) != 1 || provider.untracked[0] != record.InteractionID {
		t.Fatalf("provider untracked = %#v", provider.untracked)
	}

	recovered := New(store, Options{})
	if err := recovered.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := recovered.Get(context.Background(), record.InteractionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusAnswered || got.Response == nil || got.Response.RespondedBy != "operator" || got.InteractionInstanceID != "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8" {
		t.Fatalf("recovered = %#v", got)
	}
}

func TestServiceRecoverTracksOnlyPendingInteractions(t *testing.T) {
	store := newMemoryStore()
	store.records["pending"] = Interaction{InteractionID: "pending", Status: StatusPending}
	store.records["answered"] = Interaction{InteractionID: "answered", Status: StatusAnswered}
	store.records["canceled"] = Interaction{InteractionID: "canceled", Status: StatusCanceled}
	store.records["expired"] = Interaction{InteractionID: "expired", Status: StatusExpired}
	provider := &recordingProvider{}
	service := New(store, Options{Provider: provider})

	if err := service.Recover(); err != nil {
		t.Fatal(err)
	}
	if len(provider.tracked) != 1 || provider.tracked[0] != "pending" {
		t.Fatalf("provider tracked = %#v", provider.tracked)
	}
}

func TestServiceQuestionRequiresTextAnswer(t *testing.T) {
	service := New(newMemoryStore(), Options{})
	record, err := service.Request(context.Background(), Request{
		Kind:    KindQuestion,
		Subject: "Need hostname",
		Message: "Which host?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.InteractionInstanceID != DefaultInstanceLocal {
		t.Fatalf("default InteractionInstanceID = %q", record.InteractionInstanceID)
	}
	if _, err := service.Respond(context.Background(), Response{InteractionID: record.InteractionID}); err == nil {
		t.Fatal("expected missing answer error")
	}
	answered, err := service.Respond(context.Background(), Response{InteractionID: record.InteractionID, Answer: "db-1"})
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != StatusAnswered || answered.Response.Answer != "db-1" {
		t.Fatalf("answered = %#v", answered)
	}
}

func TestServiceRejectsMalformedApprovalResponse(t *testing.T) {
	service := New(newMemoryStore(), Options{})
	record, err := service.Request(context.Background(), Request{
		Kind:    KindApprovalRequest,
		Subject: "Approve command",
		Message: "Run command?",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Respond(context.Background(), Response{InteractionID: record.InteractionID}); err == nil {
		t.Fatal("expected approval response without Approved value to be rejected")
	}
	got, err := service.Get(context.Background(), record.InteractionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPending || got.Response != nil {
		t.Fatalf("malformed response changed interaction = %#v", got)
	}
}

func TestServiceExpiresPendingInteractions(t *testing.T) {
	now := time.Date(2026, 5, 31, 1, 0, 0, 0, time.UTC)
	service := New(newMemoryStore(), Options{
		Provider: &recordingProvider{},
		Timeout:  time.Minute,
		Now:      func() time.Time { return now },
	})
	record, err := service.Request(context.Background(), Request{
		Kind:    KindQuestion,
		Subject: "Question",
		Message: "Answer?",
	})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := service.ExpirePending(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].InteractionID != record.InteractionID {
		t.Fatalf("expired = %#v", expired)
	}
	got, err := service.Get(context.Background(), record.InteractionID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusExpired {
		t.Fatalf("status = %s", got.Status)
	}
	provider := service.provider.(*recordingProvider)
	if len(provider.untracked) != 1 || provider.untracked[0] != record.InteractionID {
		t.Fatalf("provider untracked = %#v", provider.untracked)
	}
}

func TestServiceCancelUntracksPendingInteraction(t *testing.T) {
	provider := &recordingProvider{}
	service := New(newMemoryStore(), Options{Provider: provider})
	record, err := service.Request(context.Background(), Request{
		Kind:    KindQuestion,
		Subject: "Question",
		Message: "Answer?",
	})
	if err != nil {
		t.Fatal(err)
	}

	canceled, err := service.Cancel(context.Background(), record.InteractionID, "operator canceled")
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != StatusCanceled || len(provider.canceled) != 1 {
		t.Fatalf("canceled=%#v provider canceled=%#v", canceled, provider.canceled)
	}
	if len(provider.untracked) != 1 || provider.untracked[0] != record.InteractionID {
		t.Fatalf("provider untracked = %#v", provider.untracked)
	}
}

func TestServiceNotFound(t *testing.T) {
	service := New(newMemoryStore(), Options{})
	if _, err := service.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
