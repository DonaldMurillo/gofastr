package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type redisRestoreFaults struct {
	RedisClient
	pushes     int
	skippedErr error
}

func (f *redisRestoreFaults) LPush(ctx context.Context, queue string, values ...any) error {
	f.pushes++
	switch f.pushes {
	case 1:
		return errors.New("quarantine down")
	case 2:
		return errors.New("restore down")
	case 3:
		if f.skippedErr != nil {
			return f.skippedErr
		}
	}
	return f.RedisClient.LPush(ctx, queue, values...)
}

// TestRedisRequeuesSkippedAfterRestoreFail: a failed restore of the popped
// entry still requeues the skipped jobs, and their error joins last.
func TestRedisRequeuesSkippedAfterRestoreFail(t *testing.T) {
	const queue = "test"
	const restoreErr = "unmarshal job: invalid character 'm' looking for beginning of object key string\nquarantine malformed job: quarantine down\nrestore malformed job: restore down"
	for _, tc := range []struct {
		name       string
		skippedErr error
		want       string
	}{
		{"skipped job is requeued", nil, restoreErr},
		{"skipped error follows restore error", errors.New("requeue down"), restoreErr + "\nrestore skipped jobs: requeue down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := newMockRedis()
			skipped, err := json.Marshal(Job{ID: "skipped", Type: "other", MaxAttempts: 3})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.LPush(ctx, queue, string(skipped), "{malformed"); err != nil {
				t.Fatal(err)
			}
			backend := &redisRestoreFaults{RedisClient: r, skippedErr: tc.skippedErr}
			q := NewRedisQueue(backend, queue)
			t.Cleanup(func() { _ = q.Close() })
			_, err = q.Dequeue(ctx, "wanted")
			if err == nil || err.Error() != tc.want {
				t.Errorf("Dequeue error = %q, want %q", err, tc.want)
			}
			if backend.pushes != 3 {
				t.Errorf("LPush called %d times, want 3", backend.pushes)
			}
			if tc.skippedErr == nil {
				if data, err := r.RPop(ctx, queue); err != nil || data != string(skipped) {
					t.Errorf("main queue = %q, %v, want the skipped job", data, err)
				}
			}
		})
	}
}
