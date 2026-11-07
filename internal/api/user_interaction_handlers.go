package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"proxyma/internal/agents"
	"proxyma/internal/ids"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/users"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleUserInteractions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateUserInteraction(w, r)
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
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	if strings.TrimSpace(paths.InteractionConfigDir) == "" {
		writeError(w, http.StatusInternalServerError, "interaction config directory is not configured")
		return
	}
	registry, diagnostics, err := interactioninstances.LoadDir(paths.InteractionConfigDir)
	if err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if len(diagnostics) > 0 {
		writeInteractionMutationError(w, validationError("interaction instances invalid: "+diagnostics[0].Path+": "+diagnostics[0].Message))
		return
	}
	items := registry.All()
	if !user.Admin {
		owned := make([]interactioninstances.InstanceDefinition, 0, len(items))
		for _, definition := range items {
			if s.canAccessInteractionInstance(user, definition.Metadata()) {
				owned = append(owned, definition)
			}
		}
		items = owned
	}
	response, err := userInteractionsResponse(items, s.userRegistry(), s.agentRegistry())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleUserInteraction(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/interactions/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "interaction instance not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUserInteraction(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUserInteraction(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateUserInteraction(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(paths.InteractionConfigDir) == "" {
		writeError(w, http.StatusInternalServerError, "interaction config directory is not configured")
		return
	}
	request, ok := decodeInteractionMutationRequest(w, r)
	if !ok {
		return
	}
	instance, err := ntfyFromInteractionMutationRequest(request, user, nil)
	if err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if err := s.validateInteractionCreator(instance.CreatorUserID); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	created, err := interactioninstances.CreateNtfy(paths.InteractionConfigDir, instance, nil)
	if err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if err := s.refreshInteractionProvider(paths); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, safeUserInteractionResponse(created, s.userRegistry(), s.agentRegistry()))
}

func (s *Server) handleUpdateUserInteraction(w http.ResponseWriter, r *http.Request, id string) {
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
	if strings.TrimSpace(paths.InteractionConfigDir) == "" {
		writeError(w, http.StatusInternalServerError, "interaction config directory is not configured")
		return
	}
	_, existing, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "interaction instance not found")
		return
	}
	if !s.canAccessInteractionInstance(user, existing.Metadata()) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "interaction instance is not accessible to the current user", Code: "forbidden"})
		return
	}
	request, ok := decodeInteractionMutationRequest(w, r)
	if !ok {
		return
	}
	updated, err := ntfyFromInteractionMutationRequest(request, user, &existing)
	if err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if strings.TrimSpace(request.Token) == "" {
		updated.Token = existing.Token
	}
	if err := s.validateInteractionCreator(updated.CreatorUserID); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if !updated.Enabled {
		references := referencedAgentsForInteraction(s.agentRegistry(), id)
		if len(references) > 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error: fmt.Sprintf("interaction instance %q is referenced by agent(s): %s", id, strings.Join(agentReferenceLabels(references), ", ")),
				Code:  "interaction_referenced",
			})
			return
		}
	}
	updated, err = interactioninstances.UpdateNtfy(paths.InteractionConfigDir, id, updated)
	if err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if err := s.refreshInteractionProvider(paths); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, safeUserInteractionResponse(updated, s.userRegistry(), s.agentRegistry()))
}

func (s *Server) handleDeleteUserInteraction(w http.ResponseWriter, r *http.Request, id string) {
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
	if strings.TrimSpace(paths.InteractionConfigDir) == "" {
		writeError(w, http.StatusInternalServerError, "interaction config directory is not configured")
		return
	}
	_, existing, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "interaction instance not found")
		return
	}
	if !s.canAccessInteractionInstance(user, existing.Metadata()) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "interaction instance is not accessible to the current user", Code: "forbidden"})
		return
	}
	references := referencedAgentsForInteraction(s.agentRegistry(), id)
	if len(references) > 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: fmt.Sprintf("interaction instance %q is referenced by agent(s): %s", id, strings.Join(agentReferenceLabels(references), ", ")),
			Code:  "interaction_referenced",
		})
		return
	}
	if err := interactioninstances.DeleteNtfy(paths.InteractionConfigDir, id); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	if err := s.refreshInteractionProvider(paths); err != nil {
		writeInteractionMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func userInteractionsResponse(items []interactioninstances.InstanceDefinition, userRegistry users.Registry, agentRegistry agents.Registry) (UserInteractionsResponse, error) {
	sort.Slice(items, func(i, j int) bool {
		leftMetadata := items[i].Metadata()
		left := strings.TrimSpace(leftMetadata.DisplayName)
		if left == "" {
			left = leftMetadata.ID
		}
		rightMetadata := items[j].Metadata()
		right := strings.TrimSpace(rightMetadata.DisplayName)
		if right == "" {
			right = rightMetadata.ID
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})
	response := UserInteractionsResponse{Interactions: make([]UserInteractionResponse, 0, len(items))}
	for _, definition := range items {
		switch instance := definition.(type) {
		case interactioninstances.NtfyInstance:
			response.Interactions = append(response.Interactions, safeUserInteractionResponse(instance, userRegistry, agentRegistry))
		default:
			metadata := definition.Metadata()
			return UserInteractionsResponse{}, fmt.Errorf("unsupported %s interaction instance %q", metadata.ProviderType, metadata.ID)
		}
	}
	return response, nil
}

func safeUserInteractionResponse(instance interactioninstances.NtfyInstance, userRegistry users.Registry, agentRegistry agents.Registry) UserInteractionResponse {
	return UserInteractionResponse{
		ID:                 instance.ID,
		Type:               interactioninstances.ProviderNtfy,
		CreatorUserID:      instance.CreatorUserID,
		CreatorLabel:       interactionCreatorLabel(instance.CreatorUserID, userRegistry),
		DisplayName:        instance.DisplayName,
		Enabled:            instance.Enabled,
		BaseURL:            instance.BaseURL,
		RequestTopic:       instance.RequestTopic,
		ResponseTopic:      instance.ResponseTopic,
		PollInterval:       instance.PollInterval.String(),
		PollTimeout:        instance.PollTimeout.String(),
		RespondedBy:        instance.RespondedBy,
		TokenConfigured:    strings.TrimSpace(instance.Token) != "",
		ReferencedAgentIDs: referencedAgentIDsForInteraction(agentRegistry, instance.ID),
	}
}

func interactionCreatorLabel(userID string, registry users.Registry) string {
	user, found := registry.Find(userID)
	if !found {
		return userID
	}
	if strings.TrimSpace(user.Name) != "" {
		return user.Name
	}
	return user.Username
}

func (s *Server) canAccessInteractionInstance(user users.User, metadata interactioninstances.InstanceMetadata) bool {
	if user.Admin {
		return true
	}
	if strings.TrimSpace(metadata.CreatorUserID) == user.ID {
		return true
	}
	for _, agent := range referencedAgentsForInteraction(s.agentRegistry(), metadata.ID) {
		if containsUserID(agent.UserIDs, user.ID) {
			return true
		}
	}
	return false
}

func referencedAgentIDsForInteraction(registry agents.Registry, instanceID string) []string {
	references := referencedAgentsForInteraction(registry, instanceID)
	ids := make([]string, 0, len(references))
	for _, agent := range references {
		ids = append(ids, agent.ID)
	}
	return ids
}

func referencedAgentsForInteraction(registry agents.Registry, instanceID string) []agents.Agent {
	instanceID = interactioninstances.NormalizeInstanceID(instanceID)
	var references []agents.Agent
	for _, agent := range registry.All() {
		if interactioninstances.NormalizeInstanceID(agent.InteractionInstanceID) == instanceID {
			references = append(references, agent)
		}
	}
	sort.Slice(references, func(i, j int) bool {
		return strings.ToLower(agentLabelForReference(references[i])) < strings.ToLower(agentLabelForReference(references[j]))
	})
	return references
}

func agentReferenceLabels(items []agents.Agent) []string {
	labels := make([]string, 0, len(items))
	for _, agent := range items {
		labels = append(labels, agentLabelForReference(agent))
	}
	return labels
}

func agentLabelForReference(agent agents.Agent) string {
	if strings.TrimSpace(agent.DisplayName) != "" {
		return fmt.Sprintf("%s (%s)", agent.DisplayName, agent.ID)
	}
	return agent.ID
}

func decodeInteractionMutationRequest(w http.ResponseWriter, r *http.Request) (UserInteractionMutationRequest, bool) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxInteractionBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserInteractionMutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON interaction request", Code: "invalid_interaction"})
		return UserInteractionMutationRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON interaction request", Code: "invalid_interaction"})
		return UserInteractionMutationRequest{}, false
	}
	return request, true
}

func ntfyFromInteractionMutationRequest(request UserInteractionMutationRequest, currentUser users.User, existing *interactioninstances.NtfyInstance) (interactioninstances.NtfyInstance, error) {
	providerType := strings.TrimSpace(request.Type)
	if providerType == "" {
		providerType = interactioninstances.ProviderNtfy
	}
	if providerType != interactioninstances.ProviderNtfy {
		return interactioninstances.NtfyInstance{}, validationError("interaction type must be ntfy")
	}
	creatorUserID := strings.TrimSpace(request.CreatorUserID)
	if creatorUserID == "" || !currentUser.Admin {
		creatorUserID = currentUser.ID
	}
	enabled := true
	baseURL := strings.TrimSpace(request.BaseURL)
	pollIntervalRaw := strings.TrimSpace(request.PollInterval)
	pollTimeoutRaw := strings.TrimSpace(request.PollTimeout)
	respondedBy := strings.TrimSpace(request.RespondedBy)
	if existing != nil {
		enabled = existing.Enabled
		if baseURL == "" {
			baseURL = existing.BaseURL
		}
		if pollIntervalRaw == "" {
			pollIntervalRaw = existing.PollInterval.String()
		}
		if pollTimeoutRaw == "" {
			pollTimeoutRaw = existing.PollTimeout.String()
		}
		if respondedBy == "" {
			respondedBy = existing.RespondedBy
		}
		if strings.TrimSpace(request.CreatorUserID) == "" {
			creatorUserID = existing.CreatorUserID
		}
	}
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if baseURL == "" {
		baseURL = "https://ntfy.sh"
	}
	if pollIntervalRaw == "" {
		pollIntervalRaw = "5s"
	}
	if pollTimeoutRaw == "" {
		pollTimeoutRaw = "30s"
	}
	if respondedBy == "" {
		respondedBy = "ntfy"
	}
	pollInterval, err := time.ParseDuration(pollIntervalRaw)
	if err != nil {
		return interactioninstances.NtfyInstance{}, validationError("poll_interval must be a Go duration")
	}
	pollTimeout, err := time.ParseDuration(pollTimeoutRaw)
	if err != nil {
		return interactioninstances.NtfyInstance{}, validationError("poll_timeout must be a Go duration")
	}
	instance := interactioninstances.NtfyInstance{
		CreatorUserID: creatorUserID,
		DisplayName:   strings.TrimSpace(request.DisplayName),
		Enabled:       enabled,
		BaseURL:       baseURL,
		RequestTopic:  strings.TrimSpace(request.RequestTopic),
		ResponseTopic: strings.TrimSpace(request.ResponseTopic),
		Token:         strings.TrimSpace(request.Token),
		PollInterval:  pollInterval,
		PollTimeout:   pollTimeout,
		RespondedBy:   respondedBy,
	}
	if existing != nil {
		instance.Schema = existing.Schema
		if instance.DisplayName == "" {
			instance.DisplayName = existing.DisplayName
		}
		if instance.RequestTopic == "" {
			instance.RequestTopic = existing.RequestTopic
		}
		if instance.ResponseTopic == "" {
			instance.ResponseTopic = existing.ResponseTopic
		}
	}
	return instance, nil
}

func (s *Server) refreshInteractionProvider(paths users.Paths) error {
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
		if err := s.validateInteractionCreator(definition.Metadata().CreatorUserID); err != nil {
			return err
		}
	}
	if s.InteractionProvider == nil {
		return nil
	}
	providers, err := interactioninstances.BuildProviders(registry, interactioninstances.ProviderRuntimeOptions{
		Debug:  s.Debug,
		Logger: s.Logger,
	})
	if err != nil {
		return err
	}
	s.interactionProvidersMu.Lock()
	defer s.interactionProvidersMu.Unlock()
	s.InteractionProvider.SetProviders(providers)
	return nil
}

func (s *Server) validateInteractionCreator(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return validationError("creator_user_id is required")
	}
	if !ids.IsUUIDV4(strings.TrimSpace(userID)) {
		return validationError("creator_user_id must be a UUID v4")
	}
	if _, found := s.userRegistry().Find(userID); !found {
		return validationError(fmt.Sprintf("unknown user id %q", userID))
	}
	return nil
}
