// Package agents loads Proxyma agent identities and validates Proxyma JWTs.
package agents

import (
	"strings"

	"proxyma/internal/ids"
	"proxyma/internal/interaction"
)

// Agent represents one trusted Proxyma API caller.
type Agent struct {
	Schema                string   `json:"$schema,omitempty"`
	ID                    string   `json:"id"`
	DisplayName           string   `json:"display_name,omitempty"`
	AgentJWTSigningKey    string   `json:"agent_jwt_signing_key"`
	Enabled               bool     `json:"enabled"`
	UserIDs               []string `json:"user_ids"`
	InteractionInstanceID string   `json:"interaction_instance_id,omitempty"`
}

// Diagnostic describes a rejected agent file.
type Diagnostic struct {
	Path    string
	Message string
}

// Registry stores known Proxyma agents by ID.
type Registry struct {
	agents map[string]Agent
}

// NewRegistry returns a registry containing the provided agents.
func NewRegistry(items []Agent) Registry {
	agentsByID := make(map[string]Agent, len(items))
	for _, agent := range items {
		agent = Normalize(agent)
		agentsByID[normalizeID(agent.ID)] = agent
	}
	return Registry{agents: agentsByID}
}

// Normalize returns an agent with canonical optional fields filled in.
func Normalize(agent Agent) Agent {
	agent.ID = normalizeID(agent.ID)
	agent.InteractionInstanceID = strings.TrimSpace(agent.InteractionInstanceID)
	if agent.InteractionInstanceID == "" {
		agent.InteractionInstanceID = interaction.DefaultInstanceLocal
	}
	return agent
}

// ValidateInteractionInstanceID validates the optional agent interaction reference format.
func ValidateInteractionInstanceID(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == interaction.DefaultInstanceLocal || ids.IsUUIDV4(trimmed)
}

// All returns a copy of all known agents.
func (r Registry) All() []Agent {
	out := make([]Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		out = append(out, agent)
	}
	return out
}

// Find returns the agent with the given ID.
func (r Registry) Find(id string) (Agent, bool) {
	if r.agents == nil {
		return Agent{}, false
	}
	agent, ok := r.agents[normalizeID(id)]
	return agent, ok
}

// HasEnabled reports whether at least one loaded agent can authenticate.
func (r Registry) HasEnabled() bool {
	for _, agent := range r.agents {
		if agent.Enabled {
			return true
		}
	}
	return false
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}
