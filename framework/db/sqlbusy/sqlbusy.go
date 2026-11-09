// Package sqlbusy retries SQLite's SQLITE_BUSY: a write that meets the
// database-level write lock after the driver's busy_timeout ran out.
// The 2026-11-02 stress run left a queue job `claimed` because one nack
// UPDATE failed this way and the row waited for lease expiry (#656).
// SQLite result codes distinguish contention from unrelated SQL errors.
package sqlbusy

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// Is reports whether err is SQLITE_BUSY (or its table-locked cousin).
func Is(err error) bool {
	var coded interface{ Code() int }
	return errors.As(err, &coded) && (coded.Code()&0xff == 5 || coded.Code()&0xff == 6)
}

// DefaultBudget bounds the retries of a call whose context carries no
// deadline. The settle paths that use this package all pass a detached
// context with a deadline of their own (ten seconds), so this is the
// ceiling for a host calling Ack or Nack on context.Background.
const DefaultBudget = 10 * time.Second

// maxDelay caps the pause between two attempts. Short, so a settle gets
// many tries at the lock while claims churn around it.
const maxDelay = 100 * time.Millisecond

// Retry runs fn and, while it fails with a SQLite busy error, runs it
// again until it succeeds, fails another way, or ctx ends. The first
// call keeps the caller's context; when that context has no deadline,
// the retries stop after DefaultBudget. On a context that ends first it
// returns the context error wrapped around the last busy error.
//
// The budget is the caller's deadline, never a count of attempts: an
// attempt cap of seven (about two seconds) ran out on a contended file
// while workers kept claiming, and each settle that gave up left its
// row claimed until the lease expired (#669). A failed claim costs
// nothing (the row stays pending), but a failed settle costs a whole
// lease, so a settle keeps trying for as long as its caller allows.
// Every settle in battery/queue, battery/webhook and framework/outbox
// is fenced on its claim, so a late retry that lost the race changes
// nothing.
func Retry(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := fn(ctx)
	if err == nil || !Is(err) {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultBudget)
		defer cancel()
	}
	delay := 5 * time.Millisecond
	for {
		// Jitter keeps two settles that met the same lock from
		// retrying in step and meeting each other again.
		pause := delay/2 + rand.N(delay/2+1)
		t := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-t.C:
		}
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("%w: %w", cerr, err)
		}
		if err = fn(ctx); err == nil || !Is(err) {
			return err
		}
		delay = min(delay*2, maxDelay)
	}
}

// Exec retries a single statement and reports the rows it touched. It
// exists so settle call sites keep their (result, error) shape; a
// caller that ignores the result may use Retry directly.
func Exec(ctx context.Context, exec func(context.Context) (rows int64, err error)) (int64, error) {
	var rows int64
	err := Retry(ctx, func(ctx context.Context) error {
		n, err := exec(ctx)
		if err == nil {
			rows = n
		}
		return err
	})
	return rows, err
}
