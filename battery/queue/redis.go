package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// RedisClient defines the minimal Redis operations needed by RedisQueue.
// This is an interface so callers can inject any Redis client (go-redis, redigo, etc.)
// without this package importing a specific driver.
type RedisClient interface {
	LPush(ctx context.Context, key string, values ...interface{}) error
	RPop(ctx context.Context, key string) (string, error)
	HSet(ctx context.Context, key string, values ...interface{}) error
	HGet(ctx context.Context, key, field string) (string, error)
	// HGetAll returns every field→value pair in the hash. Used by Reclaim to
	// scan the processing set for expired in-flight jobs.
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	Del(ctx context.Context, keys ...string) error
	// LRange returns the elements of the list at key in the inclusive range
	// [start, stop]; negative indices count from the tail (-1 is the last
	// element). Used by Replay to read the dead-letter list.
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	// LRem removes up to count occurrences of value from the list at key and
	// returns the number removed. Used by Replay to pull one entry off the
	// dead-letter list.
	LRem(ctx context.Context, key string, count int64, value interface{}) (int64, error)
}

// RedisQueue implements the Queue interface backed by Redis lists and hashes.
// It supports a visibility timeout for in-flight jobs and a dead letter queue
// for jobs that exceed MaxAttempts.
type RedisQueue struct {
	client            RedisClient
	queueName         string
	processingQueue   string
	deadLetterQueue   string
	visibilityTimeout time.Duration

	// now is the clock used for visibility-timeout stamps and expiry checks.
	// Defaults to time.Now; tests substitute a fake clock so reclaim
	// behaviour can be asserted without wall-clock sleeps.
	now func() time.Time
}

// NewRedisQueue creates a new Redis-backed queue.
func NewRedisQueue(client RedisClient, queueName string) *RedisQueue {
	return &RedisQueue{
		client:            client,
		queueName:         queueName,
		processingQueue:   queueName + ":processing",
		deadLetterQueue:   queueName + ":dead",
		visibilityTimeout: 30 * time.Second,
		now:               time.Now,
	}
}

// SetVisibilityTimeout configures how long a job can be in-flight before it
// is considered abandoned and eligible for re-delivery.
func (q *RedisQueue) SetVisibilityTimeout(d time.Duration) {
	q.visibilityTimeout = d
}

// Enqueue pushes a job onto the Redis list, applying defaults for ID,
// CreatedAt, and MaxAttempts when not set.
func (q *RedisQueue) Enqueue(ctx context.Context, job Job) error {
	if job.ID == "" {
		job.ID = redisRandomID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = q.now()
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return q.client.LPush(ctx, q.queueName, data)
}

// Dequeue pops a job from the Redis list and moves it to the processing queue.
// If types are specified, only jobs matching one of those types are returned;
// non-matching jobs are pushed back onto the list.
func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	requeueSkipped := func(skipped []string) {
		for _, s := range skipped {
			_ = q.client.LPush(ctx, q.queueName, s)
		}
	}

	var skipped []string
	for {
		// Bound the type-miss drain: without a server-side filter a rare-type
		// request could otherwise RPop the entire list into process memory
		// (OOM). When the bound is hit, re-enqueue what we drained and report
		// no job — the caller retries.
		if len(skipped) >= maxSkipDrain {
			requeueSkipped(skipped)
			return Job{}, ErrNoJob
		}

		data, err := q.client.RPop(ctx, q.queueName)
		if err != nil {
			// Re-enqueue skipped jobs so we don't lose them.
			requeueSkipped(skipped)
			return Job{}, ErrNoJob
		}

		var job Job
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			// A malformed entry must not take down the valid jobs we already
			// RPop'd: re-enqueue them, then quarantine the bad entry to the
			// dead-letter queue instead of silently dropping it.
			requeueSkipped(skipped)
			_ = q.client.LPush(ctx, q.deadLetterQueue, data)
			return Job{}, fmt.Errorf("unmarshal job: %w", err)
		}

		// Check type filter.
		if len(typeSet) > 0 {
			if _, ok := typeSet[job.Type]; !ok {
				skipped = append(skipped, data)
				continue
			}
		}

		// Track in processing queue for visibility timeout.
		jobData, _ := json.Marshal(map[string]interface{}{
			"job":       data,
			"expiresAt": q.now().Add(q.visibilityTimeout).UnixNano(),
		})
		_ = q.client.HSet(ctx, q.processingQueue, job.ID, jobData)

		// Re-enqueue skipped jobs.
		requeueSkipped(skipped)

		return job, nil
	}
}

// maxSkipDrain bounds how many type-miss jobs Dequeue will pull off the list
// while looking for a matching type, so a rare-type filter cannot pull the
// whole queue into process memory.
const maxSkipDrain = 1024

// Ack removes a single job from the processing queue after successful handling.
func (q *RedisQueue) Ack(ctx context.Context, jobID string) error {
	return q.client.HDel(ctx, q.processingQueue, jobID)
}

// Nack handles a failed job. If retries remain, it re-enqueues the job;
// otherwise it moves it to the dead letter queue.
func (q *RedisQueue) Nack(ctx context.Context, jobID string) error {
	// Get the job from processing queue.
	data, err := q.client.HGet(ctx, q.processingQueue, jobID)
	if err != nil {
		return fmt.Errorf("nack: job not found in processing: %w", err)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		return fmt.Errorf("nack: unmarshal: %w", err)
	}

	// Extract original job — entry["job"] is a string containing the job JSON.
	jobStr, ok := entry["job"].(string)
	if !ok {
		return fmt.Errorf("nack: job field has unexpected type")
	}
	var job Job
	if err := json.Unmarshal([]byte(jobStr), &job); err != nil {
		return fmt.Errorf("nack: unmarshal job: %w", err)
	}

	// Remove from processing.
	_ = q.client.HDel(ctx, q.processingQueue, jobID)

	// Increment attempts and check max.
	job.Attempts++
	if job.Attempts >= job.MaxAttempts {
		// Move to dead letter queue.
		dlqData, _ := json.Marshal(job)
		return q.client.LPush(ctx, q.deadLetterQueue, dlqData)
	}

	// Re-enqueue for retry.
	jobData, _ := json.Marshal(job)
	return q.client.LPush(ctx, q.queueName, jobData)
}

// Reclaim scans the processing set for in-flight jobs whose visibility
// timeout has passed (the worker that claimed them crashed before Ack/Nack),
// re-enqueues them onto the main list, and removes the stale processing
// entry. Returns the number of jobs re-delivered. Call it periodically (e.g.
// from a background ticker) to make in-flight Redis work crash-safe.
func (q *RedisQueue) Reclaim(ctx context.Context) (int, error) {
	entries, err := q.client.HGetAll(ctx, q.processingQueue)
	if err != nil {
		return 0, fmt.Errorf("reclaim: scan processing: %w", err)
	}
	now := q.now().UnixNano()
	reclaimed := 0
	for jobID, raw := range entries {
		var entry struct {
			Job       string `json:"job"`
			ExpiresAt int64  `json:"expiresAt"`
		}
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			// Corrupt processing entry: drop it so it can't wedge the sweep.
			_ = q.client.HDel(ctx, q.processingQueue, jobID)
			continue
		}
		if entry.ExpiresAt > now {
			continue // still within its lease
		}
		// Re-enqueue the original job, then clear the processing entry. Order
		// matters: enqueue first so a crash between the two ops re-delivers
		// (at-least-once) rather than loses the job.
		if err := q.client.LPush(ctx, q.queueName, entry.Job); err != nil {
			return reclaimed, fmt.Errorf("reclaim: re-enqueue %s: %w", jobID, err)
		}
		_ = q.client.HDel(ctx, q.processingQueue, jobID)
		reclaimed++
	}
	return reclaimed, nil
}

// Replay implements [Replayable]: it pulls a terminally-failed job off the
// dead-letter list and re-enqueues it onto the main queue with its attempts
// counter reset, so it gets a full set of retries again. It is idempotent —
// replaying an unknown job ID is a no-op (returns nil), matching DBQueue.Replay.
//
// The entry is LPush'd back onto the main queue first and only removed from the
// dead list on success, so a failure between the two ops leaves the job on the
// dead list (recoverable) rather than dropping it. A crash in that window can
// leave one copy on each list; the next Replay/Dequeue tolerates the duplicate.
func (q *RedisQueue) Replay(ctx context.Context, jobID string) error {
	entries, err := q.client.LRange(ctx, q.deadLetterQueue, 0, -1)
	if err != nil {
		return fmt.Errorf("replay: read dead-letter queue: %w", err)
	}

	for _, raw := range entries {
		var job Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			// Skip corrupt dead-list entries rather than letting one bad row
			// block replay of valid jobs.
			continue
		}
		if job.ID != jobID {
			continue
		}

		// Reset for a fresh set of retries, then re-marshal.
		job.Attempts = 0
		requeued, err := json.Marshal(job)
		if err != nil {
			return fmt.Errorf("replay: marshal job: %w", err)
		}

		// Enqueue first so a failure here leaves the original on the dead list
		// (no loss); only then remove the original dead-list entry.
		if err := q.client.LPush(ctx, q.queueName, requeued); err != nil {
			return fmt.Errorf("replay: re-enqueue job: %w", err)
		}
		if _, err := q.client.LRem(ctx, q.deadLetterQueue, 1, raw); err != nil {
			return fmt.Errorf("replay: remove from dead-letter queue: %w", err)
		}
		return nil
	}

	// No matching dead-lettered job — idempotent no-op.
	return nil
}

// ListJobs implements [Browsable] for the Redis backend. The only durable
// job state accessible without a scan of the full main/processing lists is
// the dead-letter queue, so this returns dead jobs for status "failed" (or
// an empty/"all" status) and nothing for any other status value. Jobs are
// returned newest-first (head of the Redis list) up to limit entries.
// limit <= 0 defaults to 100.
func (q *RedisQueue) ListJobs(ctx context.Context, status string, limit int) ([]Job, error) {
	if status != "" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	entries, err := q.client.LRange(ctx, q.deadLetterQueue, 0, int64(limit-1))
	if err != nil {
		return nil, fmt.Errorf("listjobs: read dead-letter queue: %w", err)
	}
	out := make([]Job, 0, len(entries))
	for _, raw := range entries {
		var job Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			// Skip corrupt entries so one bad entry doesn't block inspection.
			continue
		}
		out = append(out, job)
	}
	return out, nil
}

// Stats implements [Browsable] for the Redis backend. It reports the count
// of dead-lettered jobs under the "failed" key; pending/in-flight jobs are
// not enumerable without a full scan and are omitted. Cheap: a single
// LRange(0, -1) length read.
func (q *RedisQueue) Stats(ctx context.Context) (JobStats, error) {
	entries, err := q.client.LRange(ctx, q.deadLetterQueue, 0, -1)
	if err != nil {
		return nil, fmt.Errorf("stats: read dead-letter queue: %w", err)
	}
	stats := JobStats{}
	if n := len(entries); n > 0 {
		stats["failed"] = n
	}
	return stats, nil
}

// Start launches a background goroutine that calls Reclaim on every tick
// to re-enqueue in-flight jobs whose visibility timeout has expired (e.g.
// because the worker that claimed them crashed before Ack/Nack). It mirrors
// DBQueue's built-in lease-expiry reclaim so crashed-worker jobs are not
// silently stranded.
//
// The goroutine exits when ctx is cancelled. interval controls how often
// Reclaim is called; a value <= 0 defaults to 30 seconds. Typical use:
//
//	q.Start(ctx, 30*time.Second)
func (q *RedisQueue) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = q.Reclaim(ctx)
			}
		}
	}()
}

// Compile-time interface assertions for RedisQueue.
var (
	_ Queue      = (*RedisQueue)(nil)
	_ Browsable  = (*RedisQueue)(nil)
	_ Replayable = (*RedisQueue)(nil)
)

// Close is a no-op for RedisQueue — the caller manages the Redis connection.
func (q *RedisQueue) Close() error {
	return nil
}

// redisRandomID generates a 16-byte hex string ID.
func redisRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
