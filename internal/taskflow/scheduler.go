package taskflow

import (
	"context"
	"sync/atomic"
	"time"
)

const defaultSchedulerInterval = time.Second

// Scheduler runs Proxyma task lifecycle checks from one in-process loop.
type Scheduler struct {
	controller LifecycleController
	interval   time.Duration
	running    atomic.Bool
}

// LifecycleController processes Proxyma task lifecycle transitions.
type LifecycleController interface {
	StartCreated(ctx context.Context) (int, error)
	ManageState(ctx context.Context) (int, error)
}

// SchedulerResult summarizes one scheduler pass.
type SchedulerResult struct {
	StartedCreated int
	ManagedState   int
	Skipped        bool
}

// NewScheduler creates a single-process scheduler for Proxyma taskflow passes.
func NewScheduler(controller LifecycleController, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = defaultSchedulerInterval
	}
	return &Scheduler{controller: controller, interval: interval}
}

// Start runs the scheduler loop until ctx is canceled.
func (s *Scheduler) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.RunOnce(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.RunOnce(ctx)
			}
		}
	}()
	return done
}

// RunOnce processes created tasks first, then applies pending lifecycle state changes.
func (s *Scheduler) RunOnce(ctx context.Context) (SchedulerResult, error) {
	if s == nil || s.controller == nil {
		return SchedulerResult{}, nil
	}
	if !s.running.CompareAndSwap(false, true) {
		return SchedulerResult{Skipped: true}, nil
	}
	defer s.running.Store(false)

	started, err := s.controller.StartCreated(ctx)
	if err != nil {
		return SchedulerResult{StartedCreated: started}, err
	}
	managed, err := s.controller.ManageState(ctx)
	if err != nil {
		return SchedulerResult{StartedCreated: started, ManagedState: managed}, err
	}
	return SchedulerResult{StartedCreated: started, ManagedState: managed}, nil
}
