package api

import "net/http"

const agentCommandIdempotencyCapability = "agent_command_idempotency_v1"

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok", Capabilities: []string{agentCommandIdempotencyCapability}})
}
