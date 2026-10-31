package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A shutdown that lands while a handler finishes must not drop the
// outcome: the Ack ran on the worker's own context, failed once it was
// cancelled, and the finished job ran again after the lease expired
// (2026-10-27 bug bash, settlectx). The same goes for the Nack.
func TestSettleSurvivesShutdown(t *testing.T) {
	for name, outcome := range map[string]error{"ack": nil, "nack": errors.New("boom")} {
		t.Run(name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { db.Close() })

			q, err := NewDBQueue(db, WithWorkers(1))
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ran := make(chan struct{})
			q.RegisterHandler("work", func(context.Context, Job) error {
				cancel() // shutdown arrives as the work finishes
				close(ran)
				return outcome
			})
			if err := q.Enqueue(context.Background(), Job{Type: "work", Payload: json.RawMessage(`{}`), MaxAttempts: 3}); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			q.Start(ctx)
			select {
			case <-ran:
			case <-time.After(5 * time.Second):
				t.Fatal("handler never ran")
			}
			select {
			case <-q.stopped: // every worker has returned
			case <-time.After(5 * time.Second):
				t.Fatal("workers never stopped")
			}

			var claimed int
			row := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = 'claimed'`, q.qt()))
			if err := row.Scan(&claimed); err != nil {
				t.Fatalf("count: %v", err)
			}
			if claimed != 0 {
				t.Fatalf("job left claimed after shutdown: the %s was dropped and the work will run again", name)
			}
		})
	}
}
