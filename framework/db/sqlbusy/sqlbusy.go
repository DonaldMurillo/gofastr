// Package sqlbusy retries a SQLite write that met SQLITE_BUSY until it lands or its context ends: Retry, Exec.
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

// DefaultBudget bounds the retries of a call whose context has no deadline.
const DefaultBudget = 10 * time.Second

const maxDelay = 100 * time.Millisecond

// Retry reruns fn while it fails busy, until it lands, fails otherwise, or ctx ends (its error wraps the busy one).
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

// Exec is Retry for a statement, returning the rows the attempt that landed touched.
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
