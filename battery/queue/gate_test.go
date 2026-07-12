package queue

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MemoryQueue gate
// ---------------------------------------------------------------------------

func TestMemoryQueueGateDefersJob(t *testing.T) {
	q := NewMemoryQueue(1)
	q.SetGate(func(jobType string) bool { return false }) // gate everything
	var ran atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})
	q.Start()
	t.Cleanup(func() { q.Close() })

	if err := q.Enqueue(context.Background(), Job{
		Type:    "gated",
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if c := ran.Load(); c != 0 {
		t.Fatalf("gated job ran %d times despite gate", c)
	}

	// Re-enable and verify the deferred job runs.
	q.SetGate(func(jobType string) bool { return true })
	time.Sleep(300 * time.Millisecond)
	if c := ran.Load(); c != 1 {
		t.Fatalf("expected 1 run after re-enable, got %d", c)
	}
}

// TestMemQueueGateDeferNoPanicOnClose ensures the AfterFunc callback armed by
// a gate-deferral does not push onto the pending store after Close. Without
// the closed-check under pmu, the timer firing post-Close would race the
// shutdown. The test passing == no panic.
func TestMemQueueGateDeferNoPanicOnClose(t *testing.T) {
	q := NewMemoryQueue(1)
	q.SetGate(func(jobType string) bool { return false }) // gate everything
	var ran atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})
	q.Start()

	if err := q.Enqueue(context.Background(), Job{
		Type:    "gated",
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Sleep past gateDeferDelay so the AfterFunc is armed (and re-arms on
	// each failed re-defer). Close() while a timer is in flight, then wait
	// again: a callback firing post-Close must not send on the closed chan.
	time.Sleep(150 * time.Millisecond)
	_ = q.Close()
	time.Sleep(150 * time.Millisecond)

	if c := ran.Load(); c != 0 {
		t.Fatalf("gated job ran %d times despite gate", c)
	}
}

// ---------------------------------------------------------------------------
// DBQueue gate
// ---------------------------------------------------------------------------

func TestDBQueueGateDefersJob(t *testing.T) {
	_, q := openDBQueue(t, 1)
	q.SetGate(func(jobType string) bool { return false })

	var ran atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})

	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{Type: "gated", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	q.Start(ctx)
	t.Cleanup(func() { q.Close() })

	time.Sleep(200 * time.Millisecond)
	if c := ran.Load(); c != 0 {
		t.Fatalf("gated job ran %d times despite gate", c)
	}

	// Re-enable and verify the deferred job runs.
	q.SetGate(func(jobType string) bool { return true })
	time.Sleep(500 * time.Millisecond)
	if c := ran.Load(); c != 1 {
		t.Fatalf("expected 1 run after re-enable, got %d", c)
	}
}

func TestDBQueueGateNoHotLoop(t *testing.T) {
	_, q := openDBQueue(t, 1)
	q.SetGate(func(jobType string) bool { return false })

	var ran atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})

	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{
		Type:        "gated",
		Payload:     json.RawMessage(`{}`),
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	q.Start(ctx)
	t.Cleanup(func() { q.Close() })

	time.Sleep(300 * time.Millisecond)

	// The job must NOT have been dead-lettered (attempts must not have
	// been consumed by gate-failures). Verify by checking it's still
	// eligible (pending, attempts < max).
	jobs, err := q.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("job disappeared — gate should defer, not drop")
	}
	for _, j := range jobs {
		if j.Attempts > 1 {
			t.Fatalf("attempts=%d after 300ms of gating — hot loop is consuming retries", j.Attempts)
		}
	}
}

// TestDBQueueGateNoClaimChurn asserts the worker never claims gated jobs at
// all. Before M3 the loop claimed every gated job then released it every
// ~100ms — DB churn. With eligibleTypes() filtering gated types out before
// Dequeue, gated rows stay untouched: attempts never bump and scheduled_at
// is never pushed forward by release().
func TestDBQueueGateNoClaimChurn(t *testing.T) {
	_, q := openDBQueue(t, 1)
	ctx := context.Background()

	var ran atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})

	// Gate everything closed.
	q.SetGate(func(jobType string) bool { return false })

	// A fixed, distinctive scheduled_at. The buggy claim→release path calls
	// release(), which pushes scheduled_at ~100ms into the future; the
	// equality check below catches that. With eligibleTypes() filtering,
	// the value stays exactly as enqueued.
	sched := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(ctx, Job{
			Type:        "gated",
			Payload:     json.RawMessage(`{}`),
			ScheduledAt: sched,
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	q.Start(ctx)
	t.Cleanup(func() { q.Close() })

	// Several claim/release cycles (~100ms apart) would have fired here.
	time.Sleep(300 * time.Millisecond)

	jobs, err := q.ListJobs(ctx, "", 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(jobs))
	}
	for i, j := range jobs {
		if j.Attempts != 0 {
			t.Fatalf("job %d attempts=%d — gated job was claimed (churn)", i, j.Attempts)
		}
		if !j.ScheduledAt.Equal(sched) {
			t.Fatalf("job %d scheduled_at=%v moved from %v — release churn fired", i, j.ScheduledAt, sched)
		}
	}

	// Re-open the gate and confirm all three deferred jobs now run.
	q.SetGate(func(jobType string) bool { return true })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && ran.Load() < 3 {
		time.Sleep(20 * time.Millisecond)
	}
	if c := ran.Load(); c != 3 {
		t.Fatalf("expected 3 runs after re-enable, got %d", c)
	}
}

// TestDBQueueGateConcurrentSetGate hammers SetGate from a goroutine while the
// worker loop reads the gate. Under -race the unprotected q.gate read in
// workerLoop (pre-M4) is flagged as a data race. Reading under q.mu.RLock
// makes the access safe.
func TestDBQueueGateConcurrentSetGate(t *testing.T) {
	_, q := openDBQueue(t, 1)
	ctx := context.Background()

	var ran atomic.Int32
	q.RegisterHandler("flap", func(_ context.Context, _ Job) error {
		ran.Add(1)
		return nil
	})

	q.Start(ctx)
	t.Cleanup(func() { q.Close() })

	// Hammer SetGate while the worker loop reads the gate on every cycle.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				q.SetGate(func(jobType string) bool { return true })
				q.SetGate(func(jobType string) bool { return false })
				time.Sleep(time.Millisecond) // yield: don't starve the worker's RLock
			}
		}
	}()

	for i := 0; i < 10; i++ {
		if err := q.Enqueue(ctx, Job{Type: "flap", Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)

	// Settle the gate open and let any deferred jobs drain. This also
	// exercises the gate read one final time; the -race detector is the
	// real assertion.
	q.SetGate(func(jobType string) bool { return true })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && ran.Load() < 10 {
		time.Sleep(20 * time.Millisecond)
	}
	if c := ran.Load(); c != 10 {
		t.Fatalf("expected 10 runs after settling gate, got %d", c)
	}
}

func TestGateDeferNoStrand(t *testing.T) {
	q := NewMemoryQueue(1)
	var enabled atomic.Bool
	q.SetGate(func(jobType string) bool { return enabled.Load() })
	var gatedRan atomic.Int32
	q.RegisterHandler("gated", func(_ context.Context, _ Job) error {
		gatedRan.Add(1)
		return nil
	})
	q.RegisterHandler("filler", func(_ context.Context, _ Job) error { return nil })

	// Build a backlog of filler jobs before any worker runs, then gate-defer
	// a job so its re-enqueue timer fires while filler jobs are still
	// pending. With the priority-heap store (unbounded) the deferred push
	// always succeeds; this test guards against any regression that would
	// strand the deferred job behind the backlog.
	for i := 0; i < 1024; i++ {
		if err := q.Enqueue(context.Background(), Job{Type: "filler", Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	q.processJob(Job{Type: "gated", Payload: json.RawMessage(`{}`)})
	time.Sleep(3 * gateDeferDelay) // timer fires while backlog is pending

	enabled.Store(true)
	q.Start()
	t.Cleanup(func() { q.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for gatedRan.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if gatedRan.Load() == 0 {
		t.Fatal("gate-deferred job stranded after re-enable behind a backlog")
	}
}
