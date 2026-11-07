package api

import (
	"encoding/json"
	"io"
	"net/http"
	"proxyma/internal/users"
	"sort"
	"strings"
	"time"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateUser(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, userUsersResponse(s.userRegistry().All()))
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/users/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "activation-link" && r.Method == http.MethodPost {
		s.handleRegenerateActivationLink(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "password-reset-link" && r.Method == http.MethodPost {
		s.handlePasswordResetLink(w, r, parts[0])
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUser(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodeUserMutationRequest(w, r)
	if !ok {
		return
	}
	ttl, err := s.activationLifetime(request.ActivationTokenLifetimeSeconds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_activation_lifetime"})
		return
	}
	if s.CredentialTokens == nil {
		writeError(w, http.StatusInternalServerError, "user activation store is not configured")
		return
	}
	user, err := userFromMutationRequest(request, nil)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := validateUsernameAvailable(s.userRegistry(), "", user.Username); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
		return
	}
	result, err := users.CreateUser(paths, user, nil)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	record, token, err := s.CredentialTokens.Create(result.ID, users.CredentialTokenPurposeActivation, ttl, time.Now())
	if err != nil {
		_, _ = users.DeleteUser(paths, result.ID)
		writeError(w, http.StatusInternalServerError, "could not create user activation")
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeUserMutationError(w, err)
		return
	}
	created, err := readStoredUser(paths, result.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created user could not be read")
		return
	}
	response := safeUserProfileResponse(created)
	response.ActivationURL = activationURL(token)
	response.ActivationExpiresAt = &record.ExpiresAt
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request, id string) {
	currentUser, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readStoredUser(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	request, ok := decodeUserMutationRequest(w, r)
	if !ok {
		return
	}
	user, err := userFromMutationRequest(request, &existing)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := validateUserAdminChange(s.userRegistry(), currentUser, existing, user); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
		return
	}
	if err := validateUsernameAvailable(s.userRegistry(), existing.ID, user.Username); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
		return
	}
	if _, err := users.UpdateUser(paths, id, user); err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeUserMutationError(w, err)
		return
	}
	updated, err := readStoredUser(paths, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated user could not be read")
		return
	}
	writeJSON(w, http.StatusOK, safeUserProfileResponse(updated))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, id string) {
	currentUser, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readStoredUser(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err := validateUserDelete(s.userRegistry(), currentUser, existing); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
		return
	}
	if _, err := users.DeleteUser(paths, id); err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeUserMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func userUsersResponse(items []users.User) UserUsersResponse {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Username < items[j].Username
	})
	response := UserUsersResponse{Users: make([]UserProfileResponse, 0, len(items))}
	for _, user := range items {
		response.Users = append(response.Users, safeUserProfileResponse(user))
	}
	return response
}

func safeUserProfileResponse(user users.User) UserProfileResponse {
	return UserProfileResponse{
		ID:       user.ID,
		Username: user.Username,
		Name:     user.Name,
		Admin:    user.Admin,
		Status:   user.Status,
		Theme:    users.EffectiveTheme(user.Theme),
	}
}

func decodeUserMutationRequest(w http.ResponseWriter, r *http.Request) (UserMutationRequest, bool) {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUserBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserMutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON user request", Code: "invalid_user"})
		return UserMutationRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON user request", Code: "invalid_user"})
		return UserMutationRequest{}, false
	}
	return request, true
}

func userFromMutationRequest(request UserMutationRequest, existing *users.User) (users.User, error) {
	user := users.User{
		Username: strings.TrimSpace(request.Username),
		Name:     strings.TrimSpace(request.Name),
		Status:   users.StatusPendingActivation,
	}
	if request.Admin != nil {
		user.Admin = *request.Admin
	}
	if existing != nil {
		if request.Admin == nil {
			user.Admin = existing.Admin
		}
		user.PasswdHash = existing.PasswdHash
		user.Status = existing.Status
	}
	return user, nil
}

func validateUserAdminChange(registry users.Registry, currentUser users.User, existing users.User, updated users.User) error {
	if currentUser.ID == existing.ID && existing.Admin && !updated.Admin {
		return validationError("current administrator cannot be demoted")
	}
	if existing.Admin && !updated.Admin && adminCount(registry) <= 1 {
		return validationError("at least one administrator is required")
	}
	return nil
}

func validateUserDelete(registry users.Registry, currentUser users.User, existing users.User) error {
	if currentUser.ID == existing.ID {
		return validationError("current user cannot be deleted")
	}
	if existing.Admin && adminCount(registry) <= 1 {
		return validationError("at least one administrator is required")
	}
	return nil
}

func adminCount(registry users.Registry) int {
	count := 0
	for _, user := range registry.All() {
		if user.Admin {
			count++
		}
	}
	return count
}

func validateUsernameAvailable(registry users.Registry, currentUserID string, username string) error {
	existing, found := registry.FindByUsername(username)
	if found && existing.ID != currentUserID {
		return validationError("username is already in use")
	}
	return nil
}
