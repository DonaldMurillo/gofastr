package outbox

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// ============================================================================
// Non-object event data — pinned 2026-09-05 (adversarial pass round 4;
// promoted from that round's red probe once the relay delivered what
// Append stages). The round-4 decision (Q3): the durable lane unmarshals
// into `any`, matching the live bus contract, rather than rejecting
// non-object data at stage time.
// Family: F18 Layer-mismatch parsing (producer layer accepts a shape the
// consumer layer must not reject).
// Property: whatever data Append accepts and stages, the relay must be
// able to deliver — a payload that stages cleanly must not become an
// undeliverable poison row that silently dead-letters MaxAttempts·backoff
// later.
// Surfaces: outbox.go Append (json.Marshal of any `data`), relay.go
// processDelivery (json.Unmarshal into `any`), on every delivery attempt.
// ============================================================================

// TestNonObjectDataReachesConsumer stages an array payload the bus contract allows
// and asserts the consumer receives exactly what was staged.
func TestNonObjectDataReachesConsumer(t *testing.T) {
	db, o := openOutbox(t,
		WithPollInterval(20*time.Millisecond),
		WithHandlerGrace(time.Hour),
		WithMaxAttempts(100), // keep the row retrying so a regression is unambiguous
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []any
	done := make(chan struct{})
	o.Consume("svc", "t.arr", func(_ context.Context, ev event.Event) error {
		mu.Lock()
		got, _ = ev.Data.([]any)
		mu.Unlock()
		close(done)
		return nil
	})

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := o.Append(ctx, tx, "t.arr", []string{"a", "b"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	stop := o.StartRelay(ctx)
	defer stop()
	o.Nudge()

	select {
	case <-done:
		mu.Lock()
		defer mu.Unlock()
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("consumer received %v, want the staged [a b]", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("SECURITY: [outbox] consumer never received a staged non-object payload " +
			"([]string{\"a\",\"b\"}): Append accepted it inside the business tx, but processDelivery " +
			"unmarshals into map[string]any, so the delivery can only fail and eventually dead-letter — " +
			"the staged event is lost on the durable lane while the live bus would have delivered it")
	}
}
