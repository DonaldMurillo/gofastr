package queue

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Nack fenced on claim_token alone. With WithDoneRetention, Ack keeps the
// row as status 'done' under the same token, so a late Nack carrying that
// token (a duplicate settle, a worker that Acked and then hit an error on
// its way out) flipped the completed job back to pending and it ran again.
// Every Nack arm now also requires status='claimed', as Ack and release do.
func TestLateNackLeavesDoneJobDone(t *testing.T) {
	dialects := []struct {
		name string
		open func(t *testing.T, opts ...DBQueueOption) (*sql.DB, *DBQueue)
	}{
		{"sqlite", openDBQueueOpts},
		{"postgres", func(t *testing.T, opts ...DBQueueOption) (*sql.DB, *DBQueue) {
			db := pgtest.DB(t)
			q, err := NewDBQueue(db, opts...)
			if err != nil {
				t.Fatalf("new db queue: %v", err)
			}
			return db, q
		}},
	}
	arms := []struct {
		name string
		opts []DBQueueOption
		max  int
	}{
		{"no-backoff requeue", nil, 3},
		{"backoff requeue", []DBQueueOption{WithBackoff(time.Minute, time.Hour)}, 3},
		{"backoff dead-letter", []DBQueueOption{WithBackoff(time.Minute, time.Hour)}, 1},
	}
	for _, dl := range dialects {
		for _, arm := range arms {
			t.Run(dl.name+"/"+arm.name, func(t *testing.T) {
				opts := append([]DBQueueOption{WithWorkers(0), WithDoneRetention(time.Hour)}, arm.opts...)
				db, q := dl.open(t, opts...)
				ctx := context.Background()
				if err := q.Enqueue(ctx, Job{ID: "late-nack", Type: "t", MaxAttempts: arm.max}); err != nil {
					t.Fatalf("enqueue: %v", err)
				}
				job, err := q.Dequeue(ctx, "t")
				if err != nil {
					t.Fatalf("dequeue: %v", err)
				}
				if err := q.Ack(ctx, job); err != nil {
					t.Fatalf("ack: %v", err)
				}
				if err := q.Nack(ctx, job); err != nil {
					t.Fatalf("late nack: %v", err)
				}
				var status string
				if err := db.QueryRowContext(ctx, "SELECT status FROM "+q.qt()+" WHERE id = $1", job.ID).Scan(&status); err != nil {
					t.Fatalf("read status: %v", err)
				}
				if status != "done" {
					t.Fatalf("status after Ack then a late Nack = %q, want done", status)
				}
			})
		}
	}
}
