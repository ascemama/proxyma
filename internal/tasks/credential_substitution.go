package tasks

import (
	"fmt"
	"strings"

	"proxyma/internal/targets"
)

// ResolvedCommand is the transient command text Proxyma executes after substitution.
type ResolvedCommand struct {
	Command          string
	SensitiveValues  []string
	UsedPlaceholders []string
}

// ResolveCredentialPlaceholders replaces supported credential placeholders from target.
func ResolveCredentialPlaceholders(command string, target targets.Target) (ResolvedCommand, error) {
	resolved := ResolvedCommand{Command: command}
	usesUser := strings.Contains(command, PlaceholderUser)
	usesPassword := strings.Contains(command, PlaceholderPassword)
	if !usesUser && !usesPassword {
		return resolved, nil
	}

	if err := validatePlaceholderCredentialValues(target, usesUser, usesPassword); err != nil {
		return ResolvedCommand{}, err
	}

	used := make([]string, 0, 2)
	if usesUser {
		resolved.Command = strings.ReplaceAll(resolved.Command, PlaceholderUser, target.Username)
		resolved.SensitiveValues = append(resolved.SensitiveValues, target.Username)
		used = append(used, "user")
	}
	if usesPassword {
		password, _ := target.PasswordCredential()
		resolved.Command = strings.ReplaceAll(resolved.Command, PlaceholderPassword, password)
		resolved.SensitiveValues = append(resolved.SensitiveValues, password)
		used = append(used, "password")
	}
	resolved.UsedPlaceholders = used
	return resolved, nil
}

func validatePlaceholderCredentialValues(target targets.Target, usesUser, usesPassword bool) error {
	if usesUser && strings.TrimSpace(target.Username) == "" {
		return fmt.Errorf("matched target cannot resolve %s", PlaceholderUser)
	}
	if usesPassword {
		if _, ok := target.PasswordCredential(); !ok {
			return fmt.Errorf("matched target cannot resolve %s", PlaceholderPassword)
		}
	}
	return nil
}
