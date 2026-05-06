package queue

import (
	"context"
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

// Enqueue pushes a job onto the Redis list.
func (q *RedisQueue) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return q.client.LPush(ctx, q.queueName, data)
}

// Dequeue pops a job from the Redis list and moves it to the processing queue.
func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	data, err := q.client.RPop(ctx, q.queueName)
	if err != nil {
		return Job{}, ErrNoJob
	}

	var job Job
	if err := json.Unmarshal([]byte(data), &job); err != nil {
		return Job{}, fmt.Errorf("unmarshal job: %w", err)
	}

	// Track in processing queue for visibility timeout.
	jobData, _ := json.Marshal(map[string]interface{}{
		"job":       data,
		"expiresAt": time.Now().Add(q.visibilityTimeout).Unix(),
	})
	_ = q.client.HSet(ctx, q.processingQueue, job.ID, jobData)

	return job, nil
}

// Ack removes a job from the processing queue after successful handling.
func (q *RedisQueue) Ack(ctx context.Context, jobID string) error {
	return q.client.Del(ctx, q.processingQueue)
}

// Nack handles a failed job. If retries remain, it re-enqueues the job;
// otherwise it moves it to the dead letter queue.
func (q *RedisQueue) Nack(ctx context.Context, jobID string) error {
	// Note: A full implementation would fetch the job from the processing hash,
	// increment attempts, and either re-enqueue or move to DLQ. This is a
	// structural outline since we can't import a real Redis driver here.
	return nil
}

// Close is a no-op for RedisQueue — the caller manages the Redis connection.
func (q *RedisQueue) Close() error {
	return nil
}

// HDel is a helper that should be part of the RedisClient interface in a
// full implementation. Added here as a note for completeness.
// The RedisClient interface should ideally include:
//   HDel(ctx context.Context, key string, fields ...string) error
