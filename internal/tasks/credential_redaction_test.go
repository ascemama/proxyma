package tasks

import (
	"strings"
	"testing"
)

func TestRedactCredentialValues(t *testing.T) {
	got := RedactCredentialValues("ssh demo:secret@example", "demo", "secret")
	if strings.Contains(got, "demo") || strings.Contains(got, "secret") {
		t.Fatalf("redaction leaked credential values: %q", got)
	}
	if want := "ssh [REDACTED]:[REDACTED]@example"; got != want {
		t.Fatalf("redacted = %q, want %q", got, want)
	}
}

func TestRedactCredentialValuesIgnoresEmptyValues(t *testing.T) {
	got := RedactCredentialValues("abc", "", " ")
	if got != "abc" {
		t.Fatalf("redacted = %q, want abc", got)
	}
}
