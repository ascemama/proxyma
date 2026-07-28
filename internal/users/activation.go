package users

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"proxyma/internal/store"
)

var (
	ErrActivationInvalid = errors.New("activation token is invalid")
	ErrActivationExpired = errors.New("activation token has expired")
)

const (
	CredentialTokenPurposeActivation    = "activation"
	CredentialTokenPurposePasswordReset = "password_reset"
)

// CredentialTokenRecord is persisted runtime state for one pending credential operation.
type CredentialTokenRecord struct {
	UserID     string    `json:"user_id"`
	Purpose    string    `json:"purpose"`
	SecretHash string    `json:"secret_hash"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// CredentialTokenStore persists and validates one credential token record per user.
type CredentialTokenStore struct {
	mu    sync.Mutex
	files *store.FileStore
}

func NewCredentialTokenStore(dir string) *CredentialTokenStore {
	return &CredentialTokenStore{files: store.New(dir)}
}

// Create replaces any prior token and returns the plaintext token once.
func (s *CredentialTokenStore) Create(userID string, purpose string, ttl time.Duration, now time.Time) (CredentialTokenRecord, string, error) {
	if s == nil || s.files == nil {
		return CredentialTokenRecord{}, "", fmt.Errorf("credential token store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked(userID, purpose, ttl, now)
}

// BeginPasswordReset issues a token and removes the active user's old credential.
func (s *CredentialTokenStore) BeginPasswordReset(paths Paths, userID string, ttl time.Duration, now time.Time) (CredentialTokenRecord, string, error) {
	if s == nil || s.files == nil {
		return CredentialTokenRecord{}, "", fmt.Errorf("credential token store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	usersStore := store.New(paths.UserDir)
	user, recordName, err := findUserRecord(usersStore, strings.TrimSpace(userID))
	if err != nil {
		return CredentialTokenRecord{}, "", err
	}
	if user.Status != StatusActive && user.Status != StatusPasswordResetPending {
		return CredentialTokenRecord{}, "", fmt.Errorf("password reset requires an active or password_reset_pending user")
	}
	record, token, err := s.createLocked(user.ID, CredentialTokenPurposePasswordReset, ttl, now)
	if err != nil {
		return CredentialTokenRecord{}, "", err
	}
	if user.Status == StatusPasswordResetPending {
		return record, token, nil
	}
	user.Status = StatusPasswordResetPending
	user.PasswdHash = ""
	if err := validate(user); err != nil {
		_ = s.files.DeleteJSON(user.ID)
		return CredentialTokenRecord{}, "", err
	}
	if err := usersStore.WriteJSON(recordName, user); err != nil {
		_ = s.files.DeleteJSON(user.ID)
		return CredentialTokenRecord{}, "", err
	}
	return record, token, nil
}

func (s *CredentialTokenStore) createLocked(userID string, purpose string, ttl time.Duration, now time.Time) (CredentialTokenRecord, string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || !validCredentialTokenPurpose(purpose) || ttl <= 0 {
		return CredentialTokenRecord{}, "", fmt.Errorf("user id, valid purpose, and positive credential token ttl are required")
	}
	secret, err := randomToken()
	if err != nil {
		return CredentialTokenRecord{}, "", err
	}
	now = normalizedTime(now)
	record := CredentialTokenRecord{UserID: userID, Purpose: purpose, SecretHash: hashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(ttl).UTC()}
	if err := s.files.WriteJSON(userID, record); err != nil {
		return CredentialTokenRecord{}, "", err
	}
	return record, userID + "." + secret, nil
}

// Inspect validates a token without consuming it.
func (s *CredentialTokenStore) Inspect(token string, now time.Time) (CredentialTokenRecord, error) {
	if s == nil || s.files == nil {
		return CredentialTokenRecord{}, fmt.Errorf("credential token store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inspectLocked(token, now)
}

// Activate validates and consumes a token by setting the pending user's password.
func (s *CredentialTokenStore) Complete(paths Paths, token string, password []byte, now time.Time) (User, error) {
	if s == nil || s.files == nil {
		return User{}, fmt.Errorf("credential token store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.inspectLocked(token, now)
	if err != nil {
		return User{}, err
	}
	usersStore := store.New(paths.UserDir)
	user, recordName, err := findUserRecord(usersStore, record.UserID)
	if err != nil || !credentialTokenMatchesStatus(record.Purpose, user.Status) {
		return User{}, ErrActivationInvalid
	}
	user, err = ResetPassword(user, password)
	if err != nil {
		return User{}, err
	}
	user.Status = StatusActive
	if err := validate(user); err != nil {
		return User{}, err
	}
	if err := usersStore.WriteJSON(recordName, user); err != nil {
		return User{}, err
	}
	// The user is already active, so a cleanup failure cannot make the token reusable.
	_ = s.files.DeleteJSON(record.UserID)
	return user, nil
}

func (s *CredentialTokenStore) Delete(userID string) error {
	if s == nil || s.files == nil {
		return fmt.Errorf("credential token store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files.DeleteJSON(strings.TrimSpace(userID))
}

func (s *CredentialTokenStore) inspectLocked(token string, now time.Time) (CredentialTokenRecord, error) {
	userID, secret, ok := ParseSessionCookieValue(token)
	if !ok {
		return CredentialTokenRecord{}, ErrActivationInvalid
	}
	var record CredentialTokenRecord
	if err := s.files.ReadJSON(userID, &record); err != nil || record.UserID != userID || !validCredentialTokenPurpose(record.Purpose) || record.SecretHash == "" {
		return CredentialTokenRecord{}, ErrActivationInvalid
	}
	if subtle.ConstantTimeCompare([]byte(record.SecretHash), []byte(hashSecret(secret))) != 1 {
		return CredentialTokenRecord{}, ErrActivationInvalid
	}
	now = normalizedTime(now)
	if !now.Before(record.ExpiresAt) {
		_ = s.files.DeleteJSON(userID)
		return CredentialTokenRecord{}, ErrActivationExpired
	}
	return record, nil
}

func validCredentialTokenPurpose(purpose string) bool {
	return purpose == CredentialTokenPurposeActivation || purpose == CredentialTokenPurposePasswordReset
}

func credentialTokenMatchesStatus(purpose string, status string) bool {
	switch purpose {
	case CredentialTokenPurposeActivation:
		return status == StatusPendingActivation
	case CredentialTokenPurposePasswordReset:
		return status == StatusPasswordResetPending
	default:
		return false
	}
}

func normalizedTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
