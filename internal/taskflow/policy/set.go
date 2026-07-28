package policy

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Policy is one user-authored command policy file.
type Policy struct {
	Schema      string `json:"$schema,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Priority    int    `json:"priority,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Rules       []Rule `json:"rules"`
}

// Rule maps a command match condition to a policy effect.
type Rule struct {
	ID                     string   `json:"id"`
	Effect                 string   `json:"effect"`
	RemoteCommandRegex     string   `json:"remote_command_regex,omitempty"`
	RemoteCommandPrefixAny []string `json:"remote_command_prefix_any,omitempty"`
	Always                 bool     `json:"always,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
}

// Set evaluates commands against named policies.
type Set struct {
	policies map[string]compiledPolicy
}

type compiledPolicy struct {
	policy Policy
	rules  []compiledRule
}

type compiledRule struct {
	rule  Rule
	regex *regexp.Regexp
}

type match struct {
	result     Result
	priority   int
	effectRank int
	order      int
}

// NewSet returns an evaluator containing the provided policies.
func NewSet(policies map[string]Policy) *Set {
	set := &Set{policies: make(map[string]compiledPolicy, len(policies))}
	for id, policy := range policies {
		var rules []compiledRule
		for _, rule := range policy.Rules {
			compiled := compiledRule{rule: rule}
			if rule.RemoteCommandRegex != "" {
				compiled.regex = regexp.MustCompile(rule.RemoteCommandRegex)
			}
			rules = append(rules, compiled)
		}
		set.policies[id] = compiledPolicy{policy: policy, rules: rules}
	}
	return set
}

// All returns every configured policy sorted by ID.
func (s *Set) All() []Policy {
	if s == nil {
		return nil
	}
	items := make([]Policy, 0, len(s.policies))
	for _, compiled := range s.policies {
		policy := compiled.policy
		policy.Rules = append([]Rule(nil), policy.Rules...)
		items = append(items, policy)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items
}

// Has reports whether a policy id is configured.
func (s *Set) Has(policyID string) bool {
	if s == nil {
		return false
	}
	_, ok := s.policies[normalizeID(policyID)]
	return ok
}

// ValidateReferences reports target policy ids that are not configured.
func (s *Set) ValidateReferences(target string, policyIDs []string) []Diagnostic {
	var diagnostics []Diagnostic
	for _, id := range policyIDs {
		if !s.Has(id) {
			diagnostics = append(diagnostics, Diagnostic{Path: target, Message: fmt.Sprintf("unknown policy id %q", id)})
		}
	}
	return diagnostics
}

// Evaluate returns the winning policy decision for a prepared command.
func (s *Set) Evaluate(_ context.Context, request Request) (Result, error) {
	if s == nil {
		return Result{Decision: DecisionDeny, Reason: "target policy evaluator is not configured"}, nil
	}
	policyIDs := request.PolicyIDs
	if len(policyIDs) == 0 {
		policyIDs = request.Target.PolicyIDs
	}
	if len(policyIDs) == 0 {
		return Result{Decision: DecisionDeny, Reason: "target is missing policy_ids"}, nil
	}
	var matches []match
	order := 0
	for _, id := range policyIDs {
		id = normalizeID(id)
		policy, ok := s.policies[id]
		if !ok {
			return Result{Decision: DecisionDeny, PolicyID: id, Reason: fmt.Sprintf("target references unknown policy id %q", id)}, nil
		}
		if !policy.enabled() {
			continue
		}
		for _, rule := range policy.rules {
			order++
			if !rule.matches(request.RemoteCommand) {
				continue
			}
			matches = append(matches, match{
				result: Result{
					Decision: rule.rule.Effect,
					PolicyID: policy.policy.ID,
					RuleID:   rule.rule.ID,
					Reason:   rule.rule.Reason,
				},
				priority:   policy.policy.Priority,
				effectRank: rankEffect(rule.rule.Effect),
				order:      order,
			})
		}
	}
	if len(matches) == 0 {
		return Result{Decision: DecisionApproval, Reason: "no policy rule matched command"}, nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].effectRank != matches[j].effectRank {
			return matches[i].effectRank > matches[j].effectRank
		}
		if matches[i].priority != matches[j].priority {
			return matches[i].priority > matches[j].priority
		}
		return matches[i].order < matches[j].order
	})
	return matches[0].result, nil
}

func (p compiledPolicy) enabled() bool {
	return p.policy.Enabled == nil || *p.policy.Enabled
}

func (r compiledRule) matches(remoteCommand string) bool {
	remoteCommand = strings.TrimSpace(remoteCommand)
	if r.rule.Always {
		return true
	}
	if r.regex != nil && r.regex.MatchString(remoteCommand) {
		return true
	}
	for _, prefix := range r.rule.RemoteCommandPrefixAny {
		if strings.HasPrefix(remoteCommand, strings.TrimSpace(prefix)) {
			return true
		}
	}
	return false
}

func rankEffect(effect string) int {
	switch effect {
	case DecisionDeny:
		return 1
	default:
		return 0
	}
}
