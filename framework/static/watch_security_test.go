package static

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. Watch tested interval `<= 0` and
// substituted 500ms, so a negative interval (sign or unit error)
// silently became a fast poll loop instead of being refused.
// Surfaces: Builder.Watch (the polling watch loop).
// Pins interval <= 0 folding onto the 500ms default, found by the
// 2026-09-04 red-probe round; fixed in Watch returning an error for
// interval < 0 while 0 keeps the default.

func TestWatchNegativeIntervalRejected(t *testing.T) {
	b := &Builder{}
	// Short-lived ctx: with the fold in place Watch would enter the poll
	// loop and only leave via ctx, so the deadline turns a regression
	// into a fast nil-return failure instead of a package timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := b.Watch(ctx, nil, -time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatalf("Watch: negative interval silently folded onto the 500ms default: %v", err)
	}
}

func TestWatchZeroIntervalKeepsDefault(t *testing.T) {
	b := &Builder{}
	// Cancel up front: the initial Build fails on the zero Builder (no
	// Host), onError absorbs it, and the loop exits on ctx.Done. Reaching
	// the clean nil return proves validation accepted 0 and the ticker
	// was constructed with the 500ms default.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Watch(ctx, nil, 0, func(error) {}); err != nil {
		t.Fatalf("zero interval must keep the 500ms default and exit cleanly on cancel: %v", err)
	}
}
