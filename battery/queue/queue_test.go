package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// waitFor polls fn until it returns true or the deadline passes, failing the
// test with msg on timeout. Same convention as battery/webhook. Use it instead
// of fixed sleeps so async assertions are bounded, not timing-dependent.
func waitFor(t *testing.T, fn func() bool, max time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", msg)
}

// ─── Memory Queue Tests ──────────────────────────────────────────────

func TestEnqueueAndHandlerExecution(t *testing.T) {
	q := NewMemoryQueue(2)

	var executed atomic.Int32
	q.RegisterHandler("test", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	err := q.Enqueue(context.Background(), Job{
		Type:    "test",
		Payload: json.RawMessage(`{"hello":"world"}`),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Close drains the channel and waits for workers, so the handler is
	// guaranteed to have run by the time it returns, no sleep needed.
	q.Close()

	if got := executed.Load(); got != 1 {
		t.Errorf("expected 1 execution, got %d", got)
	}
}

func TestMultipleWorkers(t *testing.T) {
	const numJobs = 100
	q := NewMemoryQueue(4)

	var executed atomic.Int32
	q.RegisterHandler("work", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	for i := range numJobs {
		err := q.Enqueue(context.Background(), Job{Type: "work"})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	// Close drains all pending jobs and waits for workers before returning.
	q.Close()

	if got := executed.Load(); got != numJobs {
		t.Errorf("expected %d executions, got %d", numJobs, got)
	}
}

func TestRetryOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := NewMemoryQueue(1)

		var attempts atomic.Int32
		q.RegisterHandler("retry-me", func(ctx context.Context, job Job) error {
			a := attempts.Add(1)
			// Fail the first two attempts, succeed on the third.
			if a < 3 {
				return fmt.Errorf("transient error (attempt %d)", a)
			}
			return nil
		})

		q.Start()

		err := q.Enqueue(context.Background(), Job{
			Type:        "retry-me",
			MaxAttempts: 3,
		})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		// Retries re-enqueue asynchronously, so Close alone can't be used as the
		// barrier (it would race the re-enqueue). Poll until the third attempt.
		waitFor(t, func() bool { return attempts.Load() >= 3 }, 5*time.Second,
			"job was not retried to the 3rd attempt")
		q.Close()

		if got := attempts.Load(); got != 3 {
			t.Errorf("expected 3 attempts, got %d", got)
		}
	})
}

func TestMaxAttemptsExceeded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := NewMemoryQueue(1)

		var attempts atomic.Int32
		q.RegisterHandler("always-fail", func(ctx context.Context, job Job) error {
			attempts.Add(1)
			return fmt.Errorf("permanent failure")
		})

		q.Start()

		err := q.Enqueue(context.Background(), Job{
			Type:        "always-fail",
			MaxAttempts: 3,
		})
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		// Poll until MaxAttempts is reached, then Close (which drains anything
		// still queued) and assert no extra attempt happened.
		waitFor(t, func() bool { return attempts.Load() >= 3 }, 5*time.Second,
			"job did not reach MaxAttempts")
		q.Close()

		// Should have been attempted exactly MaxAttempts times then dropped.
		if got := attempts.Load(); got != 3 {
			t.Errorf("expected 3 attempts (MaxAttempts), got %d", got)
		}
	})
}

func TestCloseDrainsPendingJobs(t *testing.T) {
	q := NewMemoryQueue(2)

	var executed atomic.Int32
	// Handlers block on a gate so jobs are still pending/in-flight when Close
	// is called, deterministic, no sleep.
	gate := make(chan struct{})
	q.RegisterHandler("slow", func(ctx context.Context, job Job) error {
		<-gate
		executed.Add(1)
		return nil
	})

	q.Start()

	for range 10 {
		_ = q.Enqueue(context.Background(), Job{Type: "slow"})
	}

	// Release the handlers, then Close, it must drain all 10 jobs.
	close(gate)
	q.Close()

	if got := executed.Load(); got != 10 {
		t.Errorf("expected all 10 jobs to complete on Close, got %d", got)
	}
}

func TestEnqueueAfterClose(t *testing.T) {
	q := NewMemoryQueue(1)
	q.Start()
	q.Close()

	err := q.Enqueue(context.Background(), Job{Type: "test"})
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

func TestJobPriorityOrdering(t *testing.T) {
	q := NewMemoryQueue(1)

	// Enqueue jobs of differing priority without starting workers, then
	// drain via manual Dequeue to observe the order the store yields.
	jobs := []Job{
		{ID: "low", Type: "test", Priority: 1},
		{ID: "high", Type: "test", Priority: 10},
		{ID: "mid", Type: "test", Priority: 5},
	}

	for _, j := range jobs {
		_ = q.Enqueue(context.Background(), j)
	}

	// Collect jobs in priority order.
	var order []string
	for i := range 3 {
		job, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatalf("Dequeue %d: %v", i, err)
		}
		order = append(order, job.ID)
	}

	// Priority DESC (high → mid → low). MemoryQueue now honours Priority
	// via a priority heap; ties break by enqueue order (FIFO).
	expected := []string{"high", "mid", "low"}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("position %d: expected %q, got %q", i, id, order[i])
		}
	}

	q.Close()
}

func TestSchedulerFiresAtInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := NewMemoryQueue(1)

		var executed atomic.Int32
		q.RegisterHandler("scheduled", func(ctx context.Context, job Job) error {
			executed.Add(1)
			return nil
		})

		q.Start()

		sched := NewScheduler(q)
		sched.Every(20*time.Millisecond).Job("scheduled", json.RawMessage(`{}`)).Register()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var wg sync.WaitGroup
		wg.Go(func() {
			sched.Start(ctx)
		})

		// Poll until at least 2 interval firings have been executed, instead of
		// trusting a fixed window to contain N ticks on a loaded machine.
		waitFor(t, func() bool { return executed.Load() >= 2 }, 5*time.Second,
			"scheduler did not fire at least twice")
		cancel()
		wg.Wait()
		q.Close()
	})
}

func TestDequeueByType(t *testing.T) {
	q := NewMemoryQueue(1)

	_ = q.Enqueue(context.Background(), Job{ID: "a", Type: "email"})
	_ = q.Enqueue(context.Background(), Job{ID: "b", Type: "sms"})
	_ = q.Enqueue(context.Background(), Job{ID: "c", Type: "email"})

	// Dequeue only email jobs.
	job, err := q.Dequeue(context.Background(), "email")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.Type != "email" {
		t.Errorf("expected email job, got %q", job.Type)
	}
	if job.ID != "a" {
		t.Errorf("expected job ID 'a', got %q", job.ID)
	}

	// Dequeue another email.
	job, err = q.Dequeue(context.Background(), "email")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.ID != "c" {
		t.Errorf("expected job ID 'c', got %q", job.ID)
	}

	q.Close()
}

// ─── Redis Queue Tests ───────────────────────────────────────────────

// mockRedis is an in-memory implementation of RedisClient for testing.
type mockRedis struct {
	lists  map[string][]string
	hashes map[string]map[string]string
	mu     sync.Mutex
}

func newMockRedis() *mockRedis {
	return &mockRedis{
		lists:  make(map[string][]string),
		hashes: make(map[string]map[string]string),
	}
}

func (m *mockRedis) LPush(_ context.Context, key string, values ...any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range values {
		var s string
		switch val := v.(type) {
		case string:
			s = val
		case []byte:
			s = string(val)
		default:
			s = fmt.Sprint(v)
		}
		m.lists[key] = append([]string{s}, m.lists[key]...)
	}
	return nil
}

func (m *mockRedis) RPop(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.lists[key]
	if len(l) == 0 {
		return "", ErrRedisEmpty
	}
	val := l[len(l)-1]
	m.lists[key] = l[:len(l)-1]
	return val, nil
}

// RPopHSet implements [PopRecorder] under the single mutex, the atomicity
// the Lua script gives on a real server.
func (m *mockRedis) RPopHSet(_ context.Context, list, hash, field string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.lists[list]
	if len(l) == 0 {
		return "", ErrRedisEmpty
	}
	val := l[len(l)-1]
	m.lists[list] = l[:len(l)-1]
	if m.hashes[hash] == nil {
		m.hashes[hash] = make(map[string]string)
	}
	m.hashes[hash][field] = val
	return val, nil
}

func (m *mockRedis) HSet(_ context.Context, key string, values ...any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key] == nil {
		m.hashes[key] = make(map[string]string)
	}
	// Expect field, value pairs.
	for i := 0; i+1 < len(values); i += 2 {
		field := fmt.Sprint(values[i])
		var val string
		switch v := values[i+1].(type) {
		case string:
			val = v
		case []byte:
			val = string(v)
		default:
			val = fmt.Sprint(v)
		}
		m.hashes[key][field] = val
	}
	return nil
}
func (m *mockRedis) HGet(_ context.Context, key, field string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hashes[key]
	if !ok {
		return "", ErrRedisEmpty
	}
	v, ok := h[field]
	if !ok {
		return "", ErrRedisEmpty
	}
	return v, nil
}

func (m *mockRedis) HGetAll(_ context.Context, key string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hashes[key]
	if !ok {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(h))
	maps.Copy(out, h)
	return out, nil
}

func (m *mockRedis) HDel(_ context.Context, key string, fields ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hashes[key]
	if !ok {
		return nil
	}
	for _, f := range fields {
		delete(h, f)
	}
	return nil
}

// HDelIfEqual implements [CompareAndDeleter]: the mock holds the whole hash
// under one mutex, so read-compare-delete here is genuinely atomic — the same
// guarantee a Lua script gives on a real server.
func (m *mockRedis) HDelIfEqual(_ context.Context, key, field, expect string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hashes[key]
	if !ok {
		return false, nil
	}
	if v, ok := h[field]; !ok || v != expect {
		return false, nil
	}
	delete(h, field)
	return true, nil
}

// MoveIfEqual implements [CompareAndMover] under the same single mutex, the
// atomicity the Lua script gives on a real server.
func (m *mockRedis) MoveIfEqual(_ context.Context, key, field, expect, list, value string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.hashes[key][field]; !ok || v != expect {
		return false, nil
	}
	m.lists[list] = append([]string{value}, m.lists[list]...)
	delete(m.hashes[key], field)
	return true, nil
}

// Eval implements [ScriptRunner] for the one script the queue runs,
// eraseScript, in Go under the single mutex: the atomicity EVAL gives on a
// real server. redis_live_test.go runs the Lua itself against one.
func (m *mockRedis) Eval(_ context.Context, script string, keys []string, args ...any) (any, error) {
	if script == moveScript {
		return m.evalMove(keys, args)
	}
	if script != eraseScript || len(keys) != 5 || len(args) != 2 {
		return nil, fmt.Errorf("mockRedis: unknown script call")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	uid, dry := fmt.Sprint(args[0]), fmt.Sprint(args[1]) == "1"
	// Like the script: the raw top-level user_id token is compared with
	// ARGV[1], json.Marshal(userID), and the payload is never decoded.
	names := func(raw string) bool {
		var v map[string]json.RawMessage
		return json.Unmarshal([]byte(raw), &v) == nil && string(v["user_id"]) == uid
	}
	var n int64
	for i, key := range keys[:2] {
		for f, raw := range m.hashes[key] {
			if i == 1 {
				var e map[string]any
				if json.Unmarshal([]byte(raw), &e) != nil {
					continue
				}
				job, _ := e["job"].(string)
				raw = job
			}
			if names(raw) {
				n++
				if !dry {
					delete(m.hashes[key], f)
				}
			}
		}
	}
	for _, key := range keys[2:] {
		kept := m.lists[key][:0:0]
		for _, raw := range m.lists[key] {
			if names(raw) {
				n++
				if !dry {
					continue
				}
			}
			kept = append(kept, raw)
		}
		m.lists[key] = kept
	}
	return n, nil
}

// noScripts, embedded beside *mockRedis, hides mockRedis's Eval: two
// methods of one name at the same depth cancel out, so the wrapper is no
// [ScriptRunner] and the queue takes the paths of a client without scripts
// (TestNoScriptsHidesEval holds it to that).
type noScripts struct{}

func (noScripts) Eval(context.Context, string, []string, ...any) (any, error) {
	panic("noScripts.Eval is never selected")
}

// evalMove is moveScript in Go under the single mutex.
func (m *mockRedis) evalMove(keys []string, args []any) (any, error) {
	if len(keys) != 2 || len(args) < 1 || (len(args)-1)%4 != 0 {
		return nil, fmt.Errorf("mockRedis: bad move script call")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kinds := fmt.Sprint(args[0])
	src, dst := keys[0], keys[1]
	var n int64
	for i := 1; i < len(args); i += 4 {
		from, expect, to, value := fmt.Sprint(args[i]), fmt.Sprint(args[i+1]), fmt.Sprint(args[i+2]), fmt.Sprint(args[i+3])
		ok := false
		if kinds[0] == 'h' {
			if v, held := m.hashes[src][from]; held && v == expect {
				delete(m.hashes[src], from)
				ok = true
			}
		} else if j := slices.Index(m.lists[src], expect); j >= 0 {
			m.lists[src] = slices.Delete(slices.Clone(m.lists[src]), j, j+1)
			ok = true
		}
		if !ok {
			continue
		}
		if kinds[1] == 'h' {
			if m.hashes[dst] == nil {
				m.hashes[dst] = make(map[string]string)
			}
			m.hashes[dst][to] = value
		} else {
			m.lists[dst] = append([]string{value}, m.lists[dst]...)
		}
		n++
	}
	return n, nil
}

func (m *mockRedis) Del(_ context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.lists, k)
		delete(m.hashes, k)
	}
	return nil
}

func TestRedisEnqueueDefaults(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")

	job := Job{Type: "email", Payload: json.RawMessage(`{}`)}
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Pop the job back and check defaults were applied.
	data, err := r.RPop(context.Background(), "test")
	if err != nil {
		t.Fatalf("RPop: %v", err)
	}

	var stored Job
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if stored.ID == "" {
		t.Error("expected ID to be set")
	}
	if stored.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if stored.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", stored.MaxAttempts)
	}
}

func TestRedisAckRemovesOneJob(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	// Enqueue two jobs with explicit IDs.
	_ = q.Enqueue(ctx, Job{ID: "job1", Type: "test"})
	_ = q.Enqueue(ctx, Job{ID: "job2", Type: "test"})

	// Dequeue both, this puts them in the processing hash.
	job1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue job1: %v", err)
	}
	job2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue job2: %v", err)
	}

	// Ack only job1.
	if err := q.Ack(ctx, job1); err != nil {
		t.Fatalf("Ack job1: %v", err)
	}

	// job2 should still be in the processing hash.
	r.mu.Lock()
	proc := r.hashes["test:processing"]
	_, hasJob1 := proc["job1"]
	_, hasJob2 := proc["job2"]
	r.mu.Unlock()

	if hasJob1 {
		t.Error("job1 should have been removed from processing after Ack")
	}
	if !hasJob2 {
		t.Error("job2 should still be in processing after Ack of job1")
	}
	_ = job2
}

func TestRedisNackRetries(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "retry1", Type: "test", MaxAttempts: 3})

	// Dequeue the job.
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.ID != "retry1" {
		t.Fatalf("expected job ID 'retry1', got %q", job.ID)
	}

	// Nack: should re-enqueue with incremented attempts.
	if err := q.Nack(ctx, job); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	// Verify the job is back on the queue.
	data, err := r.RPop(ctx, "test")
	if err != nil {
		t.Fatalf("expected job to be re-enqueued, but list is empty")
	}

	var retried Job
	if err := json.Unmarshal([]byte(data), &retried); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if retried.Attempts != 1 {
		t.Errorf("expected Attempts=1 after first nack, got %d", retried.Attempts)
	}
	if retried.ID != "retry1" {
		t.Errorf("expected ID 'retry1', got %q", retried.ID)
	}
}

func TestRedisNackMovesToDLQ(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "dlq1", Type: "test", MaxAttempts: 1, Attempts: 0})

	// Dequeue the job.
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}

	// Nack. MaxAttempts=1 so it should go to DLQ.
	if err := q.Nack(ctx, job); err != nil {
		t.Fatalf("Nack: %v", err)
	}

	// Verify the job is on the dead letter queue.
	r.mu.Lock()
	dlq := r.lists["test:dead"]
	r.mu.Unlock()

	if len(dlq) == 0 {
		t.Fatal("expected job on dead letter queue, but DLQ is empty")
	}

	var dlqJob Job
	if err := json.Unmarshal([]byte(dlqJob.ID), &dlqJob); err != nil {
		// Unmarshal from the actual string
	}
	if err := json.Unmarshal([]byte(dlq[0]), &dlqJob); err != nil {
		t.Fatalf("Unmarshal DLQ job: %v", err)
	}

	if dlqJob.ID != "dlq1" {
		t.Errorf("expected DLQ job ID 'dlq1', got %q", dlqJob.ID)
	}

	// Main queue should be empty.
	r.mu.Lock()
	mainQ := r.lists["test"]
	r.mu.Unlock()
	if len(mainQ) != 0 {
		t.Errorf("expected main queue to be empty after DLQ move, got %d items", len(mainQ))
	}
}

func TestRedisDequeueTypeFilter(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	// Enqueue jobs of different types.
	_ = q.Enqueue(ctx, Job{ID: "a", Type: "email"})
	_ = q.Enqueue(ctx, Job{ID: "b", Type: "sms"})
	_ = q.Enqueue(ctx, Job{ID: "c", Type: "email"})

	// Dequeue only email jobs.
	job, err := q.Dequeue(ctx, "email")
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.Type != "email" {
		t.Errorf("expected email job, got %q", job.Type)
	}
	if job.ID != "a" {
		t.Errorf("expected job ID 'a', got %q", job.ID)
	}

	// Dequeue another email, "b" should be skipped and pushed back.
	job, err = q.Dequeue(ctx, "email")
	if err != nil {
		t.Fatalf("Dequeue second: %v", err)
	}
	if job.ID != "c" {
		t.Errorf("expected job ID 'c', got %q", job.ID)
	}

	// Non-matching type should return ErrNoJob.
	_, err = q.Dequeue(ctx, "email")
	if err != ErrNoJob {
		t.Errorf("expected ErrNoJob when no email jobs left, got %v", err)
	}

	// But "sms" job should still be on the queue.
	job, err = q.Dequeue(ctx, "sms")
	if err != nil {
		t.Fatalf("Dequeue sms: %v", err)
	}
	if job.ID != "b" {
		t.Errorf("expected job ID 'b', got %q", job.ID)
	}
}

func TestRedisDequeueNoFilter(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	ctx := context.Background()

	_ = q.Enqueue(ctx, Job{ID: "a", Type: "email"})
	_ = q.Enqueue(ctx, Job{ID: "b", Type: "sms"})

	// Without filter, should return first available (FIFO → "a").
	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if job.ID != "a" {
		t.Errorf("expected job ID 'a', got %q", job.ID)
	}
}

func TestRedisClose(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")

	// Close should be a no-op and always return nil.
	if err := q.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
