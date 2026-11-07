package api

import (
	"encoding/json"
	"io"
	"net/http"
	"proxyma/internal/users"
	"strings"
	"time"
)

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
