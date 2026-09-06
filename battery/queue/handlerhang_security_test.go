package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ============================================================================
// Hung handler cannot stall the queue — pinned 2026-09-05 (adversarial
// pass round 4; promoted from that round's red probe once both worker
// surfaces ran handlers on their own goroutine with a stop-waiting
// deadline).
// Family: F20 Transaction and side-effect ordering (handler timeouts /
// lane availability).
// Property: the documented handler-timeout contract — "a black-holed
// dependency ... can't wedge a worker forever, critical with the default
// single worker, where one stuck job stalls the entire queue" — holds for
// a handler that IGNORES its cancelled context, not only for a
// cooperative one. A duplicate side effect on the abandoned invocation's
// late completion is the documented at-least-once cost.
// Surfaces: db.go DBQueue.runHandler and memory.go MemoryQueue's
// awaitHandler, both reached from every worker in the pool for every
// claimed job. Mirrors the outbox relay's runHandler, pinned there by
// TestHungConsumerCannotStallSibling.
// ============================================================================

// TestHungHandlerCannotStallQueue hangs one handler past its cancelled context and
// asserts an unrelated job still completes. Both worker surfaces are looped.
func TestHungHandlerCannotStallQueue(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T)
	}{
		{"dbqueue", testHungHandlerDBQueue},
		{"memoryqueue", testHungHandlerMemoryQueue},
	} {
		t.Run(tc.name, tc.run)
	}
}

func testHungHandlerDBQueue(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	q, err := NewDBQueue(db, WithWorkers(1), WithDBHandlerTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = q.Close()
	})
	q.Start(ctx)
	driveHungHandlerScenario(t, q.Enqueue, q.RegisterHandler)
}

func testHungHandlerMemoryQueue(t *testing.T) {
	q := NewMemoryQueue(1, WithHandlerTimeout(100*time.Millisecond))
	t.Cleanup(func() { _ = q.Close() })
	q.Start()
	driveHungHandlerScenario(t, q.Enqueue, q.RegisterHandler)
}

func driveHungHandlerScenario(t *testing.T, enqueue func(context.Context, Job) error, register func(string, Handler)) {
	t.Helper()
	enter := make(chan struct{})
	entered := sync.OnceFunc(func() { close(enter) })
	release := make(chan struct{})
	okRan := make(chan struct{}, 1)
	ctx := context.Background()

	register("stuck", func(context.Context, Job) error {
		entered()
		<-release // hangs past its cancelled context: the timeout cannot make it return
		return nil
	})
	register("ok", func(context.Context, Job) error {
		okRan <- struct{}{}
		return nil
	})

	if err := enqueue(ctx, Job{Type: "stuck", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("enqueue stuck: %v", err)
	}
	select {
	case <-enter:
	case <-time.After(3 * time.Second):
		t.Fatalf("setup: stuck handler never started; cannot assert the hang case")
	}
	defer close(release)

	if err := enqueue(ctx, Job{Type: "ok", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("enqueue ok: %v", err)
	}
	select {
	case <-okRan:
		// The handler budget freed the worker despite the hang: contract holds.
	case <-time.After(3 * time.Second):
		t.Fatalf("SECURITY: [queue] an unrelated job was never processed while a handler that ignores its " +
			"cancelled context hung for 3s (timeout 100ms): runHandler/awaitHandler invoke the handler inline, " +
			"so one non-cooperative handler wedges the default single-worker queue forever — the outbox relay " +
			"stops waiting at the deadline (relay.go runHandler), the queue must too")
	}
}
