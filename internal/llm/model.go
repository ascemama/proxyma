// Package llm provides the small provider-neutral boundary used by policy evaluation.
package llm

import "context"

type Provider string

const (
	ProviderOpenAI     Provider = "openai"
	ProviderOpenRouter Provider = "openrouter"
	ProviderClaude     Provider = "claude"
)

const (
	DecisionApproved        = "approved"
	DecisionDenied          = "denied"
	DecisionRequireApproval = "require approval"
)

// Profile is persisted server-side. API keys must never be copied into public responses.
type Profile struct {
	ID             string   `json:"id"`
	OwnerUserID    string   `json:"owner_user_id"`
	Name           string   `json:"name"`
	Provider       Provider `json:"provider"`
	Model          string   `json:"model"`
	BaseURL        string   `json:"base_url"`
	APIKey         string   `json:"api_key"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Enabled        bool     `json:"enabled"`
}

// PublicProfile is safe to return through the management API.
type PublicProfile struct {
	ID             string   `json:"id"`
	OwnerUserID    string   `json:"owner_user_id"`
	Name           string   `json:"name"`
	Provider       Provider `json:"provider"`
	Model          string   `json:"model"`
	BaseURL        string   `json:"base_url"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Enabled        bool     `json:"enabled"`
	HasAPIKey      bool     `json:"has_api_key"`
}

type ReviewRequest struct {
	Command string
	Prompt  string
}

type ReviewResult struct {
	Decision string
	Valid    bool
}

func DefaultBaseURL(provider Provider) string {
	switch provider {
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	case ProviderClaude:
		return "https://api.anthropic.com/v1"
	default:
		return ""
	}
}

// Reviewer is intentionally narrower than a general-purpose LLM client.
type Reviewer interface {
	Review(context.Context, string, ReviewRequest) (ReviewResult, error)
}

func (p Profile) Public() PublicProfile {
	return PublicProfile{
		ID: p.ID, OwnerUserID: p.OwnerUserID, Name: p.Name, Provider: p.Provider,
		Model: p.Model, BaseURL: p.BaseURL, TimeoutSeconds: p.TimeoutSeconds,
		Enabled: p.Enabled, HasAPIKey: p.APIKey != "",
	}
}
