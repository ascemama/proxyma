package api

import (
	"context"

	"proxyma/internal/agents"
)

type agentContextKey struct{}

func contextWithAgent(ctx context.Context, agent agents.Agent) context.Context {
	return context.WithValue(ctx, agentContextKey{}, agent)
}

func agentFromContext(ctx context.Context) (agents.Agent, bool) {
	agent, ok := ctx.Value(agentContextKey{}).(agents.Agent)
	return agent, ok && agent.ID != ""
}
