package users

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"proxyma/internal/store"
)

func TestActivationStorePersistsHashExpiresAndRegenerates(t *testing.T) {
	dir := t.TempDir()
	activationStore := NewCredentialTokenStore(dir)
	now := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	userID := "fcd2f3a9-d546-44f4-837b-0c7aa4219752"
	record, token, err := activationStore.Create(userID, CredentialTokenPurposeActivation, 12*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, userID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), strings.Split(token, ".")[1]) {
		t.Fatalf("activation record leaked raw token: %s", data)
	}
	if record.Purpose != CredentialTokenPurposeActivation || record.SecretHash == "" || !record.ExpiresAt.Equal(now.Add(12*time.Hour)) {
		t.Fatalf("record = %#v", record)
	}
	if _, err := activationStore.Inspect(token, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	_, replacement, err := activationStore.Create(userID, CredentialTokenPurposeActivation, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activationStore.Inspect(token, now); err != ErrActivationInvalid {
		t.Fatalf("old token err = %v", err)
	}
	if _, err := activationStore.Inspect(replacement, now.Add(time.Hour)); err != ErrActivationExpired {
		t.Fatalf("expired token err = %v", err)
	}
}

func TestCredentialTokenStoreRejectsMissingPurposeAndStatusMismatch(t *testing.T) {
	root := t.TempDir()
	paths := Paths{UserDir: filepath.Join(root, "users"), CredentialTokenDir: filepath.Join(root, "credential_tokens")}
	tokenStore := NewCredentialTokenStore(paths.CredentialTokenDir)
	result, err := CreateUser(paths, User{Username: "pending", Name: "Pending", Status: StatusPendingActivation}, fixedID("fcd2f3a9-d546-44f4-837b-0c7aa4219752"))
	if err != nil {
		t.Fatal(err)
	}
	record, token, err := tokenStore.Create(result.ID, CredentialTokenPurposeActivation, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	record.Purpose = ""
	if err := store.New(paths.CredentialTokenDir).WriteJSON(result.ID, record); err != nil {
		t.Fatal(err)
	}
	if _, err := tokenStore.Inspect(token, time.Now()); err != ErrActivationInvalid {
		t.Fatalf("missing purpose err = %v", err)
	}
	_, mismatched, err := tokenStore.Create(result.ID, CredentialTokenPurposePasswordReset, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tokenStore.Complete(paths, mismatched, []byte("new-password"), time.Now()); err != ErrActivationInvalid {
		t.Fatalf("purpose/status mismatch err = %v", err)
	}
}

func TestActivationStoreActivatesExactlyOnce(t *testing.T) {
	root := t.TempDir()
	paths := Paths{UserDir: filepath.Join(root, "users"), CredentialTokenDir: filepath.Join(root, "credential_tokens")}
	activationStore := NewCredentialTokenStore(paths.CredentialTokenDir)
	result, err := CreateUser(paths, User{Username: "pending", Name: "Pending", Status: StatusPendingActivation}, fixedID("fcd2f3a9-d546-44f4-837b-0c7aa4219752"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := activationStore.Create(result.ID, CredentialTokenPurposeActivation, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := activationStore.Complete(paths, token, []byte("new-password"), time.Now()); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successes = %d", successes)
	}
	var stored User
	if err := store.New(paths.UserDir).ReadJSON(result.ID, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusActive || VerifyPassword([]byte("new-password"), stored.PasswdHash) != nil {
		t.Fatalf("stored = %#v", stored)
	}
	if _, err := activationStore.Inspect(token, time.Now()); err != ErrActivationInvalid {
		t.Fatalf("consumed token err = %v", err)
	}
}

func TestActivationStorePasswordResetLifecycle(t *testing.T) {
	root := t.TempDir()
	paths := Paths{UserDir: filepath.Join(root, "users"), CredentialTokenDir: filepath.Join(root, "credential_tokens")}
	activationStore := NewCredentialTokenStore(paths.CredentialTokenDir)
	hash, err := GeneratePasswordHash([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := CreateUser(paths, User{Username: "reset-user", Name: "Reset User", Status: StatusActive, PasswdHash: hash}, fixedID("a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := activationStore.BeginPasswordReset(paths, result.ID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var pending User
	if err := store.New(paths.UserDir).ReadJSON(result.ID, &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Status != StatusPasswordResetPending || pending.PasswdHash != "" {
		t.Fatalf("pending = %#v", pending)
	}
	_, replacement, err := activationStore.BeginPasswordReset(paths, result.ID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := activationStore.Inspect(token, time.Now()); err != ErrActivationInvalid {
		t.Fatalf("old token err = %v", err)
	}
	completed, err := activationStore.Complete(paths, replacement, []byte("new-password"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusActive || VerifyPassword([]byte("new-password"), completed.PasswdHash) != nil {
		t.Fatalf("completed = %#v", completed)
	}
	if VerifyPassword([]byte("old-password"), completed.PasswdHash) == nil {
		t.Fatal("old password still verifies")
	}
}
