package api

import (
	"net/http"
	"strings"

	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
)

func (s *Server) handleAgentPolicyGuidance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	requested, err := resolveTargetQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, ok := agentFromContext(r.Context())
	if !ok {
		writeAgentAuthError(w, "auth_required", "missing authenticated agent", false, "")
		return
	}
	matchedTarget, err := s.resolveRequestedTarget(&tasks.Target{Hostname: requested.Hostname, IP: requested.IP}, agent.ID)
	if err != nil {
		status, payload := errorResponse(err)
		writeJSON(w, status, payload)
		return
	}
	if !canSubmitCommand(agent, matchedTarget) {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "unknown target"})
		return
	}
	if s.Policies == nil || strings.TrimSpace(matchedTarget.PolicyID) == "" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "target policy unavailable"})
		return
	}
	item, found := s.Policies.Find(matchedTarget.PolicyID)
	if !found {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "target policy unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, agentPolicyGuidanceResponse(matchedTarget, item))
}

func agentPolicyGuidanceResponse(target targets.Target, item policy.Policy) AgentPolicyGuidanceResponse {
	response := AgentPolicyGuidanceResponse{
		Target: ResolvedPolicyTargetResponse{
			ID:        target.ID,
			Transport: target.Transport,
			OS:        target.OS,
			Hostname:  target.Hostname,
			IP:        target.IP,
		},
		Policy: AgentPolicyResponse{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			Enabled:     item.Enabled,
			Blacklist:   make([]AgentPolicyRule, 0),
			Whitelist:   make([]AgentPolicyRule, 0),
		},
	}
	for _, rule := range item.Rules {
		entry := AgentPolicyRule{CommandTemplate: rule.CommandTemplate, Reason: rule.Reason}
		switch rule.Effect {
		case policy.DecisionDeny:
			response.Policy.Blacklist = append(response.Policy.Blacklist, entry)
		case policy.DecisionAllow:
			response.Policy.Whitelist = append(response.Policy.Whitelist, entry)
		}
	}
	return response
}
