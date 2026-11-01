package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptClient is a client that records pops and runs scripts:
// mockRedis, or liveRedis against a real server.
type scriptClient interface {
	RedisClient
	PopRecorder
	ScriptRunner
}

// eraseMid wraps a scriptClient behind the bare RedisClient embed, which
// hides CompareAndMover and CompareAndDeleter, so the moves without them
// are the ones under test. After a named call returns, its hook runs once:
// the erasure landing while a worker holds what it just read. A call named
// in fail returns an error instead of running.
type eraseMid struct {
	RedisClient
	c     scriptClient
	hooks map[string]func() // "RPopHSet", "LRange <key>", "HGet <key>", "HGetAll <key>", "LPush <key>"
	fail  map[string]bool   // "HSet <key>", "Eval <destination key>"
}

func newEraseMid(c scriptClient) *eraseMid {
	return &eraseMid{RedisClient: c, c: c, hooks: map[string]func(){}, fail: map[string]bool{}}
}

func (e *eraseMid) after(op string) {
	if h := e.hooks[op]; h != nil {
		delete(e.hooks, op)
		h()
	}
}

func (e *eraseMid) RPopHSet(ctx context.Context, list, hash, field string) (string, error) {
	v, err := e.c.RPopHSet(ctx, list, hash, field)
	e.after("RPopHSet")
	return v, err
}

func (e *eraseMid) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if script != eraseScript && len(keys) > 1 && e.fail["Eval "+keys[1]] {
		return nil, errors.New("eval down")
	}
	return e.c.Eval(ctx, script, keys, args...)
}

func (e *eraseMid) HSet(ctx context.Context, key string, values ...any) error {
	if e.fail["HSet "+key] {
		return errors.New("hset down")
	}
	return e.c.HSet(ctx, key, values...)
}

func (e *eraseMid) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	v, err := e.c.LRange(ctx, key, start, stop)
	e.after("LRange " + key)
	return v, err
}

func (e *eraseMid) HGet(ctx context.Context, key, field string) (string, error) {
	v, err := e.c.HGet(ctx, key, field)
	e.after("HGet " + key)
	return v, err
}

func (e *eraseMid) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	v, err := e.c.HGetAll(ctx, key)
	e.after("HGetAll " + key)
	return v, err
}

func (e *eraseMid) LPush(ctx context.Context, key string, values ...any) error {
	err := e.c.LPush(ctx, key, values...)
	e.after("LPush " + key)
	return err
}

// midQueue is a queue named name over e, its clock at *now, with a
// one-minute visibility timeout. Its keys are deleted at cleanup.
func midQueue(t *testing.T, e *eraseMid, name string) (*RedisQueue, *time.Time) {
	t.Helper()
	q := NewRedisQueue(e, name)
	t.Cleanup(func() {
		_ = q.Close()
		_ = e.c.Del(context.Background(), name, name+":processing", name+":dead", name+":delayed", name+":popped")
	})
	now := time.Now()
	q.now = func() time.Time { return now }
	q.SetVisibilityTimeout(time.Minute)
	return q, &now
}

// eraseU1 is a hook that erases u1's jobs from q.
func eraseU1(t *testing.T, q *RedisQueue) func() {
	return func() {
		if _, err := q.eraseUser(context.Background(), "u1", false); err != nil {
			t.Errorf("erase: %v", err)
		}
	}
}

func mustEnqueue(t *testing.T, q *RedisQueue, jobs ...Job) {
	t.Helper()
	for _, j := range jobs {
		if err := q.Enqueue(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
}

// eraseMidCases each run a queue step with an erasure of u1's jobs landing
// between the step's read and its write, the shape the 2026-10-31 review
// found in Dequeue: it held a popped job in memory with only its pop
// record left in Redis, an erasure deleted the record, and Dequeue then
// wrote the job to the processing hash. The erased job was back. Every
// move between keys is now one script that writes only while the source
// still holds the entry. Each case must leave the queue holding none of
// u1's jobs.
var eraseMidCases = []struct {
	name string
	run  func(t *testing.T, e *eraseMid, q *RedisQueue, now *time.Time, name string)
}{
	{"claim", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, _ string) {
		mustEnqueue(t, q, secretJob("a", "u1"), secretJob("b", "u2"))
		e.hooks["RPopHSet"] = eraseU1(t, q)
		got, err := q.Dequeue(context.Background())
		if err != nil || got.ID != "b" {
			t.Fatalf("Dequeue = %q, %v; want u2's job b", got.ID, err)
		}
	}},
	// A type-miss job goes back on the main list once Dequeue gives up.
	{"skip requeue", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, _ string) {
		mustEnqueue(t, q, secretJob("a", "u1"))
		e.hooks["RPopHSet"] = eraseU1(t, q)
		if _, err := q.Dequeue(context.Background(), "other"); !errors.Is(err, ErrNoJob) {
			t.Fatalf("Dequeue = %v, want ErrNoJob", err)
		}
	}},
	// An exhausted job goes to the dead list from Dequeue's memory.
	{"dead letter", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, _ string) {
		j := secretJob("a", "u1")
		j.Attempts, j.MaxAttempts = 3, 3
		mustEnqueue(t, q, j)
		e.hooks["RPopHSet"] = eraseU1(t, q)
		if _, err := q.Dequeue(context.Background()); !errors.Is(err, ErrNoJob) {
			t.Fatalf("Dequeue = %v, want ErrNoJob", err)
		}
	}},
	// A claim whose processing write failed goes back on the main list.
	{"failed claim restore", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, name string) {
		mustEnqueue(t, q, secretJob("a", "u1"))
		e.fail["HSet "+name+":processing"] = true
		e.fail["Eval "+name+":processing"] = true
		e.hooks["RPopHSet"] = eraseU1(t, q)
		if _, err := q.Dequeue(context.Background()); err == nil {
			t.Fatal("Dequeue hid the failed processing write")
		}
	}},
	// Nack read the claim; its push (no CompareAndMover) came after.
	{"nack", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, name string) {
		mustEnqueue(t, q, secretJob("a", "u1"))
		claimed, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		e.hooks["HGet "+name+":processing"] = eraseU1(t, q)
		if err := q.Nack(context.Background(), claimed); err != nil {
			t.Fatalf("nack: %v", err)
		}
	}},
	// Reclaim read an expired claim, then re-delivered it.
	{"reclaim", func(t *testing.T, e *eraseMid, q *RedisQueue, now *time.Time, name string) {
		mustEnqueue(t, q, secretJob("a", "u1"))
		if _, err := q.Dequeue(context.Background()); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Hour)
		e.hooks["HGetAll "+name+":processing"] = eraseU1(t, q)
		if _, err := q.Reclaim(context.Background()); err != nil {
			t.Fatalf("reclaim: %v", err)
		}
	}},
	// Reclaim read a stale pop record, then put its job back.
	{"pop put-back", func(t *testing.T, e *eraseMid, q *RedisQueue, now *time.Time, name string) {
		if err := e.c.HSet(context.Background(), name+":popped", "1:tok", string(mustJSON(t, secretJob("a", "u1")))); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Hour)
		e.hooks["HGetAll "+name+":popped"] = eraseU1(t, q)
		if _, err := q.Reclaim(context.Background()); err != nil {
			t.Fatalf("reclaim: %v", err)
		}
	}},
	// Replay read the dead list, then pushed the job onto the main list.
	{"replay", func(t *testing.T, e *eraseMid, q *RedisQueue, _ *time.Time, name string) {
		j := secretJob("a", "u1")
		j.MaxAttempts = 1
		mustEnqueue(t, q, j)
		claimed, err := q.Dequeue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Nack(context.Background(), claimed); err != nil {
			t.Fatal(err)
		}
		e.hooks["LRange "+name+":dead"] = eraseU1(t, q)
		if err := q.Replay(context.Background(), "a"); err != nil {
			t.Fatalf("replay: %v", err)
		}
	}},
	// promoteDue read a due job, then pushed it onto the main list. It
	// took the push back once the delayed list showed the entry gone, but
	// a second worker could claim the job first.
	{"promote", func(t *testing.T, e *eraseMid, q *RedisQueue, now *time.Time, name string) {
		j := secretJob("a", "u1")
		j.ScheduledAt = now.Add(time.Minute)
		mustEnqueue(t, q, j)
		*now = now.Add(time.Hour)
		e.hooks["LRange "+name+":delayed"] = eraseU1(t, q)
		e.hooks["LPush "+name] = func() { _, _ = q.Dequeue(context.Background()) }
		if _, err := q.Dequeue(context.Background()); !errors.Is(err, ErrNoJob) {
			t.Fatalf("Dequeue = %v, want ErrNoJob", err)
		}
	}},
}

// runEraseMid runs every case on a queue of its own over the client c
// returns.
func runEraseMid(t *testing.T, c func(*testing.T) scriptClient) {
	for _, tc := range eraseMidCases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEraseMid(c(t))
			name := "mid-" + redisRandomID()
			q, now := midQueue(t, e, name)
			tc.run(t, e, q, now, name)
			if n, err := q.eraseUser(context.Background(), "u1", true); err != nil || n != 0 {
				t.Errorf("u1's job is back after its erasure: %d entries, %v", n, err)
			}
		})
	}
}

func TestEraseMidMoveDropsJob(t *testing.T) {
	runEraseMid(t, func(*testing.T) scriptClient { return newMockRedis() })
}

// The same cases on a real Redis, running moveScript itself.
func TestEraseMidMoveOnLiveRedis(t *testing.T) {
	runEraseMid(t, func(t *testing.T) scriptClient { return dialLiveRedis(t) })
}

// A scripted promote that fails leaves the due job on the delayed list.
func TestScriptPromoteFailKeepsJob(t *testing.T) {
	m := newMockRedis()
	e := newEraseMid(m)
	q, now := midQueue(t, e, "test")
	j := secretJob("a", "u1")
	j.ScheduledAt = now.Add(time.Minute)
	mustEnqueue(t, q, j)
	*now = now.Add(time.Hour)
	e.fail["Eval test"] = true
	if _, err := q.Dequeue(context.Background()); err == nil {
		t.Fatal("Dequeue hid the failed promote")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, l := len(m.lists["test:delayed"]), len(m.lists["test"]); d != 1 || l != 0 {
		t.Fatalf("delayed %d, main %d; want 1, 0", d, l)
	}
}
