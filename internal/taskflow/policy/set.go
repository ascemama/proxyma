package policy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"proxyma/internal/llm"
)

// Policy is one user-authored command policy file.
type Policy struct {
	Schema      string     `json:"$schema,omitempty"`
	ID          string     `json:"id"`
	OwnerUserID string     `json:"owner_user_id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Enabled     bool       `json:"enabled"`
	Rules       []Rule     `json:"rules"`
	LLM         *LLMPolicy `json:"llm,omitempty"`
}

type LLMPolicy struct {
	Enabled   bool   `json:"enabled"`
	ProfileID string `json:"profile_id,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

// Rule maps a command match condition to a policy effect.
type Rule struct {
	ID              string `json:"id"`
	Effect          string `json:"effect"`
	CommandTemplate string `json:"command_template"`
	Reason          string `json:"reason,omitempty"`
}

// Set evaluates commands against named policies.
type Set struct {
	policies map[string]compiledPolicy
	llm      llm.Reviewer
}

func (s *Set) SetReviewer(reviewer llm.Reviewer) {
	if s != nil {
		s.llm = reviewer
	}
}

type compiledPolicy struct {
	policy Policy
	rules  []compiledRule
}

type compiledRule struct {
	rule    Rule
	matcher *TemplateMatcher
}

type match struct {
	result     Result
	effectRank int
	order      int
}

// NewSet returns an evaluator containing the provided policies.
func NewSet(policies map[string]Policy) *Set {
	return NewSetWithLLM(policies, nil)
}

func NewSetWithLLM(policies map[string]Policy, reviewer llm.Reviewer) *Set {
	set := &Set{policies: make(map[string]compiledPolicy, len(policies))}
	set.llm = reviewer
	for id, policy := range policies {
		var rules []compiledRule
		for _, rule := range policy.Rules {
			compiled := compiledRule{rule: rule, matcher: MustCompileTemplate(rule.CommandTemplate)}
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
		if policy.LLM != nil {
			copy := *policy.LLM
			policy.LLM = &copy
		}
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

// ValidateReference reports a target policy id that is not configured.
func (s *Set) ValidateReference(target, policyID string) []Diagnostic {
	if !s.Has(policyID) {
		return []Diagnostic{{Path: target, Message: fmt.Sprintf("unknown policy id %q", policyID)}}
	}
	return nil
}

// Evaluate returns the winning policy decision for a prepared command.
func (s *Set) Evaluate(ctx context.Context, request Request) (Result, error) {
	if s == nil {
		return Result{Decision: DecisionDeny, Reason: "target policy evaluator is not configured"}, nil
	}
	policyID := normalizeID(request.PolicyID)
	if policyID == "" {
		policyID = normalizeID(request.Target.PolicyID)
	}
	if policyID == "" {
		return Result{Decision: DecisionDeny, Reason: "target is missing policy_id"}, nil
	}
	var matches []match
	order := 0
	policy, ok := s.policies[policyID]
	if !ok {
		return Result{Decision: DecisionDeny, PolicyID: policyID, Reason: fmt.Sprintf("target references unknown policy id %q", policyID)}, nil
	}
	if !policy.enabled() {
		return Result{Decision: DecisionApproval, PolicyID: policy.policy.ID, Reason: "policy is disabled"}, nil
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
				Source:   "rule",
			},
			effectRank: rankEffect(rule.rule.Effect),
			order:      order,
		})
	}
	if len(matches) == 0 {
		if policy.policy.LLM != nil && policy.policy.LLM.Enabled {
			if strings.TrimSpace(policy.policy.LLM.ProfileID) == "" || strings.TrimSpace(policy.policy.LLM.Prompt) == "" || s.llm == nil {
				return Result{Decision: DecisionApproval, PolicyID: policy.policy.ID, Reason: "LLM policy is unavailable; user approval required", Source: "llm", LLMProfileID: policy.policy.LLM.ProfileID}, nil
			}
			llmResult, err := s.llm.Review(ctx, policy.policy.LLM.ProfileID, llm.ReviewRequest{Command: request.RemoteCommand, Prompt: policy.policy.LLM.Prompt})
			if err != nil || !llmResult.Valid {
				return Result{Decision: DecisionApproval, PolicyID: policy.policy.ID, Reason: "LLM response was unavailable or invalid; user approval required", Source: "llm", LLMProfileID: policy.policy.LLM.ProfileID}, nil
			}
			decision := DecisionApproval
			reason := "LLM requires user approval"
			switch llmResult.Decision {
			case llm.DecisionApproved:
				decision, reason = DecisionAllow, "approved by LLM policy"
			case llm.DecisionDenied:
				decision, reason = DecisionDeny, "denied by LLM policy"
			}
			return Result{Decision: decision, PolicyID: policy.policy.ID, Reason: reason, Source: "llm", LLMProfileID: policy.policy.LLM.ProfileID}, nil
		}
		return Result{Decision: DecisionApproval, Reason: "no policy rule matched command"}, nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].effectRank != matches[j].effectRank {
			return matches[i].effectRank > matches[j].effectRank
		}
		return matches[i].order < matches[j].order
	})
	return matches[0].result, nil
}

func (p compiledPolicy) enabled() bool {
	return p.policy.Enabled
}

func (r compiledRule) matches(remoteCommand string) bool {
	return r.matcher != nil && r.matcher.Match(remoteCommand)
}

func rankEffect(effect string) int {
	switch effect {
	case DecisionDeny:
		return 2
	case DecisionAllow:
		return 1
	default:
		return 0
	}
}
