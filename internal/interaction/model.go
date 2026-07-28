// Package interaction owns Proxyma's asynchronous human interaction boundary.
package interaction

import "time"

const (
	KindApprovalRequest = "approval_request"
	KindQuestion        = "question"
)

const DefaultInstanceLocal = "local"

const (
	ExpectedApproval = "approval"
	ExpectedText     = "text"
)

const (
	StatusPending  = "pending"
	StatusAnswered = "answered"
	StatusExpired  = "expired"
	StatusCanceled = "canceled"
)

// Communicator is the Proxyma-facing facade for async human interactions.
type Communicator interface {
	Request(ctx Context, request Request) (*Interaction, error)
	Respond(ctx Context, response Response) (*Interaction, error)
	Get(ctx Context, interactionID string) (*Interaction, error)
	Cancel(ctx Context, interactionID string, reason string) (*Interaction, error)
	ExpirePending(ctx Context, now time.Time) ([]Interaction, error)
}

// Context is the subset of context.Context used by the public facade.
type Context interface {
	Done() <-chan struct{}
	Err() error
}

// Request describes one safe user-facing approval or question.
type Request struct {
	TaskID                string         `json:"task_id,omitempty"`
	InteractionInstanceID string         `json:"interaction_instance_id,omitempty"`
	Kind                  string         `json:"kind"`
	ExpectedResponse      string         `json:"expected_response,omitempty"`
	Subject               string         `json:"subject"`
	Message               string         `json:"message"`
	RequestedBy           string         `json:"requested_by,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

// Response is the normalized Proxyma-owned response shape.
type Response struct {
	InteractionID string     `json:"interaction_id,omitempty"`
	Approved      *bool      `json:"approved,omitempty"`
	Answer        string     `json:"answer,omitempty"`
	RespondedBy   string     `json:"responded_by,omitempty"`
	RespondedAt   *time.Time `json:"responded_at,omitempty"`
}

// Interaction is a durable Proxyma human-input request.
type Interaction struct {
	InteractionID         string         `json:"interaction_id"`
	TaskID                string         `json:"task_id,omitempty"`
	InteractionInstanceID string         `json:"interaction_instance_id,omitempty"`
	Kind                  string         `json:"kind"`
	ExpectedResponse      string         `json:"expected_response"`
	Subject               string         `json:"subject"`
	Message               string         `json:"message"`
	RequestedBy           string         `json:"requested_by,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
	Status                string         `json:"status"`
	CreatedAt             time.Time      `json:"created_at"`
	ExpiresAt             *time.Time     `json:"expires_at,omitempty"`
	RespondedAt           *time.Time     `json:"responded_at,omitempty"`
	Response              *Response      `json:"response,omitempty"`
	CancelReason          string         `json:"cancel_reason,omitempty"`
	Delivery              *Delivery      `json:"delivery,omitempty"`
}

// Summary is the safe task-visible interaction metadata.
type Summary struct {
	InteractionID string     `json:"interaction_id"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// Delivery records non-secret provider delivery metadata.
type Delivery struct {
	Provider    string    `json:"provider"`
	Reference   string    `json:"reference,omitempty"`
	DeliveredAt time.Time `json:"delivered_at"`
}

// Responder receives normalized asynchronous responses from providers.
type Responder interface {
	Respond(ctx Context, response Response) (*Interaction, error)
	Cancel(ctx Context, interactionID string, reason string) (*Interaction, error)
}

// Provider delivers or cancels user-facing messages for one channel.
type Provider interface {
	Send(ctx Context, interaction Interaction, responder Responder) (*Delivery, error)
	Cancel(ctx Context, interaction Interaction) error
}

// PendingProvider receives durable interaction lifecycle changes. Providers that
// poll or hold background resources can implement it without burdening simple
// delivery-only providers.
type PendingProvider interface {
	TrackPending(interaction Interaction, responder Responder)
	UntrackPending(interaction Interaction)
	Close()
}

// ToSummary returns the safe task-visible subset for an interaction.
func (i Interaction) ToSummary() Summary {
	return Summary{
		InteractionID: i.InteractionID,
		Kind:          i.Kind,
		Status:        i.Status,
		CreatedAt:     i.CreatedAt,
		ExpiresAt:     cloneTime(i.ExpiresAt),
	}
}
