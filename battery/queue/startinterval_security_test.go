package queue

import (
	"context"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. RedisQueue.Start's reclaim interval was
// tested `<= 0` and replaced by 30s, so a negative interval (sign error or
// a computed value that went negative) silently bought the SLOWEST
// crash-recovery cadence instead of being refused.
// Surfaces: RedisQueue.Start (the auto-reclaim ticker launch).
// Pins interval <= 0 folding onto the 30s default, found by the
// 2026-09-04 red-probe round; fixed in Start panicking on interval < 0
// (it has no error return) while 0 keeps the default.

func TestRedisStartNegativeIntervalPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("RedisQueue.Start: negative interval silently folded onto the 30s default")
		}
	}()
	q := &RedisQueue{}
	q.Start(context.Background(), -time.Second)
}

func TestRedisStartZeroIntervalKeepsDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &RedisQueue{}
	// Must not panic; the ticker goroutine exits on ctx cancel.
	q.Start(ctx, 0)
}
