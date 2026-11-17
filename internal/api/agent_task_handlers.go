package api

import (
	"net/http"
	"strings"
)

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/agent/tasks/")
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err := requireUUIDV4(id, "task_id"); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_task"})
		return
	}
	task, ok := s.Tasks.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	if !canReadTask(agent, task) {
		writeError(w, http.StatusForbidden, "agent is not authorized for task")
		return
	}
	writeJSON(w, http.StatusOK, taskStatusResponseFromTask(task))
}
