package framework

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofastr/gofastr/framework/cron"
)

// ============================================================================
// OnStart + OnStop fire in the documented order.
// ============================================================================

func TestLifecycle_HooksRunInOrder(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var log []string
	app.OnStart(func(_ context.Context) error { log = append(log, "start1"); return nil })
	app.OnStart(func(_ context.Context) error { log = append(log, "start2"); return nil })
	app.OnStop(func() error { log = append(log, "stop1"); return nil })
	app.OnStop(func() error { log = append(log, "stop2"); return nil })

	if err := app.runStartHooks(); err != nil {
		t.Fatalf("start hooks: %v", err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Starts in order, stops in reverse order.
	want := []string{"start1", "start2", "stop2", "stop1"}
	if len(log) != len(want) {
		t.Fatalf("log: got %v want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("log[%d]: got %q want %q (full %v)", i, log[i], want[i], log)
		}
	}
}

// ============================================================================
// First failing OnStart aborts — subsequent hooks must not run.
// ============================================================================

func TestLifecycle_StartHookErrorAborts(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	var second atomic.Int32
	boom := errors.New("kaboom")
	app.OnStart(func(_ context.Context) error { return boom })
	app.OnStart(func(_ context.Context) error { second.Add(1); return nil })

	err := app.runStartHooks()
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if second.Load() != 0 {
		t.Fatalf("second hook should not have run")
	}
}

// ============================================================================
// AddCron wires a Scheduler so Stop cancels its goroutine.
// ============================================================================

func TestLifecycle_AddCronStopsScheduler(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	s := cron.NewScheduler()
	if err := s.Register(cron.CronJob{
		Name: "noop",
		Spec: "* * * * *",
		Run:  func(_ context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	app.AddCron(s)

	if err := app.runStartHooks(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Give the scheduler's goroutine a moment to enter its loop before
	// asking it to stop — otherwise Stop races with the goroutine spawn.
	time.Sleep(10 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- app.Stop(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s")
	}
}

// ============================================================================
// AddQueue accepts anything that satisfies schedulerStartStop. We use a
// fake to avoid pulling battery/queue into the framework's tests.
// ============================================================================

type fakeQueue struct {
	started atomic.Bool
	closed  atomic.Bool
}

func (q *fakeQueue) Start(_ context.Context) { q.started.Store(true) }
func (q *fakeQueue) Close() error            { q.closed.Store(true); return nil }

func TestLifecycle_AddQueueStartsAndStops(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	q := &fakeQueue{}
	app.AddQueue(q)

	if err := app.runStartHooks(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !q.started.Load() {
		t.Fatal("queue should have been started")
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !q.closed.Load() {
		t.Fatal("queue should have been closed on Stop")
	}
}

// ============================================================================
// The start context is the one Stop cancels — workers can watch ctx.Done().
// ============================================================================

func TestLifecycle_StartContextCancelledByStop(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	cancelled := make(chan struct{})
	app.OnStart(func(ctx context.Context) error {
		go func() {
			<-ctx.Done()
			close(cancelled)
		}()
		return nil
	})
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("start context not cancelled within 1s of Stop")
	}
}

// ============================================================================
// Stop is idempotent.
// ============================================================================

func TestLifecycle_StopIdempotent(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	if err := app.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second call must not panic and must not return an error.
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
