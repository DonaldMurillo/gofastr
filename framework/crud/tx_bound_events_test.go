package crud

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// subscribeCreated collects EntityCreated events from a fresh bus on ch.
func subscribeCreated(t *testing.T, ch *CrudHandler) <-chan event.Event {
	t.Helper()
	bus := event.NewEventBus()
	ch.Events = bus
	got := make(chan event.Event, 8)
	cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
		got <- ev
		return nil
	})
	t.Cleanup(cancel)
	return got
}

// A handler bound to a caller's *sql.Tx announced its write as soon as
// the operation succeeded, before the caller committed and even when it
// rolled back (2026-10-27 bug bash D35). Nothing tells such a handler how
// the transaction ends, so with no commit queue it announces nothing.
func TestTxBoundRollbackAnnouncesNothing(t *testing.T) {
	ch, conn := covNotesHandler(t)
	got := subscribeCreated(t, ch)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	bound := *ch
	bound.DB = tx
	if _, err := bound.CreateOne(context.Background(), map[string]any{"title": "phantom"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		t.Fatalf("rolled-back write on a tx-bound handler announced %v", ev.Data)
	case <-time.After(500 * time.Millisecond):
	}
}

// With the transaction's commit queue on the context, the bound handler's
// event waits for the owner to drain it after Commit.
func TestTxBoundQueueEmitsAfterCommit(t *testing.T) {
	ch, conn := covNotesHandler(t)
	got := subscribeCreated(t, ch)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, queue := db.WithTxQueue(context.Background(), tx)
	bound := *ch
	bound.DB = tx
	if _, err := bound.CreateOne(ctx, map[string]any{"title": "kept"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-got:
		t.Fatalf("announced before commit: %v", ev.Data)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	queue.RunAfterCommit()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no EntityCreated after the queue drained")
	}
}

// syncBuffer is a log sink safe to read while the probe goroutine writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The ambient-transaction probe checked its two-minute limit only after
// its SELECT 1 returned, so a statement holding the transaction's
// connection held the probe past the limit (2026-10-27 bug bash D38). The
// limit now bounds the wait itself: the event is dropped on time while the
// statement still runs.
func TestAmbientProbeDropsOnDeadline(t *testing.T) {
	prev := ambientTxProbeTimeout
	ambientTxProbeTimeout = 150 * time.Millisecond
	t.Cleanup(func() { ambientTxProbeTimeout = prev })
	var logs syncBuffer
	prevOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	ch, conn := covNotesHandler(t)
	got := subscribeCreated(t, ch)
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx := db.WithTx(context.Background(), tx)
	if _, err := ch.CreateOne(ctx, map[string]any{"title": "slow"}); err != nil {
		t.Fatal(err)
	}
	// The owner's long statement holds the transaction's connection.
	busy := make(chan struct{})
	go func() {
		defer close(busy)
		var n int
		_ = tx.QueryRow(`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 30000000) SELECT count(*) FROM c`).Scan(&n)
	}()
	deadline := time.After(time.Second)
	for !strings.Contains(logs.String(), "still open after") {
		select {
		case <-busy:
			t.Fatal("the long statement finished first; the test proves nothing")
		case <-deadline:
			t.Fatalf("probe did not give up within 1s; log: %q", logs.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case ev := <-got:
		t.Fatalf("event published for an unresolved transaction: %v", ev.Data)
	default:
	}
	<-busy
}

// The commit check on the base pool ran with no deadline: a pool whose
// connections were all busy held it forever. It now gives up on the ctx.
func TestLandedHonoursDeadline(t *testing.T) {
	_, conn := covNotesHandler(t)
	hold, err := conn.Begin() // the pool's only connection
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := landed(ctx, conn, event.EntityCreated, "notes", []string{"id"}, []any{"x"}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("landed succeeded with no free connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("landed ignored its deadline")
	}
}
