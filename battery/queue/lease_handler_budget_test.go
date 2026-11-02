package queue

import (
	"bytes"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A handler budget (WithDBHandlerTimeout) no shorter than the claim lease
// (WithLeaseTimeout, five minutes by default) lets a handler still inside
// its budget outlive its claim: the next poll re-claims the job and runs
// it again concurrently, the first run's settle is fenced out, and on a
// final attempt the dead-letter sweep fails a job that is about to
// succeed. The outbox relay had the same pair (e5616f008). Each option is
// valid alone, so neither one's own check can see it: NewDBQueue refuses
// the pairing, and SetLeaseTimeout keeps the lease it had.
func TestHandlerBudgetMustFitInsideLease(t *testing.T) {
	open := func(opts ...DBQueueOption) (*DBQueue, error) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		return NewDBQueue(db, opts...)
	}
	for _, tc := range []struct {
		name string
		opts []DBQueueOption
	}{
		{"budget past the default lease", []DBQueueOption{WithDBHandlerTimeout(10 * time.Minute)}},
		{"budget equal to the lease", []DBQueueOption{WithLeaseTimeout(time.Minute), WithDBHandlerTimeout(time.Minute)}},
		{"lease set after the budget", []DBQueueOption{WithDBHandlerTimeout(2 * time.Minute), WithLeaseTimeout(time.Minute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := open(tc.opts...)
			if err == nil {
				t.Fatalf("NewDBQueue accepted a handler budget no shorter than the lease (lease %s, budget %s)", q.lease, q.handlerTimeout)
			}
			if !strings.Contains(err.Error(), "WithLeaseTimeout") {
				t.Errorf("refusal %q does not name the option that fixes it", err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		opts []DBQueueOption
	}{
		{"budget inside the lease", []DBQueueOption{WithLeaseTimeout(2 * time.Minute), WithDBHandlerTimeout(30 * time.Second)}},
		{"no budget", []DBQueueOption{WithLeaseTimeout(time.Second)}},
		{"defaults", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := open(tc.opts...); err != nil {
				t.Fatalf("NewDBQueue refused a lease that covers the handler budget: %v", err)
			}
		})
	}

	t.Run("setter keeps the lease", func(t *testing.T) {
		var logs bytes.Buffer
		q, err := open(WithDBHandlerTimeout(time.Minute),
			WithDBLogger(slog.New(slog.NewTextHandler(&logs, nil))))
		if err != nil {
			t.Fatal(err)
		}
		q.SetLeaseTimeout(30 * time.Second)
		if got := q.leaseTimeout(); got != 5*time.Minute {
			t.Errorf("SetLeaseTimeout(30s) under a 1m handler budget left the lease at %s, want the 5m it had", got)
		}
		if !strings.Contains(logs.String(), "lease") {
			t.Errorf("the refused lease was not logged: %q", logs.String())
		}
		q.SetLeaseTimeout(90 * time.Second)
		if got := q.leaseTimeout(); got != 90*time.Second {
			t.Errorf("SetLeaseTimeout(90s) over a 1m budget: lease %s, want 90s", got)
		}
	})
}
