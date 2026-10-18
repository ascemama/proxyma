package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/ids"
	"proxyma/internal/interaction/history"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/llm"
	"proxyma/internal/remoteexec"
	"proxyma/internal/sshclient"
	"proxyma/internal/store"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"
)

const maxLoginBodyBytes = 16 << 10
const maxUserBodyBytes = 16 << 10
const maxAgentBodyBytes = 16 << 10
const maxTargetBodyBytes = 64 << 10
const maxInteractionBodyBytes = 16 << 10
const maxPolicyBodyBytes = 64 << 10
const defaultCommandHistoryLimit = 20
const maxCommandHistoryLimit = 500

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.UserSessions == nil {
		writeError(w, http.StatusInternalServerError, "user session store is not configured")
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes))
	decoder.DisallowUnknownFields()
	var request LoginRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON login request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON login request")
		return
	}
	if strings.TrimSpace(request.Username) == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	now := time.Now()
	throttleKey := loginThrottleKey(r, request.Username)
	throttle := s.loginThrottle()
	if !throttle.allow(throttleKey, now) {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "too many login attempts", Code: "login_throttled", Retryable: true})
		return
	}

	user, found := s.userRegistry().FindByUsername(request.Username)
	if !found || user.Status != users.StatusActive {
		_ = users.VerifyPassword([]byte(request.Password), dummyPasswordHash)
		throttle.recordFailure(throttleKey, now)
		writeInvalidLogin(w)
		return
	}
	if err := users.VerifyPassword([]byte(request.Password), user.PasswdHash); err != nil {
		throttle.recordFailure(throttleKey, now)
		writeInvalidLogin(w)
		return
	}
	throttle.recordSuccess(throttleKey)
	_ = s.UserSessions.CleanupExpired(now)
	session, cookieValue, csrfToken, err := s.UserSessions.Create(user.ID, s.userSessionTTL(), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create user session")
		return
	}
	s.setUserSessionCookie(w, cookieValue, session.ExpiresAt)
	writeJSON(w, http.StatusOK, s.userSessionResponse(user, session, csrfToken))
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, session, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	rotated, csrfToken, err := s.UserSessions.RotateCSRF(session.SessionID, time.Now())
	if err != nil {
		writeUserSessionAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.userSessionResponse(user, rotated, csrfToken))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, session, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	if err := s.UserSessions.Revoke(session.SessionID, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke user session")
		return
	}
	s.clearUserSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, session, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes))
	decoder.DisallowUnknownFields()
	var request ChangePasswordRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON password change request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON password change request")
		return
	}
	if request.CurrentPassword == "" || request.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "current password and new password are required", Code: "invalid_password_change"})
		return
	}

	now := time.Now()
	throttleKey := loginThrottleKey(r, user.Username)
	throttle := s.loginThrottle()
	if !throttle.allow(throttleKey, now) {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "too many password change attempts", Code: "password_change_throttled", Retryable: true})
		return
	}
	if err := users.VerifyPassword([]byte(request.CurrentPassword), user.PasswdHash); err != nil {
		throttle.recordFailure(throttleKey, now)
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "current password is incorrect", Code: "invalid_current_password"})
		return
	}
	throttle.recordSuccess(throttleKey)

	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	if _, err := users.ResetUserPassword(paths, user.ID, []byte(request.NewPassword)); err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		s.logf("user password change incomplete user_id=%s username=%q remote=%s stage=refresh_user_registry error=%q", user.ID, user.Username, requestRemoteHost(r), err.Error())
		writeUserMutationError(w, err)
		return
	}
	revokedSessions, err := s.UserSessions.RevokeUserSessionsExcept(user.ID, session.SessionID, now)
	if err != nil {
		s.logf("user password change incomplete user_id=%s username=%q remote=%s stage=revoke_other_sessions revoked_sessions=%d error=%q", user.ID, user.Username, requestRemoteHost(r), revokedSessions, err.Error())
		writeError(w, http.StatusInternalServerError, "password changed but other sessions could not be revoked")
		return
	}
	s.logf("user password changed user_id=%s username=%q remote=%s revoked_sessions=%d", user.ID, user.Username, requestRemoteHost(r), revokedSessions)
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (s *Server) handleUserSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUserBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserSettingsRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON user settings request", Code: "invalid_user_settings"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON user settings request", Code: "invalid_user_settings"})
		return
	}
	if request.Theme != users.ThemeLight && request.Theme != users.ThemeDark {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "theme must be light or dark", Code: "invalid_user_settings"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	if _, err := users.UpdateUserTheme(paths, user.ID, request.Theme); err != nil {
		writeUserMutationError(w, err)
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeUserMutationError(w, err)
		return
	}
	updated, found := s.userRegistry().Find(user.ID)
	if !found {
		writeError(w, http.StatusInternalServerError, "updated user could not be read")
		return
	}
	writeJSON(w, http.StatusOK, safeUserProfileResponse(updated))
}

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
	if err := s.refreshAgentRegistry(paths); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	created, err := readStoredAgent(paths, result.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created agent could not be read")
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
	if err := s.refreshAgentRegistry(paths); err != nil {
		writeAgentMutationError(w, err)
		return
	}
	updated, err := readStoredAgent(paths, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated agent could not be read")
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

func (s *Server) handleUserTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateUserTarget(w, r)
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
	items := s.targetRegistry().All()
	if !user.Admin {
		owned := make([]targets.Target, 0, len(items))
		for _, target := range items {
			if containsUserID(target.UserIDs, user.ID) {
				owned = append(owned, target)
			}
		}
		items = owned
	}
	writeJSON(w, http.StatusOK, userTargetsResponse(items))
}

func (s *Server) handleTestUserTargetCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if request.TargetID != "" {
		var target targets.Target
		found := false
		for _, candidate := range s.targetRegistry().All() {
			if candidate.ID == strings.TrimSpace(request.TargetID) {
				target, found = candidate, true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "target not found", Code: "not_found"})
			return
		}
		if !user.Admin && !containsUserID(target.UserIDs, user.ID) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
			return
		}
	}
	transport := strings.TrimSpace(request.Transport)
	host := strings.TrimSpace(request.Hostname)
	if host == "" {
		host = strings.TrimSpace(request.IP)
	}
	winrmScheme := ""
	winrmPort := 0
	winrmTLSInsecure := false
	if request.WinRM != nil {
		winrmScheme = strings.ToLower(strings.TrimSpace(request.WinRM.Scheme))
		winrmPort = request.WinRM.Port
		winrmTLSInsecure = request.WinRM.TLSInsecure
	}
	s.debugf("debug target credential test requested user_id=%s transport=%s host=%s winrm_scheme=%s winrm_port=%d winrm_tls_insecure=%t",
		user.ID, transport, host, winrmScheme, winrmPort, winrmTLSInsecure)
	var cleanup func()
	var err error
	var targetThrottleKey string
	var run func() remoteexec.Result
	var label string
	switch transport {
	case targets.TransportSSH:
		var sshRequest sshclient.Request
		sshRequest, cleanup, err = s.credentialTestSSHRequest(request)
		targetThrottleKey = credentialTestTargetThrottleKey(user.ID, sshRequest)
		run = func() remoteexec.Result { return s.credentialTestSSHClient().Run(r.Context(), sshRequest) }
		label = "SSH"
	case targets.TransportWinRM:
		var winrmRequest winrmclient.Request
		winrmRequest, err = s.credentialTestWinRMRequest(request)
		targetThrottleKey = credentialTestWinRMTargetThrottleKey(user.ID, winrmRequest)
		run = func() remoteexec.Result { return s.credentialTestWinRMClient().Run(r.Context(), winrmRequest) }
		label = "WinRM"
	default:
		err = validationError("transport must be ssh or winrm")
	}
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.debugf("debug target credential test rejected user_id=%s transport=%s host=%s error=%q",
			user.ID, transport, host, tasks.RedactCredentialValues(err.Error(), request.Username, request.Password))
		status, payload := errorResponse(err)
		if status >= http.StatusInternalServerError {
			payload = ErrorResponse{Error: "private key file could not be prepared", Code: "credential_test_failed"}
			status = http.StatusBadRequest
		}
		if payload.Code == "" {
			payload.Code = "invalid_target"
		}
		writeJSON(w, status, payload)
		return
	}
	now := time.Now()
	throttle := s.targetCredentialTestThrottle()
	userThrottleKey := credentialTestUserThrottleKey(user.ID)
	if !throttle.allowUser(userThrottleKey, now) || !throttle.allowTarget(targetThrottleKey, now) {
		s.debugf("debug target credential test throttled user_id=%s transport=%s host=%s", user.ID, transport, host)
		writeCredentialTestThrottled(w)
		return
	}
	s.debugf("debug target credential test start user_id=%s transport=%s host=%s", user.ID, transport, host)
	result := run()
	s.debugf("debug target credential test finish user_id=%s transport=%s host=%s timed_out=%t exit_status=%s error_present=%t",
		user.ID, transport, host, result.TimedOut, formatCredentialTestExitStatus(result.ExitStatus), result.Error != "")
	throttle.recordUserAttempt(userThrottleKey, now)
	if result.TimedOut {
		throttle.recordTargetFailure(targetThrottleKey, now)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: label + " connection timed out", Code: "credential_test_failed"})
		return
	}
	if result.ErrorCode == sshclient.ErrorCodeHostKeyUnknown || result.ErrorCode == sshclient.ErrorCodeHostKeyChanged {
		throttle.recordTargetFailure(targetThrottleKey, now)
		var hostKey *SSHHostKeyInfo
		if result.HostKey != nil {
			hostKey = &SSHHostKeyInfo{Host: result.HostKey.Host, Port: result.HostKey.Port, Algorithm: result.HostKey.Algorithm, Fingerprint: result.HostKey.Fingerprint}
		}
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: result.Error, Code: result.ErrorCode, HostKey: hostKey})
		return
	}
	if result.Error != "" || (result.ExitStatus != nil && *result.ExitStatus != 0) {
		throttle.recordTargetFailure(targetThrottleKey, now)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: label + " credential test failed", Code: "credential_test_failed"})
		return
	}
	throttle.recordTargetSuccess(targetThrottleKey)
	proof, err := s.targetCredentialProofs().issue(user.ID, strings.TrimSpace(request.TargetID), credentialRequestDigest(request))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "credential test proof could not be created", Code: "credential_test_failed"})
		return
	}
	writeJSON(w, http.StatusOK, UserTargetCredentialTestResponse{Status: "success", Message: label + " connection verified", CredentialTestToken: proof})
}

func formatCredentialTestExitStatus(status *int) string {
	if status == nil {
		return "nil"
	}
	return strconv.Itoa(*status)
}

func writeCredentialTestThrottled(w http.ResponseWriter) {
	writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
		Error:     "too many credential test attempts",
		Code:      "credential_test_throttled",
		Retryable: true,
	})
}

func (s *Server) handleUserTarget(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/targets/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUserTarget(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUserTarget(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateUserTarget(w http.ResponseWriter, r *http.Request) {
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
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if !s.targetCredentialProofs().consume(request.CredentialTestToken, user.ID, "", credentialRequestDigest(request)) {
		writeTargetMutationError(w, credentialTestRequiredError())
		return
	}
	if err := storeUploadedTargetPrivateKey(paths, &request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	target, err := targetFromMutationRequest(paths, request, user.ID, nil)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	result, err := users.CreateTarget(paths, target, nil)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	created, err := readStoredTarget(paths, result.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created target could not be read")
		return
	}
	writeJSON(w, http.StatusCreated, safeUserTargetResponse(created))
}

func (s *Server) handleUpdateUserTarget(w http.ResponseWriter, r *http.Request, id string) {
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
	existing, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
		return
	}
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if targetCredentialChanged(&existing, request) && !s.targetCredentialProofs().consume(request.CredentialTestToken, user.ID, id, credentialRequestDigest(request)) {
		writeTargetMutationError(w, credentialTestRequiredError())
		return
	}
	if err := storeUploadedTargetPrivateKey(paths, &request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	target, err := targetFromMutationRequest(paths, request, user.ID, &existing)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if _, err := users.UpdateTarget(paths, id, target); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	updated, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated target could not be read")
		return
	}
	writeJSON(w, http.StatusOK, safeUserTargetResponse(updated))
}

func (s *Server) handleDeleteUserTarget(w http.ResponseWriter, r *http.Request, id string) {
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
	existing, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
		return
	}
	if _, err := users.DeleteTarget(paths, id); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

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

func (s *Server) handleUserCommandHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	if s.Tasks == nil {
		writeError(w, http.StatusInternalServerError, "task manager is not configured")
		return
	}
	limit, err := commandHistoryLimit(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_limit"})
		return
	}
	writeJSON(w, http.StatusOK, userCommandHistoryResponse(s.Tasks.List(), s.agentRegistry(), s.targetRegistry(), user.ID, user.Admin, limit))
}

func (s *Server) handleUserInteractionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	if s.InteractionHistory == nil {
		writeError(w, http.StatusInternalServerError, "interaction history store is not configured")
		return
	}
	query, err := userInteractionHistoryQuery(r, user.ID, user.Admin, s.agentRegistry())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_interaction_history_query"})
		return
	}
	page, err := s.InteractionHistory.ListInteractions(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_interaction_history_query"})
		return
	}
	writeJSON(w, http.StatusOK, userInteractionHistoryResponse(page))
}

func writeInvalidLogin(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid username or password", Code: "invalid_login"})
}

func (s *Server) userSessionResponse(user users.User, session users.UserSession, csrfToken string) UserSessionResponse {
	return UserSessionResponse{
		User:      safeUserProfileResponse(user),
		ExpiresAt: session.ExpiresAt,
		CSRFToken: csrfToken,
		Build:     s.BuildInfo,
	}
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

func userTargetsResponse(items []targets.Target) UserTargetsResponse {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	response := UserTargetsResponse{Targets: make([]UserTargetResponse, 0, len(items))}
	for _, target := range items {
		response.Targets = append(response.Targets, safeUserTargetResponse(target))
	}
	return response
}

func safeUserTargetResponse(target targets.Target) UserTargetResponse {
	authType := ""
	if target.Auth != nil {
		authType = target.Auth.Type
	}
	response := UserTargetResponse{
		ID:          target.ID,
		Transport:   target.Transport,
		OS:          target.OS,
		Hostname:    target.Hostname,
		IP:          target.IP,
		PolicyID:    target.PolicyID,
		AgentIDs:    append([]string(nil), target.AgentIDs...),
		UserIDs:     append([]string(nil), target.UserIDs...),
		Description: target.Description,
		AuthType:    authType,
	}
	if target.WinRM != nil {
		response.WinRM = &UserTargetWinRM{Scheme: target.WinRM.Scheme, Port: target.WinRM.Port, TLSInsecure: target.WinRM.TLSInsecure}
	}
	return response
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

func (s *Server) handleActivationPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, ActivationPolicyResponse{
		DefaultLifetimeSeconds: int64(s.userActivationTokenTTL().Seconds()),
		MaximumLifetimeSeconds: int64(s.userActivationTokenMaxTTL().Seconds()),
	})
}

func (s *Server) handleRegenerateActivationLink(w http.ResponseWriter, r *http.Request, id string) {
	user, found := s.userRegistry().Find(id)
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if user.Status != users.StatusPendingActivation {
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "user is not pending activation", Code: "invalid_user_status"})
		return
	}
	var request RegenerateActivationRequest
	if !decodeOptionalJSON(w, r, maxUserBodyBytes, &request) {
		return
	}
	ttl, err := s.activationLifetime(request.ActivationTokenLifetimeSeconds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_activation_lifetime"})
		return
	}
	record, token, err := s.CredentialTokens.Create(id, users.CredentialTokenPurposeActivation, ttl, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create user activation")
		return
	}
	writeJSON(w, http.StatusOK, ActivationLinkResponse{ActivationURL: activationURL(token), ActivationExpiresAt: record.ExpiresAt})
}

func (s *Server) handlePasswordResetLink(w http.ResponseWriter, r *http.Request, id string) {
	admin, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	if admin.ID == id {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "administrators cannot reset their own password from the Users list", Code: "self_password_reset"})
		return
	}
	target, found := s.userRegistry().Find(id)
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if target.Status != users.StatusActive && target.Status != users.StatusPasswordResetPending {
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "user is not eligible for password reset", Code: "invalid_user_status"})
		return
	}
	var request RegenerateActivationRequest
	if !decodeOptionalJSON(w, r, maxUserBodyBytes, &request) {
		return
	}
	ttl, err := s.activationLifetime(request.ActivationTokenLifetimeSeconds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_activation_lifetime"})
		return
	}
	paths, configured := s.managementPaths()
	if !configured || s.CredentialTokens == nil || s.UserSessions == nil {
		writeError(w, http.StatusInternalServerError, "password reset services are not configured")
		return
	}
	now := time.Now()
	record, token, err := s.CredentialTokens.BeginPasswordReset(paths, id, ttl, now)
	if err != nil {
		writeUserMutationError(w, err)
		return
	}
	// Fail closed even if reloading another malformed user record prevents a full registry refresh.
	target.Status = users.StatusPasswordResetPending
	target.PasswdHash = ""
	s.replaceUserInRegistry(target)
	revoked, err := s.UserSessions.RevokeUserSessionsExcept(id, "", now)
	if err != nil {
		s.logf("password reset session revocation incomplete admin_id=%s user_id=%s revoked_sessions=%d error=%q", admin.ID, id, revoked, err.Error())
		writeError(w, http.StatusInternalServerError, "password reset started but sessions could not be revoked")
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeUserMutationError(w, err)
		return
	}
	s.logf("password reset started admin_id=%s user_id=%s revoked_sessions=%d", admin.ID, id, revoked)
	writeJSON(w, http.StatusOK, ActivationLinkResponse{ActivationURL: activationURL(token), ActivationExpiresAt: record.ExpiresAt})
}

func (s *Server) handleActivation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token, ok := activationAuthorization(r.Header.Get("Authorization"))
	if !ok || s.CredentialTokens == nil {
		writeActivationError(w, users.ErrActivationInvalid)
		return
	}
	throttle := s.loginThrottle()
	throttleKey := activationThrottleKey(r, token)
	if !throttle.allow(throttleKey, time.Now()) {
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "too many activation attempts", Code: "activation_throttled", Retryable: true})
		return
	}
	record, err := s.CredentialTokens.Inspect(token, time.Now())
	if err != nil {
		throttle.recordFailure(throttleKey, time.Now())
		writeActivationError(w, err)
		return
	}
	user, found := s.userRegistry().Find(record.UserID)
	if !found ||
		(record.Purpose == users.CredentialTokenPurposeActivation && user.Status != users.StatusPendingActivation) ||
		(record.Purpose == users.CredentialTokenPurposePasswordReset && user.Status != users.StatusPasswordResetPending) {
		_ = s.CredentialTokens.Delete(record.UserID)
		throttle.recordFailure(throttleKey, time.Now())
		writeActivationError(w, users.ErrActivationInvalid)
		return
	}
	throttle.recordSuccess(throttleKey)
	purpose := record.Purpose
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, ActivationContextResponse{Username: user.Username, Name: user.Name, Purpose: purpose, ExpiresAt: record.ExpiresAt})
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBodyBytes))
	decoder.DisallowUnknownFields()
	var request CompleteActivationRequest
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.NewPassword) == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "new password is required", Code: "invalid_password"})
		return
	}
	paths, configured := s.managementPaths()
	if !configured {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	if _, err := s.CredentialTokens.Complete(paths, token, []byte(request.NewPassword), time.Now()); err != nil {
		writeActivationError(w, err)
		return
	}
	if err := s.refreshUserRegistry(paths); err != nil {
		writeError(w, http.StatusInternalServerError, "activated user could not be loaded")
		return
	}
	status := "activated"
	if purpose == "password_reset" {
		status = "password_reset"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func activationAuthorization(value string) (string, bool) {
	const prefix = "Activation "
	if !strings.HasPrefix(value, prefix) || strings.TrimSpace(strings.TrimPrefix(value, prefix)) == "" {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
}

func activationThrottleKey(r *http.Request, token string) string {
	userID, _, ok := users.ParseSessionCookieValue(token)
	if !ok {
		userID = "malformed"
	}
	return requestRemoteHost(r) + "|activation|" + userID
}

func activationURL(token string) string {
	return "/#/activate?token=" + url.QueryEscape(token)
}

func (s *Server) activationLifetime(seconds *int64) (time.Duration, error) {
	ttl := s.userActivationTokenTTL()
	if seconds != nil {
		if *seconds <= 0 {
			return 0, fmt.Errorf("activation token lifetime must be positive")
		}
		ttl = time.Duration(*seconds) * time.Second
	}
	if ttl > s.userActivationTokenMaxTTL() {
		return 0, fmt.Errorf("activation token lifetime exceeds configured maximum")
	}
	return ttl, nil
}

func (s *Server) userActivationTokenTTL() time.Duration {
	if s.UserActivationTokenTTL > 0 {
		return s.UserActivationTokenTTL
	}
	return 12 * time.Hour
}

func (s *Server) userActivationTokenMaxTTL() time.Duration {
	if s.UserActivationTokenMaxTTL > 0 {
		return s.UserActivationTokenMaxTTL
	}
	return 48 * time.Hour
}

func writeActivationError(w http.ResponseWriter, err error) {
	if errors.Is(err, users.ErrActivationExpired) {
		writeJSON(w, http.StatusGone, ErrorResponse{Error: "activation link has expired", Code: "activation_expired"})
		return
	}
	writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "activation link is invalid", Code: "invalid_activation"})
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if err == io.EOF {
			return true
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
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

func (s *Server) credentialTestSSHRequest(request UserTargetMutationRequest) (sshclient.Request, func(), error) {
	ip := strings.TrimSpace(request.IP)
	username := strings.TrimSpace(request.Username)
	authType := strings.TrimSpace(request.AuthType)
	if ip == "" {
		return sshclient.Request{}, nil, validationError("ip is required")
	}
	if username == "" {
		return sshclient.Request{}, nil, validationError("username is required")
	}
	if authType == "" {
		authType = targets.AuthTypePassword
	}
	sshRequest := sshclient.Request{
		Host:                       ip,
		Username:                   username,
		AuthType:                   authType,
		RemoteCommand:              "hostname",
		SensitiveValues:            []string{username},
		KnownHostsPath:             s.KnownHostsPath,
		ApprovedHostKeyFingerprint: strings.TrimSpace(request.ApprovedHostKeyFingerprint),
		Timeout:                    credentialTestTimeout(s.CommandTimeout),
		OutputLimit:                1024,
	}
	switch authType {
	case targets.AuthTypePassword:
		if strings.TrimSpace(request.Password) == "" {
			return sshclient.Request{}, nil, validationError("password is required")
		}
		sshRequest.AuthType = sshclient.AuthTypePassword
		sshRequest.Password = request.Password
		sshRequest.SensitiveValues = append(sshRequest.SensitiveValues, request.Password)
		return sshRequest, nil, nil
	case targets.AuthTypePrivateKey:
		if len(request.PrivateKeyUpload) == 0 {
			return sshclient.Request{}, nil, validationError("private key file is required")
		}
		privateKeyPath, cleanup, err := writeTemporaryPrivateKey(request.PrivateKeyUpload)
		if err != nil {
			return sshclient.Request{}, cleanup, err
		}
		sshRequest.AuthType = sshclient.AuthTypePrivateKey
		sshRequest.PrivateKeyPath = privateKeyPath
		return sshRequest, cleanup, nil
	default:
		return sshclient.Request{}, nil, validationError("auth_type must be password or private_key")
	}
}

func (s *Server) credentialTestSSHClient() sshclient.Client {
	if s.SSHClient != nil {
		return s.SSHClient
	}
	return sshclient.Runner{
		Timeout:               credentialTestTimeout(s.CommandTimeout),
		OutputLimit:           1024,
		DefaultKnownHostsPath: s.KnownHostsPath,
		Debug:                 s.Debug,
		Logger:                s.Logger,
	}
}

func (s *Server) credentialTestWinRMRequest(request UserTargetMutationRequest) (winrmclient.Request, error) {
	host := strings.TrimSpace(request.Hostname)
	if host == "" {
		host = strings.TrimSpace(request.IP)
	}
	if host == "" {
		return winrmclient.Request{}, validationError("hostname or ip is required")
	}
	username := strings.TrimSpace(request.Username)
	if username == "" {
		return winrmclient.Request{}, validationError("username is required")
	}
	if request.AuthType != "" && request.AuthType != targets.AuthTypePassword {
		return winrmclient.Request{}, validationError("WinRM targets require password authentication")
	}
	if strings.TrimSpace(request.Password) == "" {
		return winrmclient.Request{}, validationError("password is required")
	}
	if request.WinRM == nil {
		return winrmclient.Request{}, validationError("winrm is required")
	}
	scheme := strings.ToLower(strings.TrimSpace(request.WinRM.Scheme))
	if scheme != winrmclient.SchemeHTTP && scheme != winrmclient.SchemeHTTPS {
		return winrmclient.Request{}, validationError("winrm.scheme must be http or https")
	}
	port := request.WinRM.Port
	if port == 0 {
		if scheme == winrmclient.SchemeHTTPS {
			port = 5986
		} else {
			port = 5985
		}
	}
	if port < 1 || port > 65535 {
		return winrmclient.Request{}, validationError("winrm.port must be between 1 and 65535")
	}
	if request.WinRM.TLSInsecure && scheme != winrmclient.SchemeHTTPS {
		return winrmclient.Request{}, validationError("winrm.tls_insecure is valid only for https")
	}
	return winrmclient.Request{
		Host: host, Port: port, Scheme: scheme, TLSInsecure: request.WinRM.TLSInsecure,
		Username: username, Password: request.Password, PowerShell: "Write-Output PROXYMA_WINRM_OK",
		SensitiveValues: []string{username, request.Password}, Timeout: credentialTestWinRMTimeout(s.CommandTimeout), OutputLimit: 1024,
	}, nil
}

func (s *Server) credentialTestWinRMClient() winrmclient.Client {
	if s.WinRMClient != nil {
		return s.WinRMClient
	}
	return winrmclient.Runner{Timeout: credentialTestWinRMTimeout(s.CommandTimeout), OutputLimit: 1024, Debug: s.Debug, Logger: s.Logger}
}

func credentialTestTimeout(commandTimeout time.Duration) time.Duration {
	const defaultTimeout = 10 * time.Second
	if commandTimeout <= 0 || commandTimeout > defaultTimeout {
		return defaultTimeout
	}
	return commandTimeout
}

func credentialTestWinRMTimeout(commandTimeout time.Duration) time.Duration {
	const defaultTimeout = 30 * time.Second
	if commandTimeout <= 0 || commandTimeout > defaultTimeout {
		return defaultTimeout
	}
	return commandTimeout
}

func writeTemporaryPrivateKey(data []byte) (string, func(), error) {
	file, err := os.CreateTemp("", "Proxyma-private-key-test-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary private key: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("secure temporary private key: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary private key: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary private key: %w", err)
	}
	return file.Name(), cleanup, nil
}

func decodeTargetMutationRequest(w http.ResponseWriter, r *http.Request) (UserTargetMutationRequest, bool) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		return decodeMultipartTargetMutationRequest(w, r)
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTargetBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserTargetMutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	return request, true
}

func decodeMultipartTargetMutationRequest(w http.ResponseWriter, r *http.Request) (UserTargetMutationRequest, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxTargetBodyBytes)
	if err := r.ParseMultipartForm(maxTargetBodyBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid multipart target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	form := r.MultipartForm
	request := UserTargetMutationRequest{
		Transport:                  firstFormValue(form.Value, "transport"),
		OS:                         firstFormValue(form.Value, "os"),
		Hostname:                   firstFormValue(form.Value, "hostname"),
		IP:                         firstFormValue(form.Value, "ip"),
		Description:                firstFormValue(form.Value, "description"),
		Username:                   firstFormValue(form.Value, "username"),
		PolicyID:                   firstFormValue(form.Value, "policy_id"),
		AgentIDs:                   formValues(form.Value, "agent_ids"),
		AuthType:                   firstFormValue(form.Value, "auth_type"),
		Password:                   firstFormValue(form.Value, "password"),
		TargetID:                   firstFormValue(form.Value, "target_id"),
		ApprovedHostKeyFingerprint: firstFormValue(form.Value, "approved_host_key_fingerprint"),
		CredentialTestToken:        firstFormValue(form.Value, "credential_test_token"),
	}
	files := form.File["private_key_file"]
	if len(files) > 1 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "only one private key file may be uploaded", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	if len(files) == 1 {
		file, err := files[0].Open()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file could not be read", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file could not be read", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		if len(data) == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file must not be empty", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		request.PrivateKeyUpload = data
	}
	return request, true
}

func firstFormValue(values map[string][]string, key string) string {
	items := values[key]
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func formValues(values map[string][]string, key string) []string {
	items, ok := values[key]
	if !ok {
		return nil
	}
	return append([]string(nil), items...)
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

func targetFromMutationRequest(paths users.Paths, request UserTargetMutationRequest, userID string, existing *targets.Target) (targets.Target, error) {
	target := targets.Target{
		Transport:   strings.TrimSpace(request.Transport),
		OS:          strings.TrimSpace(request.OS),
		Hostname:    strings.TrimSpace(request.Hostname),
		IP:          strings.TrimSpace(request.IP),
		Username:    strings.TrimSpace(request.Username),
		PolicyID:    strings.TrimSpace(request.PolicyID),
		AgentIDs:    cleanStringList(request.AgentIDs),
		Description: strings.TrimSpace(request.Description),
	}
	if existing == nil {
		target.UserIDs = []string{userID}
	} else {
		target.UserIDs = append([]string(nil), existing.UserIDs...)
		if target.Username == "" {
			target.Username = existing.Username
		}
		if target.Transport == "" {
			target.Transport = existing.Transport
		}
		if target.OS == "" {
			target.OS = existing.OS
		}
		if request.PolicyID == "" {
			target.PolicyID = existing.PolicyID
		}
		if request.AgentIDs == nil {
			target.AgentIDs = append([]string(nil), existing.AgentIDs...)
		}
	}
	if request.WinRM != nil {
		target.WinRM = &targets.WinRM{Scheme: request.WinRM.Scheme, Port: request.WinRM.Port, TLSInsecure: request.WinRM.TLSInsecure}
	} else if existing != nil && target.Transport == targets.TransportWinRM && existing.WinRM != nil {
		winrm := *existing.WinRM
		target.WinRM = &winrm
	}
	target.Auth = targetAuthFromMutationRequest(request, existing)
	if err := validateRuntimeTargetCredential(paths, target); err != nil {
		return targets.Target{}, err
	}
	return target, nil
}

func targetAuthFromMutationRequest(request UserTargetMutationRequest, existing *targets.Target) *targets.TargetAuth {
	authType := strings.TrimSpace(request.AuthType)
	if authType == "" && existing != nil {
		return cloneTargetAuth(existing.Auth)
	}
	switch authType {
	case targets.AuthTypePassword:
		if strings.TrimSpace(request.Password) != "" {
			return targets.PasswordAuth(request.Password)
		}
		if existing != nil && existing.Auth != nil && existing.Auth.Type == targets.AuthTypePassword {
			return cloneTargetAuth(existing.Auth)
		}
		return targets.PasswordAuth("")
	case targets.AuthTypePrivateKey:
		privateKeyPath := strings.TrimSpace(request.PrivateKeyPath)
		if privateKeyPath != "" {
			return targets.PrivateKeyAuth(privateKeyPath)
		}
		if existing != nil && existing.Auth != nil && existing.Auth.Type == targets.AuthTypePrivateKey {
			return cloneTargetAuth(existing.Auth)
		}
		return targets.PrivateKeyAuth("")
	default:
		return nil
	}
}

func cloneTargetAuth(auth *targets.TargetAuth) *targets.TargetAuth {
	if auth == nil {
		return nil
	}
	cloned := *auth
	return &cloned
}

func storeUploadedTargetPrivateKey(paths users.Paths, request *UserTargetMutationRequest) error {
	if request == nil || len(request.PrivateKeyUpload) == 0 {
		return nil
	}
	if strings.TrimSpace(request.AuthType) != targets.AuthTypePrivateKey {
		return validationError("private key file can only be uploaded for private key targets")
	}
	keyID, err := ids.NewUUIDV4()
	if err != nil {
		return fmt.Errorf("generate private key id: %w", err)
	}
	relativePath := filepath.ToSlash(filepath.Join(targets.PrivateKeyDirectory, keyID))
	privateKeyPath, err := targetPrivateKeyAbsolutePath(paths, relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0o700); err != nil {
		return fmt.Errorf("create private key directory: %w", err)
	}
	if err := os.WriteFile(privateKeyPath, request.PrivateKeyUpload, 0o600); err != nil {
		return fmt.Errorf("write private key file: %w", err)
	}
	request.PrivateKeyPath = relativePath
	request.PrivateKeyUpload = nil
	return nil
}

func validateRuntimeTargetCredential(paths users.Paths, target targets.Target) error {
	if target.Auth == nil || target.Auth.Type != targets.AuthTypePrivateKey || strings.TrimSpace(target.Auth.PrivateKeyPath) == "" {
		return nil
	}
	privateKeyPath, err := targetPrivateKeyAbsolutePath(paths, target.Auth.PrivateKeyPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(privateKeyPath); err != nil {
		return validationError("auth.private_key_path file not found")
	}
	return nil
}

func targetPrivateKeyAbsolutePath(paths users.Paths, relativePath string) (string, error) {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return "", validationError("auth.private_key_path is required")
	}
	cleanRelativePath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	if strings.HasPrefix(cleanRelativePath, "../") || cleanRelativePath == ".." || filepath.IsAbs(relativePath) {
		return "", validationError("auth.private_key_path must be under secrets/private_keys")
	}
	privateKeyDirectory := filepath.ToSlash(filepath.Clean(filepath.FromSlash(targets.PrivateKeyDirectory)))
	if !strings.HasPrefix(cleanRelativePath, privateKeyDirectory+"/") {
		return "", validationError("auth.private_key_path must be under secrets/private_keys")
	}
	root := targets.ProjectRootForDir(paths.TargetDir)
	return filepath.Join(root, filepath.FromSlash(cleanRelativePath)), nil
}

func cleanStringList(values []string) []string {
	if values == nil {
		return nil
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func readStoredUser(paths users.Paths, id string) (users.User, error) {
	var user users.User
	if err := store.New(paths.UserDir).ReadJSON(strings.TrimSpace(id), &user); err != nil {
		return users.User{}, err
	}
	return user, nil
}

func readStoredAgent(paths users.Paths, id string) (agents.Agent, error) {
	var agent agents.Agent
	if err := store.New(paths.AgentDir).ReadJSON(strings.TrimSpace(id), &agent); err != nil {
		return agents.Agent{}, err
	}
	return agent, nil
}

func readStoredTarget(paths users.Paths, id string) (targets.Target, error) {
	var target targets.Target
	if err := store.New(paths.TargetDir).ReadJSON(strings.TrimSpace(id), &target); err != nil {
		return targets.Target{}, err
	}
	return target, nil
}

func (s *Server) managementPaths() (users.Paths, bool) {
	paths := s.ManagementPaths
	if strings.TrimSpace(paths.UserDir) == "" || strings.TrimSpace(paths.AgentDir) == "" || strings.TrimSpace(paths.TargetDir) == "" || strings.TrimSpace(paths.PolicyDir) == "" {
		return users.Paths{}, false
	}
	return paths, true
}

func (s *Server) refreshUserRegistry(paths users.Paths) error {
	registry, diagnostics, err := users.LoadDir(paths.UserDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("users invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setUserRegistry(registry)
	return nil
}

func (s *Server) userRegistry() users.Registry {
	s.usersMu.RLock()
	defer s.usersMu.RUnlock()
	return s.Users
}

func (s *Server) setUserRegistry(registry users.Registry) {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()
	s.Users = registry
}

func (s *Server) replaceUserInRegistry(updated users.User) {
	items := s.userRegistry().All()
	for index := range items {
		if items[index].ID == updated.ID {
			items[index] = updated
			break
		}
	}
	s.setUserRegistry(users.NewRegistry(items))
}

func (s *Server) refreshAgentRegistry(paths users.Paths) error {
	registry, diagnostics, err := agents.LoadDir(paths.AgentDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("agents invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setAgentRegistry(registry)
	return nil
}

func (s *Server) agentRegistry() agents.Registry {
	s.agentsMu.RLock()
	defer s.agentsMu.RUnlock()
	return s.Agents
}

func (s *Server) setAgentRegistry(registry agents.Registry) {
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	s.Agents = registry
}

func (s *Server) refreshTargetRegistry(paths users.Paths) error {
	registry, diagnostics, err := targets.LoadDir(paths.TargetDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("targets invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	s.setTargetRegistry(registry)
	return nil
}

func (s *Server) targetRegistry() targets.Registry {
	s.targetsMu.RLock()
	defer s.targetsMu.RUnlock()
	return s.Targets
}

func (s *Server) setTargetRegistry(registry targets.Registry) {
	s.targetsMu.Lock()
	defer s.targetsMu.Unlock()
	s.Targets = registry
}

func (s *Server) refreshPolicyEvaluator(paths users.Paths) error {
	set, diagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("policies invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	set.SetReviewer(s.LLMProfiles)
	if s.Policies != nil {
		s.Policies.Set(set)
	}
	return nil
}

func (s *Server) refreshLLMProfilesAndPolicies(paths users.Paths) error {
	profiles, diagnostics, err := llm.LoadDir(paths.LLMProfileDir)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return validationError("LLM profiles invalid: " + diagnostics[0].Path + ": " + diagnostics[0].Message)
	}
	catalog, err := llm.NewCatalog(profiles)
	if err != nil {
		return validationError(err.Error())
	}
	catalog.ConfigureDebug(s.Debug, s.Logger)
	set, policyDiagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		return err
	}
	if len(policyDiagnostics) > 0 {
		return validationError("policies invalid: " + policyDiagnostics[0].Path + ": " + policyDiagnostics[0].Message)
	}
	set.SetReviewer(s.LLMProfiles)
	if s.LLMProfiles != nil {
		s.LLMProfiles.Set(catalog)
	}
	if s.Policies != nil {
		s.Policies.Set(set)
	}
	return nil
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

func writeUserMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		payload.Code = "invalid_user"
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_user"})
}

func writeAgentMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_agent"})
}

func writeTargetMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_target"})
}

func writeInteractionMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		payload.Code = "invalid_interaction"
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_interaction"})
}

func writePolicyMutationError(w http.ResponseWriter, err error) {
	if status, payload := errorResponse(err); status != http.StatusInternalServerError {
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error(), Code: "invalid_policy"})
}

func userCommandHistoryResponse(items []*tasks.Task, agentRegistry agents.Registry, targetRegistry targets.Registry, userID string, admin bool, limit int) UserCommandHistoryResponse {
	agentNames := map[string]string{}
	ownedAgentIDs := map[string]struct{}{}
	for _, agent := range agentRegistry.All() {
		if strings.TrimSpace(agent.DisplayName) != "" {
			agentNames[agent.ID] = agent.DisplayName
		}
		if !admin && containsUserID(agent.UserIDs, userID) {
			ownedAgentIDs[agent.ID] = struct{}{}
		}
	}

	var ownedTargets []targets.Target
	if !admin {
		ownedTargets = ownedTargetsForUser(targetRegistry, userID)
	}
	filtered := make([]*tasks.Task, 0, len(items))
	for _, task := range items {
		if admin || userCanReadTask(task, ownedAgentIDs, ownedTargets) {
			filtered = append(filtered, task)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left := taskSortTime(filtered[i])
		right := taskSortTime(filtered[j])
		if left.Equal(right) {
			return filtered[i].TaskID < filtered[j].TaskID
		}
		return left.After(right)
	})
	if limit > len(filtered) {
		limit = len(filtered)
	}

	response := UserCommandHistoryResponse{Commands: make([]UserCommandHistoryItemResponse, 0, limit)}
	for _, task := range filtered[:limit] {
		response.Commands = append(response.Commands, UserCommandHistoryItemResponse{
			TaskID:      task.TaskID,
			AgentID:     task.AgentID,
			AgentName:   agentNames[task.AgentID],
			Target:      formatTarget(task.Target),
			Command:     task.Command,
			Status:      task.Status,
			Result:      commandHistoryResult(task),
			CreatedAt:   task.CreatedAt,
			CompletedAt: task.CompletedAt,
		})
	}
	return response
}

func userInteractionHistoryQuery(r *http.Request, userID string, admin bool, agentRegistry agents.Registry) (history.InteractionQuery, error) {
	values := r.URL.Query()
	limit, err := interactionHistoryLimit(values)
	if err != nil {
		return history.InteractionQuery{}, err
	}
	query := history.InteractionQuery{
		CurrentUserID:         userID,
		CurrentUserIsAdmin:    admin,
		AgentID:               strings.TrimSpace(values.Get("agent_id")),
		InteractionInstanceID: strings.TrimSpace(values.Get("interaction_instance_id")),
		Status:                commaValues(values.Get("status")),
		Kind:                  commaValues(values.Get("kind")),
		Target:                strings.TrimSpace(values.Get("target")),
		Text:                  strings.TrimSpace(values.Get("q")),
		CommandPattern:        strings.TrimSpace(values.Get("command_pattern")),
		CommandPatternMode:    strings.TrimSpace(values.Get("command_pattern_mode")),
		Limit:                 limit,
		Cursor:                strings.TrimSpace(values.Get("cursor")),
	}
	if query.CommandPatternMode == "" {
		query.CommandPatternMode = history.CommandPatternLiteral
	}
	switch query.CommandPatternMode {
	case history.CommandPatternLiteral, history.CommandPatternGlob, history.CommandPatternRegex:
	default:
		return history.InteractionQuery{}, fmt.Errorf("command_pattern_mode must be literal, glob, or regex")
	}
	if admin {
		query.InteractionInstanceCreatorUserID = strings.TrimSpace(values.Get("interaction_instance_creator_user_id"))
	} else if strings.TrimSpace(values.Get("interaction_instance_creator_user_id")) != "" {
		return history.InteractionQuery{}, fmt.Errorf("interaction_instance_creator_user_id is admin-only")
	}
	if query.AgentID != "" && !admin {
		agent, ok := agentRegistry.Find(query.AgentID)
		if !ok || !containsUserID(agent.UserIDs, userID) {
			return history.InteractionQuery{}, fmt.Errorf("agent_id is not visible to this user")
		}
	}
	if raw := strings.TrimSpace(values.Get("created_from")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return history.InteractionQuery{}, fmt.Errorf("created_from must be RFC3339")
		}
		query.CreatedFrom = &parsed
	}
	if raw := strings.TrimSpace(values.Get("created_to")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return history.InteractionQuery{}, fmt.Errorf("created_to must be RFC3339")
		}
		query.CreatedTo = &parsed
	}
	if query.CreatedFrom != nil && query.CreatedTo != nil && query.CreatedFrom.After(*query.CreatedTo) {
		return history.InteractionQuery{}, fmt.Errorf("created_from must be before created_to")
	}
	if query.CommandPatternMode == history.CommandPatternRegex && query.CommandPattern != "" && query.AgentID == "" && (query.CreatedFrom == nil || query.CreatedTo == nil) {
		return history.InteractionQuery{}, fmt.Errorf("regex command search requires agent_id or a created_from/created_to interval")
	}
	return query, nil
}

func interactionHistoryLimit(values url.Values) (int, error) {
	raw := strings.TrimSpace(values.Get("limit"))
	if raw == "" {
		return history.DefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, strconv.ErrSyntax
	}
	return history.NormalizeLimit(limit), nil
}

func userInteractionHistoryResponse(page history.InteractionPage) UserInteractionHistoryResponse {
	response := UserInteractionHistoryResponse{
		Items:      make([]UserInteractionHistoryItemResponse, 0, len(page.Items)),
		NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		response.Items = append(response.Items, UserInteractionHistoryItemResponse{
			InteractionID:                    item.InteractionID,
			TaskID:                           item.TaskID,
			AgentID:                          item.AgentID,
			AgentName:                        item.AgentName,
			InteractionInstanceID:            item.InteractionInstanceID,
			InteractionInstanceCreatorUserID: item.InteractionInstanceCreatorUserID,
			Provider:                         item.Provider,
			Kind:                             item.Kind,
			ExpectedResponse:                 item.ExpectedResponse,
			Status:                           item.Status,
			Subject:                          item.Subject,
			Message:                          item.Message,
			RequestedBy:                      item.RequestedBy,
			Approved:                         item.Approved,
			Answer:                           item.Answer,
			RespondedBy:                      item.RespondedBy,
			CancelReason:                     item.CancelReason,
			Target:                           item.Target,
			TargetHostname:                   item.TargetHostname,
			TargetIP:                         item.TargetIP,
			Command:                          item.Command,
			CreatedAt:                        item.CreatedAt,
			ExpiresAt:                        item.ExpiresAt,
			RespondedAt:                      item.RespondedAt,
			TaskCreatedAt:                    item.TaskCreatedAt,
		})
	}
	return response
}

func commaValues(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func commandHistoryLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultCommandHistoryLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, strconv.ErrSyntax
	}
	if limit > maxCommandHistoryLimit {
		return maxCommandHistoryLimit, nil
	}
	return limit, nil
}

func ownedTargetsForUser(registry targets.Registry, userID string) []targets.Target {
	var owned []targets.Target
	for _, target := range registry.All() {
		if containsUserID(target.UserIDs, userID) {
			owned = append(owned, target)
		}
	}
	return owned
}

func userCanReadTask(task *tasks.Task, ownedAgentIDs map[string]struct{}, ownedTargets []targets.Target) bool {
	if task == nil {
		return false
	}
	if _, ok := ownedAgentIDs[task.AgentID]; ok {
		return true
	}
	if task.Target == nil {
		return false
	}
	for _, target := range ownedTargets {
		if sameTaskTarget(task.Target, target) {
			return true
		}
	}
	return false
}

func sameTaskTarget(taskTarget *tasks.Target, target targets.Target) bool {
	if taskTarget == nil {
		return false
	}
	if strings.TrimSpace(taskTarget.Hostname) != "" && strings.EqualFold(strings.TrimSpace(taskTarget.Hostname), strings.TrimSpace(target.Hostname)) {
		return true
	}
	if strings.TrimSpace(taskTarget.IP) != "" && strings.EqualFold(strings.TrimSpace(taskTarget.IP), strings.TrimSpace(target.IP)) {
		return true
	}
	return false
}

func taskSortTime(task *tasks.Task) time.Time {
	if task == nil {
		return time.Time{}
	}
	if task.CompletedAt != nil {
		return *task.CompletedAt
	}
	return task.CreatedAt
}

func commandHistoryResult(task *tasks.Task) string {
	if task == nil {
		return ""
	}
	switch task.Status {
	case tasks.StatusCompleted:
		if task.Result != nil && task.Result.ExitStatus != nil && *task.Result.ExitStatus != 0 {
			return "warning"
		}
		return "success"
	case tasks.StatusFailed, tasks.StatusTimedOut, tasks.StatusInterrupted, tasks.StatusAbandoned, tasks.StatusBlocked:
		return "failed"
	case tasks.StatusWaitingUserResponse:
		return "waiting"
	default:
		return task.Status
	}
}

func containsUserID(values []string, userID string) bool {
	userID = strings.TrimSpace(userID)
	for _, value := range values {
		if strings.TrimSpace(value) == userID {
			return true
		}
	}
	return false
}
