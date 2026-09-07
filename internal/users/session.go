package users

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"proxyma/internal/store"
)

var (
	ErrSessionInvalid = errors.New("invalid user session")
	ErrSessionExpired = errors.New("user session expired")
)

// UserSession is a persisted GUI/API user cookie session.
type UserSession struct {
	SessionID        string     `json:"session_id"`
	UserID           string     `json:"user_id"`
	CookieSecretHash string     `json:"cookie_secret_hash"`
	CSRFTokenHash    string     `json:"csrf_token_hash"`
	CreatedAt        time.Time  `json:"created_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

// SessionStore persists user sessions as one JSON file per session ID.
type SessionStore struct {
	mu    sync.Mutex
	files *store.FileStore
}

// NewSessionStore returns a JSON-backed user session store rooted at dir.
func NewSessionStore(dir string) *SessionStore {
	return &SessionStore{files: store.New(dir)}
}

// Create creates a new session and returns the persisted record, cookie value, and CSRF token.
func (s *SessionStore) Create(userID string, ttl time.Duration, now time.Time) (UserSession, string, string, error) {
	if s == nil || s.files == nil {
		return UserSession{}, "", "", fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return UserSession{}, "", "", fmt.Errorf("user_id is required")
	}
	if ttl <= 0 {
		return UserSession{}, "", "", fmt.Errorf("session ttl must be positive")
	}
	sessionID, err := randomToken()
	if err != nil {
		return UserSession{}, "", "", err
	}
	secret, err := randomToken()
	if err != nil {
		return UserSession{}, "", "", err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return UserSession{}, "", "", err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	session := UserSession{
		SessionID:        sessionID,
		UserID:           userID,
		CookieSecretHash: hashSecret(secret),
		CSRFTokenHash:    hashSecret(csrfToken),
		CreatedAt:        now,
		LastSeenAt:       now,
		ExpiresAt:        now.Add(ttl).UTC(),
	}
	if err := s.save(session); err != nil {
		return UserSession{}, "", "", err
	}
	return session, sessionID + "." + secret, csrfToken, nil
}

// Authenticate validates a session cookie value and refreshes last_seen_at.
func (s *SessionStore) Authenticate(cookieValue string, now time.Time) (UserSession, error) {
	if s == nil || s.files == nil {
		return UserSession{}, fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sessionID, secret, ok := ParseSessionCookieValue(cookieValue)
	if !ok {
		return UserSession{}, ErrSessionInvalid
	}
	session, err := s.load(sessionID)
	if err != nil {
		return UserSession{}, err
	}
	if err := session.active(now); err != nil {
		if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrSessionInvalid) {
			_ = s.files.DeleteJSON(session.SessionID)
		}
		return UserSession{}, err
	}
	if subtle.ConstantTimeCompare([]byte(session.CookieSecretHash), []byte(hashSecret(secret))) != 1 {
		return UserSession{}, ErrSessionInvalid
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	session.LastSeenAt = now
	if err := s.save(session); err != nil {
		return UserSession{}, err
	}
	return session, nil
}

// RotateCSRF replaces a session's CSRF token and returns the plaintext token once.
func (s *SessionStore) RotateCSRF(sessionID string, now time.Time) (UserSession, string, error) {
	if s == nil || s.files == nil {
		return UserSession{}, "", fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.load(sessionID)
	if err != nil {
		return UserSession{}, "", err
	}
	if err := session.active(now); err != nil {
		return UserSession{}, "", err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return UserSession{}, "", err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	session.CSRFTokenHash = hashSecret(csrfToken)
	session.LastSeenAt = now
	if err := s.save(session); err != nil {
		return UserSession{}, "", err
	}
	return session, csrfToken, nil
}

// VerifyCSRF checks a submitted CSRF token against the persisted hash.
func VerifyCSRF(session UserSession, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || session.CSRFTokenHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(session.CSRFTokenHash), []byte(hashSecret(token))) == 1
}

// Revoke marks a session revoked.
func (s *SessionStore) Revoke(sessionID string, now time.Time) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.load(sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return nil
		}
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	session.RevokedAt = &now
	session.LastSeenAt = now
	return s.save(session)
}

// RevokeUserSessionsExcept revokes a user's sessions other than the selected session.
func (s *SessionStore) RevokeUserSessionsExcept(userID string, exceptSessionID string, now time.Time) (int, error) {
	if s == nil || s.files == nil {
		return 0, fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	userID = strings.TrimSpace(userID)
	exceptSessionID = strings.TrimSpace(exceptSessionID)
	if userID == "" {
		return 0, fmt.Errorf("user_id is required")
	}
	paths, err := s.files.ListJSONFiles()
	if err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	revoked := 0
	for _, path := range paths {
		var session UserSession
		if err := s.files.ReadJSONFile(path, &session); err != nil {
			return revoked, err
		}
		if session.UserID != userID || session.SessionID == exceptSessionID || session.RevokedAt != nil || !now.Before(session.ExpiresAt) {
			continue
		}
		session.RevokedAt = &now
		session.LastSeenAt = now
		if err := s.save(session); err != nil {
			return revoked, err
		}
		revoked++
	}
	return revoked, nil
}

// CleanupExpired deletes expired or revoked session records.
func (s *SessionStore) CleanupExpired(now time.Time) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("user session store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := s.files.ListJSONFiles()
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	for _, path := range paths {
		var session UserSession
		if err := s.files.ReadJSONFile(path, &session); err != nil {
			return err
		}
		if session.RevokedAt != nil || !now.Before(session.ExpiresAt) {
			if err := s.files.DeleteJSON(session.SessionID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParseSessionCookieValue parses Proxyma's opaque session_id.secret cookie value.
func ParseSessionCookieValue(value string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *SessionStore) load(sessionID string) (UserSession, error) {
	if s == nil || s.files == nil {
		return UserSession{}, fmt.Errorf("user session store is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return UserSession{}, ErrSessionInvalid
	}
	var session UserSession
	if err := s.files.ReadJSON(sessionID, &session); err != nil {
		return UserSession{}, ErrSessionInvalid
	}
	if strings.TrimSpace(session.SessionID) == "" {
		session.SessionID = sessionID
	}
	return session, nil
}

func (s *SessionStore) save(session UserSession) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("user session store is not configured")
	}
	if strings.TrimSpace(session.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	return s.files.WriteJSON(session.SessionID, session)
}

func (s UserSession) active(now time.Time) error {
	if s.RevokedAt != nil {
		return ErrSessionInvalid
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if !now.Before(s.ExpiresAt) {
		return ErrSessionExpired
	}
	return nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashSecret(secret string) string {
	// SHA-256 is used here to avoid persisting bearer-equivalent cookie or CSRF secrets.
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
