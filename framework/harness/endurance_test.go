//go:build e2e_real

// Endurance test — drives 1000+ turns through one session with a
// scripted provider and asserts the harness survives without
// goroutine leaks, monotonic event IDs, or bus deadlocks.
//
// Run with:
//
//	go test -tags=e2e_real -run TestEndurance ./framework/harness -v -count=1 -timeout=2m

package harness

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

func TestEndurance_1000TurnsNoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("endurance test skipped in -short mode")
	}

	// Each turn is a single text response; cheap.
	scripts := make([][]provider.StreamEvent, 1100)
	for i := range scripts {
		scripts[i] = []provider.StreamEvent{
			{Kind: provider.KindTextDelta, Text: "ok"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}
	}
	prov := &scriptedProvider{scripts: scripts}
	h, sess, cleanup := plumbingHarnessWithRealTools(t, prov)
	defer cleanup()

	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	gStart := runtime.NumGoroutine()
	var memStart, memEnd runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStart)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const N = 1000
	for i := 0; i < N; i++ {
		// Per-turn ctx so the Subscribe goroutine exits when we're
		// done draining. Without this, 1000 subscriptions × 2
		// goroutines each = 2000 leak (test bug, not harness bug).
		turnCtx, turnCancel := context.WithCancel(ctx)
		sub := c.Subscribe(turnCtx)
		if err := c.Send(turnCtx, control.SendInput{
			SessionID: sess, Content: engine.SimpleInput("ping"),
		}); err != nil {
			turnCancel()
			t.Fatalf("turn %d: send failed: %v", i, err)
		}
		drainUntilTurnEnded(t, sub, 5*time.Second, i)
		turnCancel()
	}

	// Settle, then sample goroutines + memory.
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	runtime.ReadMemStats(&memEnd)
	gEnd := runtime.NumGoroutine()

	// Leak guard: per-turn we subscribe + send. The subscription
	// goroutine should exit when ctx ends; minor slack allowed.
	if gLeak := gEnd - gStart; gLeak > 50 {
		t.Errorf("goroutine leak: started=%d ended=%d (Δ=%d, want < 50)",
			gStart, gEnd, gLeak)
	}
	if delta := int64(memEnd.HeapAlloc) - int64(memStart.HeapAlloc); delta > 64*1024*1024 {
		t.Errorf("heap grew by %d MB over %d turns (>64MB threshold)",
			delta/(1024*1024), N)
	}

	// Monotonic IDs: peek at the bus's next ID — should be >= N
	// events emitted.
	finalID := h.Mux.EngineFor(sess).Bus.NextID()
	if finalID < uint64(N) {
		t.Errorf("event id only at %d after %d turns — IDs not monotonic?", finalID, N)
	}

	t.Logf("endurance ok: %d turns, goroutines Δ=%d, heap Δ=%dKB, next event id=%d",
		N, gEnd-gStart,
		(int64(memEnd.HeapAlloc)-int64(memStart.HeapAlloc))/1024,
		finalID)
}

func drainUntilTurnEnded(t *testing.T, sub <-chan control.EventEnvelope, timeout time.Duration, turn int) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case env := <-sub:
			if env.Kind == "TurnEnded" {
				return
			}
		case <-deadline:
			t.Fatalf("turn %d never ended", turn)
		}
	}
}
