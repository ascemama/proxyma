package users

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreCreateAuthenticateAndRotateCSRF(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	now := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	session, cookieValue, csrfToken, err := store.Create(testUserID, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID == "" || session.CookieSecretHash == "" || session.CSRFTokenHash == "" || cookieValue == "" || csrfToken == "" {
		t.Fatalf("session = %#v cookie=%q csrf=%q", session, cookieValue, csrfToken)
	}
	if session.CookieSecretHash == cookieValue || session.CSRFTokenHash == csrfToken {
		t.Fatalf("stored plaintext secret: %#v cookie=%q csrf=%q", session, cookieValue, csrfToken)
	}
	if !VerifyCSRF(session, csrfToken) {
		t.Fatal("expected initial CSRF token to verify")
	}

	authenticated, err := store.Authenticate(cookieValue, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.SessionID != session.SessionID || !authenticated.LastSeenAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("authenticated = %#v", authenticated)
	}
	rotated, rotatedCSRF, err := store.RotateCSRF(session.SessionID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rotatedCSRF == csrfToken || !VerifyCSRF(rotated, rotatedCSRF) || VerifyCSRF(rotated, csrfToken) {
		t.Fatalf("csrf rotation failed old=%q new=%q session=%#v", csrfToken, rotatedCSRF, rotated)
	}
}

func TestSessionStoreRejectsInvalidExpiredAndRevokedSessions(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	now := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	session, cookieValue, _, err := store.Create(testUserID, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(session.SessionID+".wrong", now); err != ErrSessionInvalid {
		t.Fatalf("wrong secret err = %v", err)
	}
	if _, err := store.Authenticate(cookieValue, now.Add(time.Hour)); err != ErrSessionExpired {
		t.Fatalf("expired err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.files.Dir(), session.SessionID+".json")); !os.IsNotExist(err) {
		t.Fatalf("expired session was not deleted: %v", err)
	}

	session, cookieValue, _, err = store.Create(testUserID, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(session.SessionID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(cookieValue, now.Add(2*time.Minute)); err != ErrSessionInvalid {
		t.Fatalf("revoked err = %v", err)
	}
}

func TestSessionStoreCleanupExpiredRemovesExpiredAndRevokedRecords(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	now := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	expired, _, _, err := store.Create(testUserID, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	revoked, _, _, err := store.Create(testUserID, time.Hour, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	active, _, _, err := store.Create(testUserID, 2*time.Hour, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(revoked.SessionID, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupExpired(now.Add(70 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []UserSession{expired, revoked} {
		if _, err := os.Stat(filepath.Join(store.files.Dir(), removed.SessionID+".json")); !os.IsNotExist(err) {
			t.Fatalf("session %s still exists: %v", removed.SessionID, err)
		}
	}
	if _, err := os.Stat(filepath.Join(store.files.Dir(), active.SessionID+".json")); err != nil {
		t.Fatalf("active session missing: %v", err)
	}
}

func TestSessionStoreRevokeUserSessionsExcept(t *testing.T) {
	store := NewSessionStore(t.TempDir())
	now := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	current, currentCookie, _, err := store.Create(testUserID, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, otherCookie, _, err := store.Create(testUserID, time.Hour, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, differentUserCookie, _, err := store.Create("a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", time.Hour, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := store.RevokeUserSessionsExcept(testUserID, current.SessionID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
	}
	if _, err := store.Authenticate(currentCookie, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("current session was revoked: %v", err)
	}
	if _, err := store.Authenticate(otherCookie, now.Add(4*time.Minute)); err != ErrSessionInvalid {
		t.Fatalf("other session err = %v, want %v", err, ErrSessionInvalid)
	}
	if _, err := store.Authenticate(differentUserCookie, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("different user's session was revoked: %v", err)
	}
}

func TestParseSessionCookieValue(t *testing.T) {
	id, secret, ok := ParseSessionCookieValue("id.secret")
	if !ok || id != "id" || secret != "secret" {
		t.Fatalf("parsed id=%q secret=%q ok=%t", id, secret, ok)
	}
	for _, value := range []string{"", "id", "id.", ".secret", "a.b.c"} {
		if _, _, ok := ParseSessionCookieValue(value); ok {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
