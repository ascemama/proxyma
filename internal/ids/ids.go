// Package ids owns Proxyma opaque identifier generation and validation.
package ids

import "github.com/google/uuid"

// NewUUIDV4 returns a lowercase UUID v4 string.
func NewUUIDV4() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// IsUUIDV4 reports whether value is a lowercase UUID v4 string.
func IsUUIDV4(value string) bool {
	id, err := uuid.Parse(value)
	if err != nil {
		return false
	}
	return id.Version() == 4 && id.String() == value
}
