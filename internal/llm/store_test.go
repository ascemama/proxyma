package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndLoadProtectsProfileDirectoryAndKeysArePubliclyRedacted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	profile := Profile{ID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c", OwnerUserID: "14fb57cf-11a1-45ed-ad51-26d733a0b2db", Name: "test", Provider: ProviderOpenAI, Model: "test-model", BaseURL: "https://api.openai.com/v1", APIKey: "secret", TimeoutSeconds: 30, Enabled: true}
	if err := Write(dir, profile); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", info.Mode().Perm())
	}
	loaded, diagnostics, err := LoadDir(dir)
	if err != nil || len(diagnostics) != 0 || len(loaded) != 1 {
		t.Fatalf("loaded=%#v diagnostics=%#v err=%v", loaded, diagnostics, err)
	}
	public := loaded[0].Public()
	if public.HasAPIKey != true || public.ID != profile.ID {
		t.Fatalf("public profile = %#v", public)
	}
	if publicProfile := (&Catalog{profiles: map[string]Profile{profile.ID: profile}}).Profiles()[0]; publicProfile.APIKey != "" {
		t.Fatalf("catalog exposed API key: %#v", publicProfile)
	}
}
