// Package users loads Proxyma user identities for ownership metadata.
package users

import "strings"

const (
	ThemeLight                 = "light"
	ThemeDark                  = "dark"
	StatusActive               = "active"
	StatusPendingActivation    = "pending_activation"
	StatusPasswordResetPending = "password_reset_pending"
)

// User represents one Proxyma user.
type User struct {
	Schema     string `json:"$schema,omitempty"`
	ID         string `json:"id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	Admin      bool   `json:"admin"`
	Status     string `json:"status"`
	PasswdHash string `json:"passwd_hash,omitempty"`
	Theme      string `json:"theme,omitempty"`
}

// ValidStatus reports whether value is a supported persisted user status.
func ValidStatus(value string) bool {
	return value == StatusActive || value == StatusPendingActivation || value == StatusPasswordResetPending
}

// EffectiveTheme returns the user's supported theme, defaulting legacy records to light.
func EffectiveTheme(value string) string {
	if strings.TrimSpace(value) == ThemeDark {
		return ThemeDark
	}
	return ThemeLight
}

// ValidTheme reports whether a persisted or requested theme is supported.
func ValidTheme(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == ThemeLight || value == ThemeDark
}

// Diagnostic describes a rejected user file.
type Diagnostic struct {
	Path    string
	Message string
}

// Registry stores known Proxyma users by ID.
type Registry struct {
	users      map[string]User
	byUsername map[string]User
}

// NewRegistry returns a registry containing the provided users.
func NewRegistry(items []User) Registry {
	usersByID := make(map[string]User, len(items))
	usersByUsername := make(map[string]User, len(items))
	for _, user := range items {
		usersByID[normalizeID(user.ID)] = user
		if username := NormalizeUsername(user.Username); username != "" {
			usersByUsername[username] = user
		}
	}
	return Registry{users: usersByID, byUsername: usersByUsername}
}

// All returns a copy of all known users.
func (r Registry) All() []User {
	out := make([]User, 0, len(r.users))
	for _, user := range r.users {
		out = append(out, user)
	}
	return out
}

// Find returns the user with the given ID.
func (r Registry) Find(id string) (User, bool) {
	if r.users == nil {
		return User{}, false
	}
	user, ok := r.users[normalizeID(id)]
	return user, ok
}

// FindByUsername returns the user with the given login username.
func (r Registry) FindByUsername(username string) (User, bool) {
	if r.byUsername == nil {
		return User{}, false
	}
	user, ok := r.byUsername[NormalizeUsername(username)]
	return user, ok
}

// NormalizeUsername returns Proxyma's canonical username key.
func NormalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeID(value string) string {
	return strings.TrimSpace(value)
}
