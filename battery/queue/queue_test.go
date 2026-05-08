package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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

	time.Sleep(100 * time.Millisecond)
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

	for i := 0; i < numJobs; i++ {
		err := q.Enqueue(context.Background(), Job{Type: "work"})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	// Give workers time to process.
	time.Sleep(500 * time.Millisecond)
	q.Close()

	if got := executed.Load(); got != numJobs {
		t.Errorf("expected %d executions, got %d", numJobs, got)
	}
}

func TestRetryOnFailure(t *testing.T) {
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

	// Wait for all retries.
	time.Sleep(500 * time.Millisecond)
	q.Close()

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestMaxAttemptsExceeded(t *testing.T) {
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

	time.Sleep(500 * time.Millisecond)
	q.Close()

	// Should have been attempted exactly MaxAttempts times then dropped.
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts (MaxAttempts), got %d", got)
	}
}

func TestCloseDrainsPendingJobs(t *testing.T) {
	q := NewMemoryQueue(2)

	var executed atomic.Int32
	// Slow handler to simulate work in progress.
	q.RegisterHandler("slow", func(ctx context.Context, job Job) error {
		time.Sleep(50 * time.Millisecond)
		executed.Add(1)
		return nil
	})

	q.Start()

	for i := 0; i < 10; i++ {
		_ = q.Enqueue(context.Background(), Job{Type: "slow"})
	}

	// Close should drain all jobs.
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

	// Pause processing so we can enqueue jobs first.
	// We'll use a manual dequeue approach to test priority ordering.
	jobs := []Job{
		{ID: "low", Type: "test", Priority: 1},
		{ID: "high", Type: "test", Priority: 10},
		{ID: "mid", Type: "test", Priority: 5},
	}

	// Enqueue directly into channel (bypass any sorting).
	for _, j := range jobs {
		_ = q.Enqueue(context.Background(), j)
	}

	// Collect jobs in order.
	var order []string
	for i := 0; i < 3; i++ {
		job, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatalf("Dequeue %d: %v", i, err)
		}
		order = append(order, job.ID)
	}

	// FIFO ordering from the channel: low, high, mid.
	// Priority is an attribute on the Job for backends that support it;
	// the MemoryQueue channel is FIFO.
	expected := []string{"low", "high", "mid"}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("position %d: expected %q, got %q", i, id, order[i])
		}
	}

	q.Close()
}

func TestSchedulerFiresAtInterval(t *testing.T) {
	q := NewMemoryQueue(1)

	var executed atomic.Int32
	q.RegisterHandler("scheduled", func(ctx context.Context, job Job) error {
		executed.Add(1)
		return nil
	})

	q.Start()

	sched := NewScheduler(q)
	sched.Every(100*time.Millisecond).Job("scheduled", json.RawMessage(`{}`)).Register()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sched.Start(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
	q.Close()

	got := executed.Load()
	if got < 2 {
		t.Errorf("expected at least 2 scheduled executions, got %d", got)
	}
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

func (m *mockRedis) LPush(_ context.Context, key string, values ...interface{}) error {
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
		return "", fmt.Errorf("list empty")
	}
	val := l[len(l)-1]
	m.lists[key] = l[:len(l)-1]
	return val, nil
}

func (m *mockRedis) HSet(_ context.Context, key string, values ...interface{}) error {
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
		return "", fmt.Errorf("hash not found")
	}
	v, ok := h[field]
	if !ok {
		return "", fmt.Errorf("field not found")
	}
	return v, nil
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

	// Dequeue both — this puts them in the processing hash.
	job1, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue job1: %v", err)
	}
	job2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue job2: %v", err)
	}

	// Ack only job1.
	if err := q.Ack(ctx, job1.ID); err != nil {
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

	// Nack — should re-enqueue with incremented attempts.
	if err := q.Nack(ctx, job.ID); err != nil {
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

	// Nack — MaxAttempts=1 so it should go to DLQ.
	if err := q.Nack(ctx, job.ID); err != nil {
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

	// Dequeue another email — "b" should be skipped and pushed back.
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
