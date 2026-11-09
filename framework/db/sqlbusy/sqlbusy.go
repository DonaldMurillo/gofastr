// Package sqlbusy retries SQLite's SQLITE_BUSY: a write that meets the
// database-level write lock after the driver's busy_timeout ran out.
// The 2026-11-02 stress run left a queue job `claimed` because one nack
// UPDATE failed this way and the row waited for lease expiry (#656).
// SQLite result codes distinguish contention from unrelated SQL errors.
package sqlbusy

import (
	"context"
	"errors"
	"time"
)

// Is reports whether err is SQLITE_BUSY (or its table-locked cousin).
func Is(err error) bool {
	var coded interface{ Code() int }
	return errors.As(err, &coded) && (coded.Code()&0xff == 5 || coded.Code()&0xff == 6)
}

// Retry preserves the first call's context, then bounds retries after a
// SQLite busy error to two seconds or the caller's earlier deadline.
// It returns the last error or the context error when cancelled.
// A busy database is contention, not corruption: retrying a
// settle write keeps the row out of the slow lease-expiry recovery path,
// and every settle in battery/queue and framework/outbox is fenced on
// its claim, so a late retry that lost the race changes nothing.
func Retry(ctx context.Context, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := fn(ctx)
	if err == nil || !Is(err) {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	delay := 25 * time.Millisecond
	for attempt := 1; attempt < 8; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = fn(ctx); err == nil || !Is(err) {
			return err
		}
		delay = min(delay*2, 500*time.Millisecond)
	}
	return err
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
