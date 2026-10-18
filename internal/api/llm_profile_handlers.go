package api

import (
	"net/http"
	"strings"

	"proxyma/internal/ids"
	"proxyma/internal/llm"
	"proxyma/internal/users"
)

func (s *Server) handleUserLLMProfiles(w http.ResponseWriter, r *http.Request) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, configured := s.managementPaths()
	if !configured || strings.TrimSpace(paths.LLMProfileDir) == "" {
		writeError(w, http.StatusInternalServerError, "LLM profile directory is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		profiles, err := users.ReadLLMProfiles(paths)
		if err != nil {
			writeUserMutationError(w, err)
			return
		}
		response := UserLLMProfilesResponse{Profiles: make([]llm.PublicProfile, 0, len(profiles))}
		for _, profile := range profiles {
			if users.CanUseLLMProfile(profile, user.ID, user.Admin) {
				response.Profiles = append(response.Profiles, profile.Public())
			}
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPost:
		request, ok := decodeLLMProfileMutationRequest(w, r, false)
		if !ok {
			return
		}
		profile, err := profileFromMutationRequest(request, user.ID)
		if err != nil {
			writeUserMutationError(w, err)
			return
		}
		result, err := users.CreateLLMProfile(paths, profile, nil)
		if err != nil {
			writeUserMutationError(w, err)
			return
		}
		if err := s.refreshLLMProfilesAndPolicies(paths); err != nil {
			writeUserMutationError(w, err)
			return
		}
		created, err := findLLMProfile(paths, result.ID)
		if err != nil {
			writeUserMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, created.Public())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUserLLMProfile(w http.ResponseWriter, r *http.Request) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/llm-profiles/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 1 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "LLM profile not found")
		return
	}
	id := strings.TrimSpace(parts[0])
	paths, configured := s.managementPaths()
	if !configured || strings.TrimSpace(paths.LLMProfileDir) == "" {
		writeError(w, http.StatusInternalServerError, "LLM profile directory is not configured")
		return
	}
	profile, err := findLLMProfile(paths, id)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !users.CanUseLLMProfile(profile, user.ID, user.Admin) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "LLM profile is not available to this user", Code: "forbidden"})
			return
		}
		writeJSON(w, http.StatusOK, profile.Public())
	case http.MethodPut:
		if !users.CanModifyLLMProfile(profile, user.ID, user.Admin) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "only the LLM profile owner or an administrator may modify this profile", Code: "forbidden"})
			return
		}
		request, ok := decodeLLMProfileMutationRequest(w, r, true)
		if !ok {
			return
		}
		updated := llm.Profile{
			ID:             profile.ID,
			OwnerUserID:    profile.OwnerUserID,
			Name:           request.Name,
			Provider:       request.Provider,
			Model:          request.Model,
			BaseURL:        request.BaseURL,
			APIKey:         request.APIKey,
			TimeoutSeconds: request.TimeoutSeconds,
			Enabled:        mutationEnabled(request.Enabled, profile.Enabled),
		}
		if _, err := users.UpdateLLMProfile(paths, id, updated); err != nil {
			writeUserMutationError(w, err)
			return
		}
		if err := s.refreshLLMProfilesAndPolicies(paths); err != nil {
			writeUserMutationError(w, err)
			return
		}
		updated, err = findLLMProfile(paths, id)
		if err != nil {
			writeUserMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated.Public())
	case http.MethodDelete:
		if !users.CanModifyLLMProfile(profile, user.ID, user.Admin) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "only the LLM profile owner or an administrator may delete this profile", Code: "forbidden"})
			return
		}
		if _, err := users.DeleteLLMProfile(paths, id); err != nil {
			writeUserMutationError(w, err)
			return
		}
		if err := s.refreshLLMProfilesAndPolicies(paths); err != nil {
			writeUserMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTestUserLLMProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	s.testLLMProfile(w, r, user)
}

func (s *Server) testLLMProfile(w http.ResponseWriter, r *http.Request, user users.User) {
	request, ok := decodeLLMProfileTestRequest(w, r)
	if !ok {
		return
	}
	id, err := ids.NewUUIDV4()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create temporary LLM profile id")
		return
	}
	profile := llm.Profile{ID: id, OwnerUserID: user.ID, Name: request.Name, Provider: request.Provider, Model: request.Model, BaseURL: request.BaseURL, APIKey: request.APIKey, TimeoutSeconds: request.TimeoutSeconds, Enabled: true}
	if profile.BaseURL == "" {
		profile.BaseURL = llm.DefaultBaseURL(profile.Provider)
	}
	if err := llm.ValidateProfile(&profile); err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := llm.TestProfile(r.Context(), profile); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "LLM connection test failed", Code: "llm_test_failed"})
		return
	}
	writeJSON(w, http.StatusOK, UserLLMProfileTestResponse{Status: "ok"})
}

func decodeLLMProfileMutationRequest(w http.ResponseWriter, r *http.Request, update bool) (UserLLMProfileMutationRequest, bool) {
	var request UserLLMProfileMutationRequest
	if !decodeStrictJSON(w, r, &request, maxPolicyBodyBytes, "invalid JSON LLM profile request") {
		return UserLLMProfileMutationRequest{}, false
	}
	if strings.TrimSpace(request.BaseURL) == "" {
		request.BaseURL = llm.DefaultBaseURL(request.Provider)
	}
	if !update && request.Enabled == nil {
		enabled := true
		request.Enabled = &enabled
	}
	return request, true
}

func decodeLLMProfileTestRequest(w http.ResponseWriter, r *http.Request) (UserLLMProfileTestRequest, bool) {
	var request UserLLMProfileTestRequest
	if !decodeStrictJSON(w, r, &request, maxPolicyBodyBytes, "invalid JSON LLM profile test request") {
		return UserLLMProfileTestRequest{}, false
	}
	if strings.TrimSpace(request.BaseURL) == "" {
		request.BaseURL = llm.DefaultBaseURL(request.Provider)
	}
	return request, true
}

func profileFromMutationRequest(request UserLLMProfileMutationRequest, ownerID string) (llm.Profile, error) {
	return llm.Profile{OwnerUserID: ownerID, Name: strings.TrimSpace(request.Name), Provider: request.Provider, Model: strings.TrimSpace(request.Model), BaseURL: strings.TrimSpace(request.BaseURL), APIKey: request.APIKey, TimeoutSeconds: request.TimeoutSeconds, Enabled: mutationEnabled(request.Enabled, true)}, nil
}

func mutationEnabled(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func findLLMProfile(paths users.Paths, id string) (llm.Profile, error) {
	profiles, err := users.ReadLLMProfiles(paths)
	if err != nil {
		return llm.Profile{}, err
	}
	for _, profile := range profiles {
		if profile.ID == strings.TrimSpace(id) {
			return profile, nil
		}
	}
	return llm.Profile{}, notFoundError("LLM profile does not exist")
}
