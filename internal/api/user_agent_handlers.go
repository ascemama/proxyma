package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"proxyma/internal/agents"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/users"
	"sort"
	"strings"
)

func (s *Server) handleUserAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateUserAgent(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	items := s.agentRegistry().All()
	if !user.Admin {
		owned := make([]agents.Agent, 0, len(items))
		for _, agent := range items {
			if containsUserID(agent.UserIDs, user.ID) {
				owned = append(owned, agent)
			}
		}
		items = owned
	}
	writeJSON(w, http.StatusOK, userAgentsResponse(items))
}

func (s *Server) handleUserAgent(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/agents/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUserAgent(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUserAgent(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateUserAgent(w http.ResponseWriter, r *http.Request) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodeAgentMutationRequest(w, r)
	if !ok {
		return
	}
	agent := agentFromMutationRequest(request, user.ID, nil)
	if err := validateAgentInteractionAssignment(paths, agent.InteractionInstanceID, user.ID); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	result, err := users.CreateAgent(paths, agent, nil)
	if err != nil {
		writeAgentMutationError(w, err)
		return
	}
	created, err := readStoredAgent(paths, result.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created agent could not be read")
		return
	}
	if err := s.refreshAgentRegistry(paths); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, safeUserAgentResponse(created))
}

func (s *Server) handleUpdateUserAgent(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readStoredAgent(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "agent is not owned by the current user", Code: "forbidden"})
		return
	}
	request, ok := decodeAgentMutationRequest(w, r)
	if !ok {
		return
	}
	agent := agentFromMutationRequest(request, user.ID, &existing)
	if interactioninstances.NormalizeInstanceID(agent.InteractionInstanceID) != interactioninstances.NormalizeInstanceID(existing.InteractionInstanceID) {
		if err := validateAgentInteractionAssignment(paths, agent.InteractionInstanceID, user.ID); err != nil {
			writeAgentMutationError(w, err)
			return
		}
	}
	if _, err := users.UpdateAgent(paths, id, agent); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	updated, err := readStoredAgent(paths, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated agent could not be read")
		return
	}
	if err := s.refreshAgentRegistry(paths); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, safeUserAgentResponse(updated))
}

func (s *Server) handleDeleteUserAgent(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readStoredAgent(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "agent is not owned by the current user", Code: "forbidden"})
		return
	}
	if _, err := users.DeleteAgent(paths, id); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	if err := s.refreshAgentRegistry(paths); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func userAgentsResponse(items []agents.Agent) UserAgentsResponse {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	response := UserAgentsResponse{Agents: make([]UserAgentResponse, 0, len(items))}
	for _, agent := range items {
		response.Agents = append(response.Agents, safeUserAgentResponse(agent))
	}
	return response
}

func safeUserAgentResponse(agent agents.Agent) UserAgentResponse {
	return UserAgentResponse{
		ID:                    agent.ID,
		DisplayName:           agent.DisplayName,
		Enabled:               agent.Enabled,
		UserIDs:               append([]string(nil), agent.UserIDs...),
		InteractionInstanceID: agent.InteractionInstanceID,
	}
}

func decodeAgentMutationRequest(w http.ResponseWriter, r *http.Request) (UserAgentMutationRequest, bool) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAgentBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserAgentMutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON agent request", Code: "invalid_agent"})
		return UserAgentMutationRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON agent request", Code: "invalid_agent"})
		return UserAgentMutationRequest{}, false
	}
	return request, true
}

func agentFromMutationRequest(request UserAgentMutationRequest, userID string, existing *agents.Agent) agents.Agent {
	agent := agents.Agent{
		DisplayName:           strings.TrimSpace(request.DisplayName),
		AgentJWTSigningKey:    request.AgentJWTSigningKey,
		Enabled:               true,
		UserIDs:               []string{userID},
		InteractionInstanceID: strings.TrimSpace(request.InteractionInstanceID),
	}
	if request.Enabled != nil {
		agent.Enabled = *request.Enabled
	}
	if existing != nil {
		agent.UserIDs = append([]string(nil), existing.UserIDs...)
		if strings.TrimSpace(agent.AgentJWTSigningKey) == "" {
			agent.AgentJWTSigningKey = existing.AgentJWTSigningKey
		}
		if request.Enabled == nil {
			agent.Enabled = existing.Enabled
		}
		if strings.TrimSpace(request.InteractionInstanceID) == "" {
			agent.InteractionInstanceID = existing.InteractionInstanceID
		}
	}
	return agents.Normalize(agent)
}

func validateAgentInteractionAssignment(paths users.Paths, instanceID string, userID string) error {
	instanceID = interactioninstances.NormalizeInstanceID(instanceID)
	if instanceID == "local" {
		return nil
	}
	if strings.TrimSpace(paths.InteractionConfigDir) == "" {
		return validationError("interaction config directory is not configured")
	}
	registry, diagnostics, err := interactioninstances.LoadDir(paths.InteractionConfigDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("interaction instances invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	for _, definition := range registry.All() {
		metadata := definition.Metadata()
		if metadata.ID != instanceID {
			continue
		}
		if !metadata.Enabled {
			return validationError(fmt.Sprintf("interaction instance %q is disabled", instanceID))
		}
		if strings.TrimSpace(metadata.CreatorUserID) != strings.TrimSpace(userID) {
			return validationError(fmt.Sprintf("interaction instance %q is not owned by the current user", instanceID))
		}
		return nil
	}
	return validationError(fmt.Sprintf("interaction instance %q was not found", instanceID))
}
