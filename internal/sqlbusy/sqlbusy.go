// Package sqlbusy retries SQLite's SQLITE_BUSY: a write that meets the
// database-level write lock after the driver's busy_timeout ran out.
// The 2026-11-02 stress run left a queue job `claimed` because one nack
// UPDATE failed this way and the row waited for lease expiry (#656).
// Both bundled SQLite drivers spell the error in text ("database is
// locked", optionally with "(5) (SQLITE_BUSY)"), with no typed error to
// errors.Is, so the match is on the message.
package sqlbusy

import (
	"context"
	"strings"
	"time"
)

// Is reports whether err is SQLITE_BUSY (or its table-locked cousin).
func Is(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy")
}

// Retry runs fn until it returns nil, a non-busy error, or the budget
// runs out (about 2s across the backoff schedule below); the last error
// is returned. A busy database is contention, not corruption: retrying a
// settle write keeps the row out of the slow lease-expiry recovery path,
// and every settle in battery/queue and framework/outbox is fenced on
// its claim, so a late retry that lost the race changes nothing.
func Retry(ctx context.Context, fn func(context.Context) error) error {
	var err error
	delay := 25 * time.Millisecond
	for attempt := 0; attempt < 8; attempt++ {
		if err = fn(ctx); err == nil || !Is(err) {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		if delay < 500*time.Millisecond {
			delay *= 2
		}
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
