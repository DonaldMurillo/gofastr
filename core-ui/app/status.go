package app

import (
	"context"
	"sync/atomic"
)

// statusContextKey carries the render's status recorder.
type statusContextKey struct{}

// TrackStatus returns a context whose render can set the response
// status through [SetStatus], and a func that reads what it set (0 when
// nothing did). The host installs it once per page render, before the
// screen renders, and reads it after, ranking it below a 404-outlet
// outcome and the screen's own ScreenStatusCode.
func TrackStatus(ctx context.Context) (context.Context, func() int) {
	var code atomic.Int32
	return context.WithValue(ctx, statusContextKey{}, &code), func() int { return int(code.Load()) }
}

// SetStatus sets the HTTP status of the page ctx is rendering. It is for
// a component that learns the answer while it draws: a record view
// whose row is missing or hidden draws its not-found state and sets 404,
// so the page says what its body says. The first call wins; a status
// outside 100-999 and a render with no recorder (a component rendered
// outside a page, an island) are ignored.
func SetStatus(ctx context.Context, code int) {
	if code < 100 || code > 999 {
		return
	}
	if rec, ok := ctx.Value(statusContextKey{}).(*atomic.Int32); ok {
		rec.CompareAndSwap(0, int32(code))
	}
}
