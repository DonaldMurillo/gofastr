package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// dlqFailingRedis injects failures into LPush calls that target the
// dead-letter list only, leaving the main list healthy.
type dlqFailingRedis struct {
	RedisClient
	dlqErr error
}

func (f *dlqFailingRedis) LPush(ctx context.Context, key string, values ...interface{}) error {
	if f.dlqErr != nil && strings.HasSuffix(key, ":dead") {
		return f.dlqErr
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// An exhausted job whose dead-letter push fails must not vanish: it was
// already popped off the main list, so the failed DLQ write has to
// restore it and surface the error — the same no-silent-loss contract
// the pop→processing transition has.
func TestRedisExhaustedJobSurvivesDLQFailure(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()

	// Attempts already at the cap: the claim-time bump exhausts it.
	poison := Job{ID: "poison", Type: "x", MaxAttempts: 3, Attempts: 3}
	data, _ := json.Marshal(poison)
	_ = r.LPush(ctx, "test", data)

	fail := &dlqFailingRedis{RedisClient: r, dlqErr: errors.New("dlq down")}
	q := NewRedisQueue(fail, "test")

	if _, err := q.Dequeue(ctx); err == nil || errors.Is(err, ErrNoJob) {
		t.Fatalf("Dequeue must surface the DLQ failure, got: %v", err)
	}

	r.mu.Lock()
	mainLen := len(r.lists["test"])
	deadLen := len(r.lists["test:dead"])
	r.mu.Unlock()
	if deadLen != 0 {
		t.Fatalf("dead list has %d entries despite failing pushes", deadLen)
	}
	if mainLen == 0 {
		t.Fatal("exhausted job permanently lost: neither on the main list nor dead-lettered")
	}

	// Once the DLQ heals, the job must complete its journey to dead.
	fail.dlqErr = nil
	if _, err := q.Dequeue(ctx); !errors.Is(err, ErrNoJob) {
		t.Fatalf("healed dequeue should dead-letter and report no job, got: %v", err)
	}
	r.mu.Lock()
	deadLen = len(r.lists["test:dead"])
	r.mu.Unlock()
	if deadLen != 1 {
		t.Fatalf("healed DLQ push should have dead-lettered the job, dead list = %d", deadLen)
	}
}

// pushFailingRedis fails every LPush, whatever list it targets.
type pushFailingRedis struct {
	RedisClient
	err error
}

func (f *pushFailingRedis) LPush(ctx context.Context, key string, values ...interface{}) error {
	if f.err != nil {
		return f.err
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// Nack must not delete the processing entry until the job's next home is
// written. The processing hash is the only durable copy of a claimed job:
// removing it first and then failing the retry (or dead-letter) push leaves
// the job in no list and invisible to Reclaim — silently lost.
func TestRedisNackKeepsJobWhenPushFails(t *testing.T) {
	for _, tc := range []struct {
		name        string
		attempts    int
		maxAttempts int
	}{
		{"retry push fails", 1, 3},
		// Attempts becomes 3 at claim, so Nack dead-letters rather than retries.
		{"dead-letter push fails", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newMockRedis()
			ctx := context.Background()

			job := Job{ID: "j1", Type: "x", MaxAttempts: tc.maxAttempts, Attempts: tc.attempts}
			data, _ := json.Marshal(job)
			_ = r.LPush(ctx, "test", data)

			q := NewRedisQueue(r, "test")
			claimed, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatalf("Dequeue: %v", err)
			}

			fail := &pushFailingRedis{RedisClient: r, err: errors.New("redis down")}
			failing := NewRedisQueue(fail, "test")
			if err := failing.Nack(ctx, claimed); err == nil {
				t.Fatal("Nack must surface the failed push")
			}

			r.mu.Lock()
			mainLen := len(r.lists["test"])
			deadLen := len(r.lists["test:dead"])
			processing := len(r.hashes["test:processing"])
			r.mu.Unlock()

			if mainLen+deadLen+processing == 0 {
				t.Fatal("job permanently lost: not on the main list, not dead-lettered, not reclaimable")
			}
		})
	}
}

// A type-filtered Dequeue pops non-matching jobs off the main list and
// holds them in memory until it restores them. Discarding a restore
// failure left a valid job in NO list while Dequeue reported an ordinary
// empty queue — the same silent-loss class as the Nack ordering bug, in
// the sibling path.
func TestRedisSkippedJobLossIsReported(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()

	job := Job{ID: "a1", Type: "type-a", MaxAttempts: 3}
	data, _ := json.Marshal(job)
	_ = r.LPush(ctx, "test", data)

	fail := &pushFailingRedis{RedisClient: r, err: errors.New("redis down")}
	q := NewRedisQueue(fail, "test")

	// Ask for a type the queued job does not match: it is popped, skipped,
	// and then the restoring push fails.
	_, err := q.Dequeue(ctx, "type-b")
	if err == nil || errors.Is(err, ErrNoJob) {
		t.Fatalf("a failed restore must not be reported as an empty queue, got: %v", err)
	}
	if !strings.Contains(err.Error(), "restore skipped job") {
		t.Errorf("error should name the failed restore, got: %v", err)
	}
}
