package app

import (
	"context"
	"sync/atomic"
)

type statusContextKey struct{}

// TrackStatus returns a context whose render can set the response status through [SetStatus], and a
// func that reads what it set (0 when nothing did).
func TrackStatus(ctx context.Context) (context.Context, func() int) {
	var code atomic.Int32
	return context.WithValue(ctx, statusContextKey{}, &code), func() int { return int(code.Load()) }
}

// SetStatus sets the HTTP status of the page ctx is rendering.
func SetStatus(ctx context.Context, code int) {
	if code < 100 || code > 999 {
		return
	}
	if rec, ok := ctx.Value(statusContextKey{}).(*atomic.Int32); ok {
		rec.CompareAndSwap(0, int32(code))
	}
}
