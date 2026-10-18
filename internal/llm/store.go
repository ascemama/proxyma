package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"proxyma/internal/ids"
)

type Diagnostic struct {
	Path    string
	Message string
}

func LoadDir(dir string) ([]Profile, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	profiles := make([]Profile, 0, len(entries))
	var diagnostics []Diagnostic
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: readErr.Error()})
			continue
		}
		var profile Profile
		if decodeErr := json.Unmarshal(data, &profile); decodeErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("invalid JSON: %v", decodeErr)})
			continue
		}
		if err := validateStoredProfile(&profile); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if seen[profile.ID] {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate profile id %q", profile.ID)})
			continue
		}
		seen[profile.ID] = true
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return profiles, diagnostics, nil
}

func Write(dir string, profile Profile) error {
	if err := validateStoredProfile(&profile); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, profile.ID+".json")); err != nil {
		return err
	}
	return nil
}

func Delete(dir, id string) error {
	if !ids.IsUUIDV4(strings.TrimSpace(id)) {
		return fmt.Errorf("LLM profile id must be a UUID v4")
	}
	if err := os.Remove(filepath.Join(dir, strings.TrimSpace(id)+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validateStoredProfile(profile *Profile) error {
	if profile == nil || !ids.IsUUIDV4(strings.TrimSpace(profile.ID)) {
		return errors.New("LLM profile id must be a UUID v4")
	}
	if !ids.IsUUIDV4(strings.TrimSpace(profile.OwnerUserID)) {
		return errors.New("LLM profile owner_user_id must be a UUID v4")
	}
	return ValidateProfile(profile)
}
