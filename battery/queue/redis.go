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
	HDel(ctx context.Context, key string, fields ...string) error
	Del(ctx context.Context, keys ...string) error
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
}

// NewRedisQueue creates a new Redis-backed queue.
func NewRedisQueue(client RedisClient, queueName string) *RedisQueue {
	return &RedisQueue{
		client:            client,
		queueName:         queueName,
		processingQueue:   queueName + ":processing",
		deadLetterQueue:   queueName + ":dead",
		visibilityTimeout: 30 * time.Second,
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
		job.CreatedAt = time.Now()
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

	var skipped []string
	for {
		data, err := q.client.RPop(ctx, q.queueName)
		if err != nil {
			// Re-enqueue skipped jobs so we don't lose them.
			for _, s := range skipped {
				_ = q.client.LPush(ctx, q.queueName, s)
			}
			return Job{}, ErrNoJob
		}

		var job Job
		if err := json.Unmarshal([]byte(data), &job); err != nil {
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
			"expiresAt": time.Now().Add(q.visibilityTimeout).Unix(),
		})
		_ = q.client.HSet(ctx, q.processingQueue, job.ID, jobData)

		// Re-enqueue skipped jobs.
		for _, s := range skipped {
			_ = q.client.LPush(ctx, q.queueName, s)
		}

		return job, nil
	}
}

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
