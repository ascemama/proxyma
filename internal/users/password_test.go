package users

import (
	"strings"
	"testing"
)

func TestGeneratePasswordHashProducesArgon2idPHC(t *testing.T) {
	hash, err := GeneratePasswordHash([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %q", hash)
	}
	if err := ValidatePasswordHash(hash); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword([]byte("correct horse battery staple"), hash); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratePasswordHashUsesDifferentSalts(t *testing.T) {
	left, err := GeneratePasswordHash([]byte("same-password"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := GeneratePasswordHash([]byte("same-password"))
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("expected different hashes for the same password")
	}
}

func TestGeneratePasswordHashRejectsEmptyPassword(t *testing.T) {
	if _, err := GeneratePasswordHash(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := GeneratePasswordHash([]byte("right-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword([]byte("wrong-password"), hash); err == nil {
		t.Fatal("expected error")
	}
}
