package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"proxyma/internal/users"
	"strings"
	"time"
)

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
