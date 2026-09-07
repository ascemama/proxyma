package taskflow

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type fakeLifecycleController struct {
	started int
	managed int
	calls   []string
}

func (c *fakeLifecycleController) StartCreated(_ context.Context) (int, error) {
	c.calls = append(c.calls, "StartCreated")
	return c.started, nil
}

func (c *fakeLifecycleController) ManageState(_ context.Context) (int, error) {
	c.calls = append(c.calls, "ManageState")
	return c.managed, nil
}

func TestSchedulerRunOnceCallsLifecycleControllerInOrder(t *testing.T) {
	controller := &fakeLifecycleController{started: 2, managed: 1}
	scheduler := NewScheduler(controller, time.Millisecond)

	result, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StartedCreated != 2 || result.ManagedState != 1 || result.Skipped {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(controller.calls, []string{"StartCreated", "ManageState"}) {
		t.Fatalf("calls = %#v", controller.calls)
	}
}

func TestSchedulerSkipsOverlappingRun(t *testing.T) {
	controller := &fakeLifecycleController{}
	scheduler := NewScheduler(controller, time.Millisecond)
	scheduler.running.Store(true)

	result, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped {
		t.Fatalf("result = %#v", result)
	}
	if len(controller.calls) != 0 {
		t.Fatalf("calls = %#v", controller.calls)
	}
}

func TestSchedulerRunOnceNoopsWithoutController(t *testing.T) {
	var nilScheduler *Scheduler
	if result, err := nilScheduler.RunOnce(context.Background()); err != nil || result != (SchedulerResult{}) {
		t.Fatalf("nil scheduler result = %#v err=%v", result, err)
	}

	scheduler := NewScheduler(nil, time.Millisecond)
	if result, err := scheduler.RunOnce(context.Background()); err != nil || result != (SchedulerResult{}) {
		t.Fatalf("nil controller result = %#v err=%v", result, err)
	}
}
