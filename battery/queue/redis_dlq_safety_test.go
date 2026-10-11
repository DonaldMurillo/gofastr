package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// dlqFailingRedis injects failures into LPush calls that target the
// dead-letter list only, leaving the main list healthy.
type dlqFailingRedis struct {
	RedisClient
	dlqErr error
}

func (f *dlqFailingRedis) LPush(ctx context.Context, key string, values ...any) error {
	if f.dlqErr != nil && strings.HasSuffix(key, ":dead") {
		return f.dlqErr
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// An exhausted job whose dead-letter push fails must not vanish: it was
// already popped off the main list, so the failed DLQ write has to
// restore it and surface the error, the same no-silent-loss contract
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

// A malformed job has no processing record after RPop, so a failed
// quarantine write must restore it to the main list and surface the error.
func TestRedisMalformedJobSurvivesDLQFailure(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()
	_ = r.LPush(ctx, "test", "{malformed")

	fail := &dlqFailingRedis{RedisClient: r, dlqErr: errors.New("dlq down")}
	q := NewRedisQueue(fail, "test")
	if _, err := q.Dequeue(ctx); err == nil || errors.Is(err, ErrNoJob) {
		t.Fatalf("Dequeue must surface the quarantine failure, got: %v", err)
	}

	r.mu.Lock()
	mainLen := len(r.lists["test"])
	deadLen := len(r.lists["test:dead"])
	r.mu.Unlock()
	if deadLen != 0 {
		t.Fatalf("dead list has %d entries despite failing pushes", deadLen)
	}
	if mainLen != 1 {
		t.Fatalf("malformed job should be restored while quarantine is unavailable, main list = %d", mainLen)
	}

	fail.dlqErr = nil
	if _, err := q.Dequeue(ctx); err == nil {
		t.Fatal("healed dequeue should still report malformed JSON")
	}
	r.mu.Lock()
	deadLen = len(r.lists["test:dead"])
	mainLen = len(r.lists["test"])
	r.mu.Unlock()
	if deadLen != 1 || mainLen != 0 {
		t.Fatalf("healed quarantine should move malformed job to DLQ (dead=%d main=%d)", deadLen, mainLen)
	}
}

type cancelAfterPopRedis struct {
	RedisClient
	cancel context.CancelFunc
}

func (f *cancelAfterPopRedis) RPop(ctx context.Context, key string) (string, error) {
	data, err := f.RedisClient.RPop(ctx, key)
	if err == nil {
		f.cancel()
	}
	return data, err
}

func (f *cancelAfterPopRedis) HSet(ctx context.Context, key string, values ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.RedisClient.HSet(ctx, key, values...)
}

func (f *cancelAfterPopRedis) LPush(ctx context.Context, key string, values ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// Once RPop succeeds, caller cancellation cannot be allowed to cancel the
// rollback write if recording the processing lease then fails.
func TestRedisCanceledDequeueRestoresPoppedJob(t *testing.T) {
	r := newMockRedis()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := Job{ID: "claimed", Type: "email", MaxAttempts: 3}
	data, _ := json.Marshal(job)
	_ = r.LPush(context.Background(), "test", data)

	client := &cancelAfterPopRedis{RedisClient: r, cancel: cancel}
	q := NewRedisQueue(client, "test")
	if _, err := q.Dequeue(ctx); err == nil {
		t.Fatal("Dequeue should report the canceled claim write")
	}

	r.mu.Lock()
	mainLen := len(r.lists["test"])
	processingLen := len(r.hashes["test:processing"])
	r.mu.Unlock()
	if mainLen != 1 || processingLen != 0 {
		t.Fatalf("popped job must be restored (main=%d processing=%d)", mainLen, processingLen)
	}
}

// pushFailingRedis fails every LPush, whatever list it targets.
type pushFailingRedis struct {
	RedisClient
	err error
}

func (f *pushFailingRedis) LPush(ctx context.Context, key string, values ...any) error {
	if f.err != nil {
		return f.err
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// Nack must not delete the processing entry until the job's next home is
// written. The processing hash is the only durable copy of a claimed job:
// removing it first and then failing the retry (or dead-letter) push leaves
// the job in no list and invisible to Reclaim, silently lost.
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
// empty queue, the same silent-loss class as the Nack ordering bug, in
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

// slowRecoveryRedis stretches RPop past the recovery deadline, fails the
// processing write, and can make dead-letter pushes hang until their
// context ends. Every LPush refuses a dead context, as a real client does.
type slowRecoveryRedis struct {
	RedisClient
	popDelay time.Duration
	failHSet bool
	hangDead bool
}

func (f *slowRecoveryRedis) RPop(ctx context.Context, key string) (string, error) {
	data, err := f.RedisClient.RPop(ctx, key)
	if err == nil {
		time.Sleep(f.popDelay)
	}
	return data, err
}

func (f *slowRecoveryRedis) HSet(ctx context.Context, key string, values ...any) error {
	if f.failHSet {
		return errors.New("processing write failed")
	}
	return f.RedisClient.HSet(ctx, key, values...)
}

func (f *slowRecoveryRedis) LPush(ctx context.Context, key string, values ...any) error {
	if f.hangDead && strings.HasSuffix(key, ":dead") {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

func shrinkRecoveryTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := redisRecoveryTimeout
	redisRecoveryTimeout = d
	t.Cleanup(func() { redisRecoveryTimeout = prev })
}

// The recovery deadline starts when a recovery write is needed, not before
// RPop: an RPop that outlasts it must still leave the restore a live context.
func TestRedisSlowPopStillRestoresJob(t *testing.T) {
	shrinkRecoveryTimeout(t, 50*time.Millisecond)
	r := newMockRedis()
	data, _ := json.Marshal(Job{ID: "slow", Type: "email", MaxAttempts: 3})
	_ = r.LPush(context.Background(), "test", data)

	q := NewRedisQueue(&slowRecoveryRedis{RedisClient: r, popDelay: 100 * time.Millisecond, failHSet: true}, "test")
	_, err := q.Dequeue(context.Background())
	if err == nil || strings.Contains(err.Error(), "restore popped job") {
		t.Fatalf("Dequeue should report only the processing write failure, got: %v", err)
	}
	r.mu.Lock()
	mainLen := len(r.lists["test"])
	r.mu.Unlock()
	if mainLen != 1 {
		t.Fatalf("popped job lost after a slow RPop (main=%d)", mainLen)
	}
}

// A quarantine write that spends its whole deadline must not spend the
// rollback's: the restore behind it gets a deadline of its own.
func TestRedisHungQuarantineStillRestoresJob(t *testing.T) {
	shrinkRecoveryTimeout(t, 50*time.Millisecond)
	r := newMockRedis()
	_ = r.LPush(context.Background(), "test", "{malformed")

	q := NewRedisQueue(&slowRecoveryRedis{RedisClient: r, hangDead: true}, "test")
	_, err := q.Dequeue(context.Background())
	if err == nil || strings.Contains(err.Error(), "restore malformed job") {
		t.Fatalf("Dequeue should report only the quarantine failure, got: %v", err)
	}
	r.mu.Lock()
	mainLen := len(r.lists["test"])
	r.mu.Unlock()
	if mainLen != 1 {
		t.Fatalf("malformed job lost after a hung quarantine write (main=%d)", mainLen)
	}
}

// mainPushCounter counts LPush calls that target the main list.
type mainPushCounter struct {
	RedisClient
	mainPushes int
}

func (f *mainPushCounter) LPush(ctx context.Context, key string, values ...any) error {
	if key == "test" {
		f.mainPushes++
	}
	return f.RedisClient.LPush(ctx, key, values...)
}

// Skipped jobs go back in one LPush, so a dead backend costs one recovery
// deadline per Dequeue, not one per skipped job, and the list keeps its
// order.
func TestRedisSkippedJobsRestoredInOneBatch(t *testing.T) {
	r := newMockRedis()
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		data, _ := json.Marshal(Job{ID: id, Type: "other", MaxAttempts: 3})
		_ = r.LPush(ctx, "test", data)
	}
	r.mu.Lock()
	before := append([]string(nil), r.lists["test"]...)
	r.mu.Unlock()

	client := &mainPushCounter{RedisClient: r}
	q := NewRedisQueue(client, "test")
	if _, err := q.Dequeue(ctx, "wanted"); !errors.Is(err, ErrNoJob) {
		t.Fatalf("Dequeue = %v, want ErrNoJob", err)
	}
	if client.mainPushes != 1 {
		t.Fatalf("skipped jobs restored with %d LPush calls, want 1", client.mainPushes)
	}
	r.mu.Lock()
	after := append([]string(nil), r.lists["test"]...)
	r.mu.Unlock()
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("restore changed list order:\nbefore %q\nafter  %q", before, after)
	}
}
