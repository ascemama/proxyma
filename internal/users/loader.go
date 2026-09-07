package users

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"proxyma/internal/ids"
)

// LoadDir loads one JSON user object from each .json file in dir.
func LoadDir(dir string) (Registry, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Registry{}, nil, err
	}

	var diagnostics []Diagnostic
	var loaded []User
	seenIDs := make(map[string]string)
	seenUsernames := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		user, err := loadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if err := validate(user); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		id := normalizeID(user.ID)
		if firstPath, ok := seenIDs[id]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate user id %q also used by %s", id, firstPath)})
			continue
		}
		username := NormalizeUsername(user.Username)
		if firstPath, ok := seenUsernames[username]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate username %q also used by %s", username, firstPath)})
			continue
		}
		seenIDs[id] = path
		seenUsernames[username] = path
		loaded = append(loaded, user)
	}

	return NewRegistry(loaded), diagnostics, nil
}

func loadFile(path string) (User, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return User{}, fmt.Errorf("user file not found")
		}
		return User{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return User{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, ok := raw["admin"]; !ok {
		return User{}, fmt.Errorf("missing required field admin")
	}
	if _, ok := raw["username"]; !ok {
		return User{}, fmt.Errorf("missing required field username")
	}
	if _, ok := raw["status"]; !ok {
		return User{}, fmt.Errorf("missing required field status")
	}
	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return User{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return user, nil
}

func validate(user User) error {
	switch {
	case strings.TrimSpace(user.ID) == "":
		return fmt.Errorf("missing required field id")
	case !ids.IsUUIDV4(strings.TrimSpace(user.ID)):
		return fmt.Errorf("id must be a UUID v4")
	case !validUsername(user.Username):
		return fmt.Errorf("username must be 3-64 lowercase letters, numbers, dots, underscores, or hyphens")
	case strings.TrimSpace(user.Name) == "":
		return fmt.Errorf("missing required field name")
	case strings.TrimSpace(user.Status) == "":
		return fmt.Errorf("missing required field status")
	case !ValidStatus(user.Status):
		return fmt.Errorf("status must be active, pending_activation, or password_reset_pending")
	case user.Status == StatusActive && strings.TrimSpace(user.PasswdHash) == "":
		return fmt.Errorf("active user requires passwd_hash")
	case user.Status != StatusActive && strings.TrimSpace(user.PasswdHash) != "":
		return fmt.Errorf("non-active user must not include passwd_hash")
	case !ValidTheme(user.Theme):
		return fmt.Errorf("theme must be light or dark")
	case user.Status == StatusActive && ValidatePasswordHash(user.PasswdHash) != nil:
		return ValidatePasswordHash(user.PasswdHash)
	default:
		return nil
	}
}

func validUsername(value string) bool {
	value = strings.TrimSpace(value)
	if value != NormalizeUsername(value) || len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
