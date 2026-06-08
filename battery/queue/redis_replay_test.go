package queue

import (
	"context"
	"encoding/json"
	"testing"
)

// Compile-time check that RedisQueue satisfies the Replayable interface.
var _ Replayable = (*RedisQueue)(nil)

// LRange returns the elements of the list at key in the inclusive range
// [start, stop]. Negative indices count from the end (-1 is the last element).
// It operates on the same backing store mockRedis.LPush/RPop use, where
// index 0 is the head (most recently LPush'd) and the tail is the oldest.
func (m *mockRedis) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.lists[key]
	n := int64(len(l))
	if n == 0 {
		return []string{}, nil
	}
	if start < 0 {
		start += n
	}
	if stop < 0 {
		stop += n
	}
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop {
		return []string{}, nil
	}
	out := make([]string, 0, stop-start+1)
	out = append(out, l[start:stop+1]...)
	return out, nil
}

// LRem removes up to count occurrences of value from the list at key.
// This mock implements the count > 0 case (remove from head toward tail),
// which is all RedisQueue.Replay needs. Returns the number removed.
func (m *mockRedis) LRem(_ context.Context, key string, count int64, value interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var target string
	switch v := value.(type) {
	case string:
		target = v
	case []byte:
		target = string(v)
	default:
		return 0, nil
	}
	l := m.lists[key]
	out := make([]string, 0, len(l))
	var removed int64
	for _, item := range l {
		if item == target && (count <= 0 || removed < count) {
			removed++
			continue
		}
		out = append(out, item)
	}
	m.lists[key] = out
	return removed, nil
}

func TestRedisReplayMovesDLQJobToQueue(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	// Drive a job to the dead-letter queue: MaxAttempts=1 means the first
	// Nack moves it straight to the DLQ.
	_ = q.Enqueue(ctx, Job{ID: "dead1", Type: "email", MaxAttempts: 1})
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if err := q.Nack(ctx, job.ID); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	// Sanity: job is on the dead list, main queue is empty.
	r.mu.Lock()
	if len(r.lists["test:dead"]) != 1 {
		r.mu.Unlock()
		t.Fatalf("expected 1 job on DLQ before replay, got %d", len(r.lists["test:dead"]))
	}
	r.mu.Unlock()

	// Replay it.
	if err := q.Replay(ctx, "dead1"); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// Dead list must now be empty.
	r.mu.Lock()
	if got := len(r.lists["test:dead"]); got != 0 {
		r.mu.Unlock()
		t.Fatalf("expected DLQ empty after replay, got %d", got)
	}
	r.mu.Unlock()

	// The job should be back on the main queue and Dequeue-able with a
	// reset attempts counter.
	replayed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after replay: %v", err)
	}
	if replayed.ID != "dead1" {
		t.Errorf("expected replayed job ID 'dead1', got %q", replayed.ID)
	}
	if replayed.Attempts != 0 {
		t.Errorf("expected Attempts reset to 0 after replay, got %d", replayed.Attempts)
	}
	if replayed.Type != "email" {
		t.Errorf("expected Type 'email' preserved, got %q", replayed.Type)
	}
}

func TestRedisReplayUnknownIsNoop(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	// Put one job on the DLQ that we will NOT replay.
	dead := Job{ID: "stays", Type: "email", MaxAttempts: 3, Attempts: 3}
	data, _ := json.Marshal(dead)
	_ = r.LPush(ctx, "test:dead", data)

	// Replaying an unknown ID is a no-op and returns nil.
	if err := q.Replay(ctx, "unknown-id"); err != nil {
		t.Fatalf("Replay unknown: expected nil error, got %v", err)
	}

	// Nothing changed: DLQ still has the one job, main queue empty.
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := len(r.lists["test:dead"]); got != 1 {
		t.Errorf("expected DLQ unchanged (1 job), got %d", got)
	}
	if got := len(r.lists["test"]); got != 0 {
		t.Errorf("expected main queue empty, got %d", got)
	}
}
