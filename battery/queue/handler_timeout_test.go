package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// WithDBHandlerTimeout must cancel a handler's context at the deadline so a
// black-holed dependency can't wedge the (single default) worker forever.
func TestDBQueueHandlerTimeoutCancelsCtx(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	q, err := NewDBQueue(db, WithWorkers(1), WithDBHandlerTimeout(80*time.Millisecond))
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	deadlineHonored := make(chan bool, 1)
	q.RegisterHandler("slow", func(ctx context.Context, _ Job) error {
		select {
		case <-ctx.Done():
			deadlineHonored <- true // cancelled at the timeout
		case <-time.After(3 * time.Second):
			deadlineHonored <- false // handler ran unbounded
		}
		return ctx.Err()
	})

	if err := q.Enqueue(context.Background(), Job{Type: "slow", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Start(ctx)

	select {
	case ok := <-deadlineHonored:
		if !ok {
			t.Fatal("handler ran past the timeout — its context was never cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never observed the timeout")
	}
}
