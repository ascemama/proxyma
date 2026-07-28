package api

import (
	"net/http"
	"strings"

	"proxyma/internal/agents"
	"proxyma/internal/interaction"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
)

func canSubmitCommand(agent agents.Agent, target targets.Target) bool {
	for _, allowed := range target.AgentIDs {
		if strings.TrimSpace(allowed) == agent.ID {
			return true
		}
	}
	return false
}

func canReadTask(agent agents.Agent, task *tasks.Task) bool {
	return task != nil && strings.TrimSpace(task.AgentID) != "" && task.AgentID == agent.ID
}

func canUseInteraction(agent agents.Agent, record *interaction.Interaction, task *tasks.Task) bool {
	return record != nil && strings.TrimSpace(record.TaskID) != "" && canReadTask(agent, task)
}

func forbiddenError(message string) error {
	return requestError{status: http.StatusForbidden, payload: ErrorResponse{Error: message}}
}
