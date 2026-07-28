package agents

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"proxyma/internal/ids"
)

// LoadDir loads one JSON agent object from each .json file in dir.
func LoadDir(dir string) (Registry, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Registry{}, nil, err
	}

	var diagnostics []Diagnostic
	var loaded []Agent
	seenIDs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		agent, err := loadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if err := validate(agent); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		agent = Normalize(agent)
		id := normalizeID(agent.ID)
		if firstPath, ok := seenIDs[id]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate agent id %q also used by %s", id, firstPath)})
			continue
		}
		seenIDs[id] = path
		loaded = append(loaded, agent)
	}

	return NewRegistry(loaded), diagnostics, nil
}

func loadFile(path string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return Agent{}, fmt.Errorf("agent file not found")
		}
		return Agent{}, err
	}
	var agent Agent
	if err := json.Unmarshal(data, &agent); err != nil {
		return Agent{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return agent, nil
}

// Validate validates an agent definition.
func Validate(agent Agent) error {
	return validate(agent)
}

func validate(agent Agent) error {
	switch {
	case strings.TrimSpace(agent.ID) == "":
		return fmt.Errorf("missing required field id")
	case !ids.IsUUIDV4(strings.TrimSpace(agent.ID)):
		return fmt.Errorf("id must be a UUID v4")
	case strings.TrimSpace(agent.SharedSecret) == "":
		return fmt.Errorf("missing required field shared_secret")
	case len(agent.UserIDs) == 0:
		return fmt.Errorf("missing required field user_ids")
	case !ValidateInteractionInstanceID(agent.InteractionInstanceID):
		return fmt.Errorf("interaction_instance_id must be local or a UUID v4")
	default:
		return validateUserIDs(agent.UserIDs)
	}
}

func validateUserIDs(userIDs []string) error {
	seen := make(map[string]bool, len(userIDs))
	for i, userID := range userIDs {
		userIDs[i] = strings.TrimSpace(userID)
		if userIDs[i] == "" {
			return fmt.Errorf("user_ids must not contain empty values")
		}
		if seen[userIDs[i]] {
			return fmt.Errorf("user_ids must not contain duplicate values")
		}
		seen[userIDs[i]] = true
	}
	return nil
}
