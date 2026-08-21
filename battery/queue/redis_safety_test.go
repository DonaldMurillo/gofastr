package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// opFailingRedis wraps a RedisClient and injects failures into individual
// operations so the crash-safety properties of Dequeue can be exercised at
// exactly the pop→processing transition. It implements RedisClient by
// delegation; only the methods that need to fail are overridden.
type opFailingRedis struct {
	RedisClient
	rpopErr error
	hsetErr error
}

func (f *opFailingRedis) RPop(ctx context.Context, key string) (string, error) {
	if f.rpopErr != nil {
		return "", f.rpopErr
	}
	return f.RedisClient.RPop(ctx, key)
}

func (f *opFailingRedis) HSet(ctx context.Context, key string, values ...any) error {
	if f.hsetErr != nil {
		return f.hsetErr
	}
	return f.RedisClient.HSet(ctx, key, values...)
}

// ----------------------------------------------------------------------------
// Fix 1: the pop→processing transition must not permanently lose a job.
// ----------------------------------------------------------------------------

// TestRedisDequeueRestoresJobOnHSetFailure asserts that when the processing-hash
// write fails after a successful RPop, the claimed job is pushed back onto the
// main queue instead of being dropped into the gap between the two operations.
func TestRedisDequeueRestoresJobOnHSetFailure(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()

	// Seed a job directly so we know its exact payload/ID.
	precious := Job{ID: "precious", Type: "x", MaxAttempts: 3, Attempts: 0}
	data, _ := json.Marshal(precious)
	_ = r.LPush(ctx, "test", data)

	// Wrap the healthy mock so the very first HSet (the processing write) fails.
	fail := &opFailingRedis{RedisClient: r, hsetErr: errors.New("hset down")}
	q := NewRedisQueue(fail, "test")

	if _, err := q.Dequeue(ctx); err == nil {
		t.Fatal("expected Dequeue to surface an error when the processing write fails")
	}

	// The job MUST still exist somewhere reclaimable: it was RPop'd off the main
	// list, so the fix must have restored it. It must NOT be silently lost.
	r.mu.Lock()
	mainLen := len(r.lists["test"])
	procLen := len(r.hashes["test:processing"])
	r.mu.Unlock()

	if mainLen == 0 {
		t.Fatal("job permanently lost: main queue empty and processing empty after HSet failure")
	}
	if procLen != 0 {
		t.Fatalf("job leaked into processing (%d entries) despite HSet failure", procLen)
	}

	// A healthy queue over the same backing store must be able to recover it.
	fail.hsetErr = nil
	q2 := NewRedisQueue(r, "test")
	recovered, err := q2.Dequeue(ctx)
	if err != nil {
		t.Fatalf("restored job was not re-dequeueable: %v", err)
	}
	if recovered.ID != "precious" {
		t.Fatalf("recovered wrong job: %q (expected precious)", recovered.ID)
	}
}

// ----------------------------------------------------------------------------
// Fix 2: a poison message that crashes the worker before Nack must not
// redeliver forever; attempts are bumped at claim, matching DBQueue.
// ----------------------------------------------------------------------------

// TestRedisClaimCrashLoopRespectsMaxAttempts simulates claim→crash→reclaim in a
// tight loop. A poison job (MaxAttempts=3) must be claimed at most 3 times and
// then land on the dead-letter queue, not cycle indefinitely.
func TestRedisClaimCrashLoopRespectsMaxAttempts(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	q.SetVisibilityTimeout(time.Minute)

	// Fake clock: no background goroutine runs (Start is never called), so the
	// single test goroutine is the only reader of q.now, expiry is driven by
	// advancing the clock, not by sleeping.
	now := time.Now()
	q.now = func() time.Time { return now }

	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "poison", Type: "x", MaxAttempts: 3}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	const maxIters = 30
	claims := 0
	for range maxIters {
		job, err := q.Dequeue(ctx)
		if errors.Is(err, ErrNoJob) {
			// Nothing claimable right now: advance past the visibility timeout
			// and reclaim any in-flight entry, then keep cycling.
			now = now.Add(2 * time.Minute)
			_, _ = q.Reclaim(ctx)
			continue
		}
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		if job.ID != "poison" {
			t.Fatalf("unexpected job claimed: %q", job.ID)
		}
		claims++
		// Simulate a crash: never Ack/Nack. Advance the clock past the lease
		// so Reclaim hands the job back to the main queue.
		now = now.Add(2 * time.Minute)
		_, _ = q.Reclaim(ctx)
	}

	if claims > 3 {
		t.Fatalf("poison claimed %d times — redelivers forever (want <= MaxAttempts=3)", claims)
	}

	// The exhausted job must be on the dead-letter queue, not still in flight.
	r.mu.Lock()
	dlq := append([]string(nil), r.lists["test:dead"]...)
	r.mu.Unlock()

	found := false
	for _, raw := range dlq {
		var j Job
		if json.Unmarshal([]byte(raw), &j) == nil && j.ID == "poison" {
			found = true
		}
	}
	if !found {
		t.Fatal("poison never dead-lettered after exhausting MaxAttempts")
	}
}

// ----------------------------------------------------------------------------
// Fix 3: a Redis backend error must surface as an error, not an empty queue.
// ----------------------------------------------------------------------------

// TestRedisDequeueSurfacesBackendError asserts that a real RPop failure (e.g.
// Redis unreachable) is returned as an error, not masked as an empty queue
// (ErrNoJob), which would make an outage indistinguishable from idle.
func TestRedisDequeueSurfacesBackendError(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()
	fail := &opFailingRedis{RedisClient: r, rpopErr: errors.New("connection lost")}
	q := NewRedisQueue(fail, "test")

	_, err := q.Dequeue(ctx)
	if err == nil {
		t.Fatal("expected Dequeue to surface a backend error, got nil")
	}
	if errors.Is(err, ErrNoJob) {
		t.Fatal("backend error masked as empty queue (ErrNoJob) — outage would look like idle")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Fatalf("expected the underlying error to be surfaced, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Fix 4: SetVisibilityTimeout vs Dequeue must be race-free.
// ----------------------------------------------------------------------------

// TestRedisVisTimeoutVsDequeueNoRace hammers SetVisibilityTimeout from one
// goroutine while another dequeues. Under -race the unsynchronized read/write
// of the timeout field is detected; the test fails (race) on the buggy build
// and passes on the fixed one.
func TestRedisVisTimeoutVsDequeueNoRace(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "race")
	ctx := context.Background()

	// Keep the queue non-empty so Dequeue does real work reading the timeout.
	for range 200 {
		_ = q.Enqueue(ctx, Job{ID: "j", Type: "x"})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 2000 {
			q.SetVisibilityTimeout(time.Duration(i+1) * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for range 2000 {
			_, _ = q.Dequeue(ctx)
			// Refill so the loop keeps reading the timeout field.
			_ = q.Enqueue(ctx, Job{ID: "j", Type: "x"})
		}
	}()
	wg.Wait()
}
