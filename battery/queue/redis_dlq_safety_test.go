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
