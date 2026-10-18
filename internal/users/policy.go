package users

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"proxyma/internal/ids"
	"proxyma/internal/store"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
)

// RuleListDocument is an import-only collection of rules for an existing policy.
type RuleListDocument struct {
	Schema    string          `json:"$schema,omitempty"`
	Version   int             `json:"version"`
	Blacklist []RuleListEntry `json:"blacklist"`
	Whitelist []RuleListEntry `json:"whitelist"`
}

// RuleListEntry is one typed command template in an import document.
type RuleListEntry struct {
	CommandTemplate string `json:"command_template"`
	Reason          string `json:"reason,omitempty"`
}

// RuleImportResult describes an append-only rule-list import.
type RuleImportResult struct {
	Policy    policy.Policy
	Added     []policy.Rule
	Skipped   []RuleListEntry
	Conflicts []RuleListEntry
}

// UpdatePolicy replaces an existing policy while retaining its ID and owner.
func UpdatePolicy(paths Paths, id string, item policy.Policy) (MutationResult, error) {
	id = strings.TrimSpace(id)
	if !ids.IsUUIDV4(id) {
		return MutationResult{}, fmt.Errorf("policy id must be a UUID v4")
	}
	path, err := policyPathByID(paths.PolicyDir, id)
	if err != nil {
		return MutationResult{}, missingError("policy", id, err)
	}
	var existing policy.Policy
	if err := store.New(paths.PolicyDir).ReadJSONFile(path, &existing); err != nil {
		return MutationResult{}, err
	}
	item.ID = id
	item.OwnerUserID = existing.OwnerUserID
	if err := policy.Validate(&item); err != nil {
		return MutationResult{}, err
	}
	policiesStore := store.New(paths.PolicyDir)
	if err := policiesStore.WriteJSON(id, item); err != nil {
		return MutationResult{}, err
	}
	if path != policiesStore.RecordPath(id) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return MutationResult{}, err
		}
	}
	return result("policies", "update", id, policiesStore.RecordPath(id)), nil
}

// CanModifyPolicy reports whether user may edit the policy.
func CanModifyPolicy(item policy.Policy, userID string, admin bool) bool {
	return admin || strings.TrimSpace(item.OwnerUserID) == strings.TrimSpace(userID)
}

// ImportRuleList appends validated blacklist and whitelist entries to a policy.
func ImportRuleList(paths Paths, id string, document RuleListDocument) (RuleImportResult, error) {
	if document.Version != 1 {
		return RuleImportResult{}, fmt.Errorf("version must be 1")
	}
	id = strings.TrimSpace(id)
	path, err := policyPathByID(paths.PolicyDir, id)
	if err != nil {
		return RuleImportResult{}, missingError("policy", id, err)
	}
	var existing policy.Policy
	if err := store.New(paths.PolicyDir).ReadJSONFile(path, &existing); err != nil {
		return RuleImportResult{}, err
	}

	seen := make(map[string]string, len(existing.Rules))
	for _, rule := range existing.Rules {
		seen[policy.NormalizeTemplate(rule.CommandTemplate)] = rule.Effect
	}
	var added []policy.Rule
	var skipped []RuleListEntry
	var conflicts []RuleListEntry
	appendEntries := func(entries []RuleListEntry, effect string) error {
		for _, entry := range entries {
			template := policy.NormalizeTemplate(entry.CommandTemplate)
			ruleID, err := ids.NewUUIDV4()
			if err != nil {
				return err
			}
			rule := policy.Rule{ID: ruleID, Effect: effect, CommandTemplate: template, Reason: strings.TrimSpace(entry.Reason)}
			if err := policy.ValidateRuleForImport(&rule); err != nil {
				return err
			}
			if previous, ok := seen[template]; ok {
				if previous != effect {
					conflicts = append(conflicts, entry)
				} else {
					skipped = append(skipped, entry)
				}
				continue
			}
			seen[template] = effect
			added = append(added, rule)
		}
		return nil
	}
	if err := appendEntries(document.Blacklist, policy.DecisionDeny); err != nil {
		return RuleImportResult{}, err
	}
	if err := appendEntries(document.Whitelist, policy.DecisionAllow); err != nil {
		return RuleImportResult{}, err
	}
	if len(conflicts) > 0 {
		return RuleImportResult{Policy: existing, Skipped: skipped, Conflicts: conflicts}, fmt.Errorf("import contains conflicting command templates")
	}
	existing.Rules = append(existing.Rules, added...)
	if err := policy.Validate(&existing); err != nil {
		return RuleImportResult{}, err
	}
	if err := store.New(paths.PolicyDir).WriteJSON(existing.ID, existing); err != nil {
		return RuleImportResult{}, err
	}
	return RuleImportResult{Policy: existing, Added: added, Skipped: skipped}, nil
}

// AssignedTargets returns targets currently referencing a policy.
func AssignedTargets(paths Paths, policyID string) ([]targets.Target, error) {
	registry, err := loadTargets(paths)
	if err != nil {
		return nil, err
	}
	var assigned []targets.Target
	for _, target := range registry.All() {
		if target.PolicyID == strings.TrimSpace(policyID) {
			assigned = append(assigned, target)
		}
	}
	sort.Slice(assigned, func(i, j int) bool { return assigned[i].ID < assigned[j].ID })
	return assigned, nil
}

// AssignPolicyTargets replaces assignments on targets visible to the acting user.
func AssignPolicyTargets(paths Paths, policyID, userID string, admin bool, targetIDs []string) error {
	policyID = strings.TrimSpace(policyID)
	if _, err := policyPathByID(paths.PolicyDir, policyID); err != nil {
		return missingError("policy", policyID, err)
	}
	registry, err := loadTargets(paths)
	if err != nil {
		return err
	}
	selected := make(map[string]bool, len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID != "" {
			selected[targetID] = true
		}
	}
	for targetID := range selected {
		found := false
		for _, target := range registry.All() {
			if target.ID == targetID {
				found = true
				if !admin && !contains(target.UserIDs, userID) {
					return fmt.Errorf("target %q is not owned by current user", targetID)
				}
			}
		}
		if !found {
			return fmt.Errorf("target %q does not exist", targetID)
		}
	}
	updates := make([]targets.Target, 0, len(selected))
	for _, target := range registry.All() {
		if !admin && !contains(target.UserIDs, userID) {
			continue
		}
		if !selected[target.ID] || target.PolicyID == policyID {
			continue
		}
		target.PolicyID = policyID
		if err := validateTarget(paths, &target); err != nil {
			return err
		}
		updates = append(updates, target)
	}
	for _, target := range updates {
		if err := store.New(paths.TargetDir).WriteJSON(target.ID, target); err != nil {
			return err
		}
	}
	return nil
}
