package interaction

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("interaction not found")

// Service persists interactions and normalizes responses independently from delivery channels.
type Service struct {
	mu           sync.RWMutex
	store        Store
	provider     Provider
	timeout      time.Duration
	now          func() time.Time
	interactions map[string]*Interaction
}

// Options configures the interaction service.
type Options struct {
	Provider Provider
	Timeout  time.Duration
	Now      func() time.Time
}

// New creates an interaction communicator backed by a durable interaction store.
func New(interactionStore Store, options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:        interactionStore,
		provider:     options.Provider,
		timeout:      options.Timeout,
		now:          now,
		interactions: make(map[string]*Interaction),
	}
}

// Recover loads persisted interactions.
func (s *Service) Recover() error {
	if s.store == nil {
		return fmt.Errorf("interaction store is not configured")
	}
	records, err := s.store.LoadAll()
	if err != nil {
		return err
	}
	for _, record := range records {
		copied := cloneInteraction(&record)
		s.interactions[copied.InteractionID] = copied
		if copied.Status == StatusPending {
			s.trackPending(*copied)
		}
	}
	return nil
}

// Request creates, delivers, and persists one pending interaction.
func (s *Service) Request(ctx Context, request Request) (*Interaction, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var expiresAt *time.Time
	if s.timeout > 0 {
		expires := now.Add(s.timeout)
		expiresAt = &expires
	}
	interaction := &Interaction{
		InteractionID:         id,
		TaskID:                strings.TrimSpace(request.TaskID),
		InteractionInstanceID: normalizeInstanceID(request.InteractionInstanceID),
		Kind:                  request.Kind,
		ExpectedResponse:      expectedResponse(request),
		Subject:               request.Subject,
		Message:               request.Message,
		HarnessName:           request.HarnessName,
		Metadata:              copyMetadata(request.Metadata),
		Status:                StatusPending,
		CreatedAt:             now,
		ExpiresAt:             expiresAt,
	}
	if err := s.save(interaction); err != nil {
		return nil, err
	}
	s.trackPending(*interaction)
	if s.provider != nil {
		delivery, err := s.provider.Send(ctx, *cloneInteraction(interaction), s)
		if err != nil {
			_, _ = s.Cancel(ctx, interaction.InteractionID, "interaction delivery failed: "+err.Error())
			return nil, err
		}
		if err := s.setDelivery(interaction.InteractionID, delivery); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, interaction.InteractionID)
}

// Respond validates and records a normalized response.
func (s *Service) Respond(_ Context, response Response) (*Interaction, error) {
	if strings.TrimSpace(response.InteractionID) == "" {
		return nil, fmt.Errorf("interaction_id is required")
	}
	interaction, ok := s.getMutable(response.InteractionID)
	if !ok {
		return nil, ErrNotFound
	}
	if interaction.Status != StatusPending {
		return nil, fmt.Errorf("interaction is %s", interaction.Status)
	}
	if err := validateResponse(*interaction, response); err != nil {
		return nil, err
	}
	respondedAt := s.now().UTC()
	if response.RespondedAt != nil {
		respondedAt = response.RespondedAt.UTC()
	}
	response.InteractionID = interaction.InteractionID
	response.RespondedAt = &respondedAt
	interaction.Status = StatusAnswered
	interaction.RespondedAt = &respondedAt
	interaction.Response = cloneResponse(&response)
	if err := s.save(interaction); err != nil {
		return nil, err
	}
	s.untrackPending(*interaction)
	return cloneInteraction(interaction), nil
}

// Get returns an interaction by id.
func (s *Service) Get(_ Context, interactionID string) (*Interaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	interaction, ok := s.interactions[interactionID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneInteraction(interaction), nil
}

// Cancel marks a pending interaction canceled and asks the provider to cancel delivery.
func (s *Service) Cancel(ctx Context, interactionID string, reason string) (*Interaction, error) {
	interaction, ok := s.getMutable(interactionID)
	if !ok {
		return nil, ErrNotFound
	}
	if interaction.Status != StatusPending {
		return cloneInteraction(interaction), nil
	}
	if s.provider != nil {
		if err := s.provider.Cancel(ctx, *cloneInteraction(interaction)); err != nil {
			return nil, err
		}
	}
	interaction.Status = StatusCanceled
	interaction.CancelReason = strings.TrimSpace(reason)
	if err := s.save(interaction); err != nil {
		return nil, err
	}
	s.untrackPending(*interaction)
	return cloneInteraction(interaction), nil
}

// ExpirePending marks overdue pending interactions expired.
func (s *Service) ExpirePending(_ Context, now time.Time) ([]Interaction, error) {
	now = now.UTC()
	var expired []Interaction
	for _, id := range s.pendingIDs() {
		interaction, ok := s.getMutable(id)
		if !ok || interaction.Status != StatusPending || interaction.ExpiresAt == nil || interaction.ExpiresAt.After(now) {
			continue
		}
		interaction.Status = StatusExpired
		if err := s.save(interaction); err != nil {
			return expired, err
		}
		s.untrackPending(*interaction)
		expired = append(expired, *cloneInteraction(interaction))
	}
	return expired, nil
}

func (s *Service) trackPending(interaction Interaction) {
	if provider, ok := s.provider.(PendingProvider); ok {
		provider.TrackPending(interaction, s)
	}
}

func (s *Service) untrackPending(interaction Interaction) {
	if provider, ok := s.provider.(PendingProvider); ok {
		provider.UntrackPending(interaction)
	}
}

func normalizeInstanceID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultInstanceLocal
	}
	return trimmed
}

func validateRequest(request Request) error {
	switch request.Kind {
	case KindApprovalRequest, KindQuestion:
	default:
		return fmt.Errorf("unsupported interaction kind %q", request.Kind)
	}
	if strings.TrimSpace(request.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(request.Message) == "" {
		return fmt.Errorf("message is required")
	}
	switch expectedResponse(request) {
	case ExpectedApproval, ExpectedText:
		return nil
	default:
		return fmt.Errorf("unsupported expected response %q", request.ExpectedResponse)
	}
}

func expectedResponse(request Request) string {
	if strings.TrimSpace(request.ExpectedResponse) != "" {
		return request.ExpectedResponse
	}
	if request.Kind == KindApprovalRequest {
		return ExpectedApproval
	}
	return ExpectedText
}

func validateResponse(interaction Interaction, response Response) error {
	switch interaction.ExpectedResponse {
	case ExpectedApproval:
		if response.Approved == nil {
			return fmt.Errorf("approved is required")
		}
	case ExpectedText:
		if strings.TrimSpace(response.Answer) == "" {
			return fmt.Errorf("answer is required")
		}
	default:
		return fmt.Errorf("unsupported expected response %q", interaction.ExpectedResponse)
	}
	return nil
}

func (s *Service) getMutable(id string) (*Interaction, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	interaction, ok := s.interactions[id]
	return interaction, ok
}

func (s *Service) pendingIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.interactions))
	for id, interaction := range s.interactions {
		if interaction.Status == StatusPending {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Service) save(interaction *Interaction) error {
	if s.store == nil {
		return fmt.Errorf("interaction store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := cloneInteraction(interaction)
	s.interactions[interaction.InteractionID] = copied
	return s.store.Save(*copied)
}

func (s *Service) setDelivery(interactionID string, delivery *Delivery) error {
	if delivery == nil {
		return nil
	}
	interaction, ok := s.getMutable(interactionID)
	if !ok {
		return ErrNotFound
	}
	interaction.Delivery = cloneDelivery(delivery)
	return s.save(interaction)
}

func cloneInteraction(interaction *Interaction) *Interaction {
	if interaction == nil {
		return nil
	}
	copied := *interaction
	copied.ExpiresAt = cloneTime(interaction.ExpiresAt)
	copied.RespondedAt = cloneTime(interaction.RespondedAt)
	copied.Response = cloneResponse(interaction.Response)
	copied.CancelReason = interaction.CancelReason
	copied.Delivery = cloneDelivery(interaction.Delivery)
	copied.Metadata = copyMetadata(interaction.Metadata)
	return &copied
}

func cloneResponse(response *Response) *Response {
	if response == nil {
		return nil
	}
	copied := *response
	if response.Approved != nil {
		approved := *response.Approved
		copied.Approved = &approved
	}
	copied.RespondedAt = cloneTime(response.RespondedAt)
	return &copied
}

func cloneDelivery(delivery *Delivery) *Delivery {
	if delivery == nil {
		return nil
	}
	copied := *delivery
	return &copied
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}

func newID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
