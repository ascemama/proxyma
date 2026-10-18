package policy

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"proxyma/internal/ids"
)

// LoadDir loads one policy object from each .json file in dir.
func LoadDir(dir string) (*Set, []Diagnostic, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	policies := make(map[string]Policy)
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		policy, err := loadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if err := Validate(&policy); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		id := normalizeID(policy.ID)
		if _, ok := policies[id]; ok {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("duplicate policy id %q", id)})
			continue
		}
		policy.ID = id
		policies[id] = policy
	}
	return NewSet(policies), diagnostics, nil
}

func loadFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return Policy{}, fmt.Errorf("policy file not found")
		}
		return Policy{}, err
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return Policy{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return policy, nil
}

// Validate reports whether a policy is safe to store and evaluate.
func Validate(policy *Policy) error {
	switch {
	case policy == nil:
		return fmt.Errorf("missing policy")
	case strings.TrimSpace(policy.ID) == "":
		return fmt.Errorf("missing required field id")
	case !ids.IsUUIDV4(strings.TrimSpace(policy.ID)):
		return fmt.Errorf("id must be a UUID v4")
	case strings.TrimSpace(policy.OwnerUserID) == "":
		return fmt.Errorf("missing required field owner_user_id")
	case !ids.IsUUIDV4(strings.TrimSpace(policy.OwnerUserID)):
		return fmt.Errorf("owner_user_id must be a UUID v4")
	case strings.TrimSpace(policy.Name) == "":
		return fmt.Errorf("missing required field name")
	}
	if policy.LLM != nil && policy.LLM.Enabled {
		if strings.TrimSpace(policy.LLM.ProfileID) == "" {
			return fmt.Errorf("llm.profile_id is required when LLM fallback is enabled")
		}
		if strings.TrimSpace(policy.LLM.Prompt) == "" {
			return fmt.Errorf("llm.prompt is required when LLM fallback is enabled")
		}
		if len(policy.LLM.Prompt) > 16384 {
			return fmt.Errorf("llm.prompt is too long")
		}
	}
	for i := range policy.Rules {
		if err := validateRule(&policy.Rules[i]); err != nil {
			return fmt.Errorf("rule %d: %w", i, err)
		}
	}
	return nil
}

func validateRule(rule *Rule) error {
	switch {
	case rule == nil:
		return fmt.Errorf("missing rule")
	case strings.TrimSpace(rule.ID) == "":
		return fmt.Errorf("missing required field id")
	case !validDecision(rule.Effect):
		return fmt.Errorf("unsupported effect %q", rule.Effect)
	case strings.TrimSpace(rule.CommandTemplate) == "":
		return fmt.Errorf("missing required field command_template")
	}
	if _, err := CompileTemplate(rule.CommandTemplate); err != nil {
		return fmt.Errorf("command_template: %w", err)
	}
	return nil
}

// ValidateRuleForImport validates a rule before it is added to a persisted policy.
func ValidateRuleForImport(rule *Rule) error {
	return validateRule(rule)
}

func validDecision(value string) bool {
	switch value {
	case DecisionAllow, DecisionDeny:
		return true
	default:
		return false
	}
}
