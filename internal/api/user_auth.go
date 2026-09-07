package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"
)

const (
	userSessionCookieName     = "__Host-proxyma_session"
	devUserSessionCookieName  = "proxyma_session_dev"
	userSessionCSRFHeader     = "X-CSRF-Token"
	defaultUserSessionTTL     = 12 * time.Hour
	maxLoginFailures          = 5
	loginThrottleWindow       = 15 * time.Minute
	loginThrottleLockout      = 15 * time.Minute
	maxCredentialTestFailures = 5
	maxCredentialTestAttempts = 30
	credentialTestWindow      = 15 * time.Minute
	credentialTestLockout     = 15 * time.Minute
	dummyPasswordHash         = "$argon2id$v=19$m=19456,t=2,p=1$WGeElL1uhb1hV8gVYptHmg$rvzcxwJQV6VK9gqH/GnHKWGB3t+40DX8XUYLkr8iiGE"
)

type userSessionContextKey struct{}

type authenticatedUserSession struct {
	User    users.User
	Session users.UserSession
}

func contextWithUserSession(ctx context.Context, user users.User, session users.UserSession) context.Context {
	return context.WithValue(ctx, userSessionContextKey{}, authenticatedUserSession{User: user, Session: session})
}

func userSessionFromContext(ctx context.Context) (users.User, users.UserSession, bool) {
	authenticated, ok := ctx.Value(userSessionContextKey{}).(authenticatedUserSession)
	return authenticated.User, authenticated.Session, ok && authenticated.User.ID != "" && authenticated.Session.SessionID != ""
}

func (s *Server) authenticateUserSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.UserSessions == nil {
			writeError(w, http.StatusInternalServerError, "user session store is not configured")
			return
		}
		cookie, err := r.Cookie(s.userSessionCookieName())
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
			return
		}
		session, err := s.UserSessions.Authenticate(cookie.Value, time.Now())
		if err != nil {
			writeUserSessionAuthError(w, err)
			return
		}
		user, ok := s.userRegistry().Find(session.UserID)
		if !ok || user.Status != users.StatusActive {
			_ = s.UserSessions.Revoke(session.SessionID, time.Now())
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid user session", Code: "invalid_session"})
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithUserSession(r.Context(), user, session)))
	})
}

func (s *Server) authenticateAdminSession(next http.Handler) http.Handler {
	return s.authenticateUserSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := userSessionFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
			return
		}
		if !user.Admin {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "administrator access is required", Code: "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (s *Server) verifyUserSessionCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		_, session, ok := userSessionFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
			return
		}
		if !users.VerifyCSRF(session, r.Header.Get(userSessionCSRFHeader)) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "invalid CSRF token", Code: "invalid_csrf"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUserSessionAuthError(w http.ResponseWriter, err error) {
	code := "invalid_session"
	message := "invalid user session"
	if errors.Is(err, users.ErrSessionExpired) {
		code = "session_expired"
		message = "user session expired"
	}
	writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: message, Code: code})
}

func (s *Server) userSessionCookieName() string {
	if s.UseDevelopmentCookies {
		return devUserSessionCookieName
	}
	return userSessionCookieName
}

func (s *Server) setUserSessionCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.userSessionCookieName(),
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   !s.UseDevelopmentCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearUserSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.userSessionCookieName(),
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !s.UseDevelopmentCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) userSessionTTL() time.Duration {
	if s.UserSessionTTL > 0 {
		return s.UserSessionTTL
	}
	return defaultUserSessionTTL
}

func (s *Server) loginThrottle() *loginThrottle {
	if s == nil {
		return nil
	}
	if s.throttle == nil {
		s.throttle = newLoginThrottle()
	}
	return s.throttle
}

type loginThrottle struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	Count       int
	WindowStart time.Time
	LockedUntil time.Time
}

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{attempts: map[string]loginAttempt{}}
}

func (t *loginThrottle) allow(key string, now time.Time) bool {
	if t == nil || key == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt := t.attempts[key]
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false
	}
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > loginThrottleWindow {
		delete(t.attempts, key)
	}
	return true
}

func (t *loginThrottle) recordFailure(key string, now time.Time) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt := t.attempts[key]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > loginThrottleWindow {
		attempt = loginAttempt{WindowStart: now}
	}
	attempt.Count++
	if attempt.Count >= maxLoginFailures {
		attempt.LockedUntil = now.Add(loginThrottleLockout)
	}
	t.attempts[key] = attempt
}

func (t *loginThrottle) recordSuccess(key string) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key)
}

func loginThrottleKey(r *http.Request, username string) string {
	return requestRemoteHost(r) + "|" + users.NormalizeUsername(username)
}

func requestRemoteHost(r *http.Request) string {
	host := r.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsedHost
	}
	return host
}

func (s *Server) targetCredentialTestThrottle() *credentialTestThrottle {
	if s == nil {
		return nil
	}
	if s.credentialTestThrottle == nil {
		s.credentialTestThrottle = newCredentialTestThrottle()
	}
	return s.credentialTestThrottle
}

type credentialTestThrottle struct {
	mu             sync.Mutex
	userAttempts   map[string]loginAttempt
	targetFailures map[string]loginAttempt
}

func newCredentialTestThrottle() *credentialTestThrottle {
	return &credentialTestThrottle{
		userAttempts:   map[string]loginAttempt{},
		targetFailures: map[string]loginAttempt{},
	}
}

func (t *credentialTestThrottle) allowUser(key string, now time.Time) bool {
	if t == nil {
		return true
	}
	return t.allow(t.userAttempts, key, now)
}

func (t *credentialTestThrottle) allowTarget(key string, now time.Time) bool {
	if t == nil {
		return true
	}
	return t.allow(t.targetFailures, key, now)
}

func (t *credentialTestThrottle) allow(attempts map[string]loginAttempt, key string, now time.Time) bool {
	if t == nil || key == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt := attempts[key]
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false
	}
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > credentialTestWindow {
		delete(attempts, key)
	}
	return true
}

func (t *credentialTestThrottle) recordUserAttempt(key string, now time.Time) {
	if t == nil {
		return
	}
	t.record(t.userAttempts, key, now, maxCredentialTestAttempts)
}

func (t *credentialTestThrottle) recordTargetFailure(key string, now time.Time) {
	if t == nil {
		return
	}
	t.record(t.targetFailures, key, now, maxCredentialTestFailures)
}

func (t *credentialTestThrottle) record(attempts map[string]loginAttempt, key string, now time.Time, maxAttempts int) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt := attempts[key]
	if attempt.WindowStart.IsZero() || now.Sub(attempt.WindowStart) > credentialTestWindow {
		attempt = loginAttempt{WindowStart: now}
	}
	attempt.Count++
	if attempt.Count >= maxAttempts {
		attempt.LockedUntil = now.Add(credentialTestLockout)
	}
	attempts[key] = attempt
}

func (t *credentialTestThrottle) recordTargetSuccess(key string) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.targetFailures, key)
}

func credentialTestUserThrottleKey(userID string) string {
	return strings.TrimSpace(userID)
}

func credentialTestTargetThrottleKey(userID string, request sshclient.Request) string {
	return strings.Join([]string{
		strings.TrimSpace(userID),
		strings.TrimSpace(request.Host),
		users.NormalizeUsername(request.Username),
		strings.TrimSpace(request.AuthType),
	}, "|")
}

func credentialTestWinRMTargetThrottleKey(userID string, request winrmclient.Request) string {
	return strings.Join([]string{
		strings.TrimSpace(userID), targets.TransportWinRM, strings.TrimSpace(request.Host),
		users.NormalizeUsername(request.Username), request.Scheme, strconv.Itoa(request.Port),
	}, "|")
}
