package api

import (
	"errors"
	"net/http"
	"strings"

	"proxyma/internal/interaction"
)

func (s *Server) handleAgentInteractions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/agent/interactions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if s.Interactions == nil || len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	interactionID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		record, err := s.Interactions.Get(r.Context(), interactionID)
		if err != nil {
			writeInteractionError(w, err)
			return
		}
		if err := s.authorizeAgentInteraction(r, record); err != nil {
			status, payload := errorResponse(err)
			writeJSON(w, status, payload)
			return
		}
		writeJSON(w, http.StatusOK, interactionResponseFromInteraction(record))
		return
	}
	writeError(w, http.StatusNotFound, "interaction not found")
}

func (s *Server) authorizeAgentInteraction(r *http.Request, record *interaction.Interaction) error {
	agent, ok := agentFromContext(r.Context())
	if !ok {
		return requestError{status: http.StatusUnauthorized, payload: ErrorResponse{Error: "missing authenticated agent", Code: "auth_required"}}
	}
	if record == nil || strings.TrimSpace(record.TaskID) == "" || s.Tasks == nil {
		return forbiddenError("agent is not authorized for interaction")
	}
	task, ok := s.Tasks.Get(record.TaskID)
	if !ok || !canUseInteraction(agent, record, task) {
		return forbiddenError("agent is not authorized for interaction")
	}
	return nil
}

func writeInteractionError(w http.ResponseWriter, err error) {
	if errors.Is(err, interaction.ErrNotFound) {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
