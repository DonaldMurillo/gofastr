package queue

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// raceRedis runs a hook once, right after the first read of a key, to put
// a worker's move into the gap between two of the erasure's reads.
type raceRedis struct {
	*mockRedis
	key  string
	hook func()
	once sync.Once
}

func (r *raceRedis) fire(key string) {
	if key == r.key {
		r.once.Do(r.hook)
	}
}

func (r *raceRedis) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	out, err := r.mockRedis.LRange(ctx, key, start, stop)
	r.fire(key)
	return out, err
}

func (r *raceRedis) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	out, err := r.mockRedis.HGetAll(ctx, key)
	r.fire(key)
	return out, err
}

// plainRedis hides the optional capabilities: the RedisClient methods only.
type plainRedis struct{ RedisClient }

func secretJob(id, user string) Job {
	return Job{ID: id, Type: "mail", UserID: user, Payload: json.RawMessage(`{"to":"` + user + `-secret"}`)}
}

// A worker Nacked the victim's claim back onto the main list after the
// erasure had read the main list and before it read the processing hash.
// The erasure found the job in neither and reported success with it on the
// main list (2026-10-31 review). A move between lists in either direction
// during the sweep is caught.
func TestEraseCatchesJobMovedMidSweep(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		key  string // the read after which the worker moves the job
		move func(q *RedisQueue, claimed Job) error
	}{
		{"nack after main read", "race-q", func(q *RedisQueue, claimed Job) error { return q.Nack(ctx, claimed) }},
		{"claim after processing read", "race-q:processing", func(q *RedisQueue, _ Job) error {
			_, err := q.Dequeue(ctx)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &raceRedis{mockRedis: newMockRedis(), key: tc.key}
			q := NewRedisQueue(r, "race-q")
			t.Cleanup(func() { _ = q.Close() })
			if err := q.Enqueue(ctx, secretJob("v1", "u1")); err != nil {
				t.Fatal(err)
			}
			var claimed Job
			if tc.key == "race-q" {
				var err error
				if claimed, err = q.Dequeue(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var moveErr error
			r.hook = func() { moveErr = tc.move(q, claimed) }
			n, err := q.eraseUser(ctx, "u1", false)
			if err != nil || moveErr != nil {
				t.Fatalf("erase: %v; worker move: %v", err, moveErr)
			}
			if got := redisHolds(r.mockRedis, "u1-secret"); len(got) != 0 {
				t.Errorf("the job a worker moved mid-sweep survives in %v (erased %d)", got, n)
			}
		})
	}
}

// The erasure read the processing hash, the victim's claim finished, and
// another user's job took the same field before the delete. The delete was
// unconditional and removed the bystander's claim (2026-10-31 review). It
// now deletes a field only while it holds the value the erasure read, with
// the atomic capability or without it.
func TestEraseKeepsFieldReusedMidSweep(t *testing.T) {
	ctx := context.Background()
	for _, plain := range []bool{false, true} {
		r := &raceRedis{mockRedis: newMockRedis(), key: "reuse-q:processing"}
		var client RedisClient = r
		if plain {
			client = plainRedis{r}
		}
		q := NewRedisQueue(client, "reuse-q")
		if err := q.Enqueue(ctx, secretJob("x", "u1")); err != nil {
			t.Fatal(err)
		}
		if _, err := q.Dequeue(ctx); err != nil {
			t.Fatal(err)
		}
		bystander, _ := json.Marshal(processingEntry{Job: string(mustJSON(t, secretJob("x", "u2")))})
		r.hook = func() {
			r.mu.Lock()
			r.hashes["reuse-q:processing"]["x"] = string(bystander)
			r.mu.Unlock()
		}
		if _, err := q.eraseUser(ctx, "u1", false); err != nil {
			t.Fatalf("plain=%v: erase: %v", plain, err)
		}
		if got := redisHolds(r.mockRedis, "u2-secret"); len(got) != 1 {
			t.Errorf("plain=%v: the bystander's claim on the reused field is in %v, want it kept", plain, got)
		}
		_ = q.Close()
	}
}

// refillRedis puts a fresh copy of the victim's job back on the main list
// every time the erasure reads it, as a stream of workers would.
type refillRedis struct {
	*mockRedis
	raw string
}

func (r *refillRedis) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	out, err := r.mockRedis.LRange(ctx, key, start, stop)
	if key == "busy-q" {
		_ = r.mockRedis.LPush(ctx, key, r.raw)
	}
	return out, err
}

// A sweep that keeps finding the user's jobs gives up with an error
// instead of reporting success over the copy it could not catch.
func TestEraseFailsWhileJobsKeepMoving(t *testing.T) {
	ctx := context.Background()
	r := &refillRedis{mockRedis: newMockRedis(), raw: string(mustJSON(t, secretJob("v1", "u1")))}
	q := NewRedisQueue(r, "busy-q")
	t.Cleanup(func() { _ = q.Close() })
	if _, err := q.eraseUser(ctx, "u1", false); err == nil {
		t.Fatal("the erasure reported success with the user's job still arriving")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
