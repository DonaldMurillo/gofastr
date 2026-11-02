package queue

import (
	"context"
	"strings"
	"testing"
	"time"
)

// panickyHGetAll is a host RedisClient whose HGetAll panics.
type panickyHGetAll struct{ *mockRedis }

func (panickyHGetAll) HGetAll(context.Context, string) (map[string]string, error) {
	panic("host redis client exploded")
}

// TestRedisReclaimClientPanicContained: the RedisClient is host code, and
// Start runs Reclaim on its own ticker goroutine, which no per-request
// recover net reaches. Enqueue already turned a panicking client into an
// error; Reclaim did not, so one panic on a tick killed the process. It
// is now that tick's error, and the ticker keeps running.
func TestRedisReclaimClientPanicContained(t *testing.T) {
	q := NewRedisQueue(panickyHGetAll{newMockRedis()}, "reclaimpanic")

	_, err := q.Reclaim(t.Context())
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("Reclaim err = %v, want the recovered panic as an error", err)
	}

	// The background ticker survives a panicking tick: with no recover the
	// test binary dies here.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	q.Start(ctx, 5*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
}
