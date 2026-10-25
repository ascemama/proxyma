package policy

import (
	"context"
	"sync"
)

// MutableEvaluator allows policy CRUD to refresh the active evaluator without restarting Proxyma.
type MutableEvaluator struct {
	mu        sync.RWMutex
	evaluator Evaluator
}

// NewMutableEvaluator returns a thread-safe evaluator wrapper.
func NewMutableEvaluator(evaluator Evaluator) *MutableEvaluator {
	return &MutableEvaluator{evaluator: evaluator}
}

// Set replaces the active policy evaluator.
func (m *MutableEvaluator) Set(evaluator Evaluator) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evaluator = evaluator
}

// Evaluate delegates to the active evaluator.
func (m *MutableEvaluator) Evaluate(ctx context.Context, request Request) (Result, error) {
	if m == nil {
		return Result{Decision: DecisionDeny, Reason: "target policy evaluator is not configured"}, nil
	}
	m.mu.RLock()
	evaluator := m.evaluator
	m.mu.RUnlock()
	if evaluator == nil {
		return Result{Decision: DecisionDeny, Reason: "target policy evaluator is not configured"}, nil
	}
	return evaluator.Evaluate(ctx, request)
}

// Find returns a copy of a policy from the active policy set when available.
// It is intentionally read-only so API callers cannot mutate the evaluator.
func (m *MutableEvaluator) Find(policyID string) (Policy, bool) {
	if m == nil {
		return Policy{}, false
	}
	m.mu.RLock()
	evaluator := m.evaluator
	m.mu.RUnlock()
	lookup, ok := evaluator.(*Set)
	if !ok {
		return Policy{}, false
	}
	return lookup.Find(policyID)
}
