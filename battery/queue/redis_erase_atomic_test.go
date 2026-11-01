package queue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// chaseRedis moves the victim's job into each key right after the erasure
// reads it, the worst order a stream of workers can produce: every snapshot
// misses the job, and a sweep that reads key by key finds nothing.
type chaseRedis struct {
	*mockRedis
	queue string
	job   string // the victim's job, as json.Marshal wrote it
}

// moveInto takes the victim's job off every key and puts it on key.
func (r *chaseRedis) moveInto(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, l := range r.lists {
		r.lists[k] = slicesDeleteValue(l, r.job)
	}
	for _, h := range r.hashes {
		for f, v := range h {
			if strings.Contains(v, "u1-secret") {
				delete(h, f)
			}
		}
	}
	switch key {
	case r.queue + ":processing":
		entry, _ := json.Marshal(processingEntry{Job: r.job})
		r.hashes[key] = map[string]string{"v1": string(entry)}
	case r.queue + ":popped":
		r.hashes[key] = map[string]string{"1:tok": r.job}
	default:
		r.lists[key] = append([]string{r.job}, r.lists[key]...)
	}
}

func slicesDeleteValue(l []string, v string) []string {
	out := l[:0]
	for _, s := range l {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

func (r *chaseRedis) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	out, err := r.mockRedis.LRange(ctx, key, start, stop)
	r.moveInto(key)
	return out, err
}

func (r *chaseRedis) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	out, err := r.mockRedis.HGetAll(ctx, key)
	r.moveInto(key)
	return out, err
}

// plainRedis hides the optional capabilities: the RedisClient methods only.
type plainRedis struct{ RedisClient }

func secretJob(id, user string) Job {
	return Job{ID: id, Type: "mail", UserID: user, Payload: json.RawMessage(`{"to":"` + user + `-secret"}`)}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A sweep that reads one key at a time proves nothing by finding nothing:
// Dequeue and Nack can move the victim's job into each key just after the
// sweep read it, so every pass came up empty and the erasure returned nil
// with the job still queued (2026-10-31 review). The erasure is one Lua
// script now, which Redis runs with no other command in between.
func TestEraseSweepIsOneAtomicStep(t *testing.T) {
	ctx := context.Background()
	r := &chaseRedis{mockRedis: newMockRedis(), queue: "chase-q", job: string(mustJSON(t, secretJob("v1", "u1")))}
	q := NewRedisQueue(r, "chase-q")
	t.Cleanup(func() { _ = q.Close() })
	r.moveInto("chase-q")

	n, err := q.eraseUser(ctx, "u1", false)
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if got := redisHolds(r.mockRedis, "u1-secret"); len(got) != 0 || n != 1 {
		t.Errorf("erased %d, and the job survives in %v; want 1 erased and none left", n, got)
	}
}

// hgetSwapRedis lets another user's job take a processing field between
// the read that checked it and the delete.
type hgetSwapRedis struct {
	*mockRedis
	swap func()
}

func (r *hgetSwapRedis) HGet(ctx context.Context, key, field string) (string, error) {
	v, err := r.mockRedis.HGet(ctx, key, field)
	if r.swap != nil {
		r.swap()
		r.swap = nil
	}
	return v, err
}

// Without an atomic compare-and-delete the erasure read a processing field
// again and then deleted it in a second round trip; a claim another user's
// job took in between was deleted with it (2026-10-31 review). A client that
// cannot run the erase script now gets an error and nothing is deleted.
func TestEraseRefusesWithoutScripts(t *testing.T) {
	ctx := context.Background()
	r := &hgetSwapRedis{mockRedis: newMockRedis()}
	q := NewRedisQueue(plainRedis{r}, "plain-q")
	t.Cleanup(func() { _ = q.Close() })
	if err := q.Enqueue(ctx, secretJob("x", "u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatal(err)
	}
	bystander, _ := json.Marshal(processingEntry{Job: string(mustJSON(t, secretJob("x", "u2")))})
	r.swap = func() {
		r.mu.Lock()
		r.hashes["plain-q:processing"]["x"] = string(bystander)
		r.mu.Unlock()
	}

	_, err := q.eraseUser(ctx, "u1", false)
	r.mu.Lock()
	_, held := r.hashes["plain-q:processing"]["x"]
	r.mu.Unlock()
	if !held {
		t.Error("the claim on the processing field was deleted; it was the bystander's by then")
	}
	if err == nil || !strings.Contains(err.Error(), "ScriptRunner") {
		t.Errorf("erase with a client that cannot run scripts: err = %v, want a ScriptRunner refusal", err)
	}
	if _, err := q.eraseUser(ctx, "u1", true); err == nil {
		t.Error("the dry run reported a count the erasure cannot deliver")
	}
}

// json.Marshal stores both "\xff" and "\xfe" as "�", so the queue
// cannot tell those users' jobs apart: an erasure for one would take the
// other's. It refuses instead.
func TestEraseRefusesInvalidUTF8ID(t *testing.T) {
	ctx := context.Background()
	q := NewRedisQueue(newMockRedis(), "utf8-q")
	t.Cleanup(func() { _ = q.Close() })
	if err := q.Enqueue(ctx, secretJob("x", "\xfe")); err != nil {
		t.Fatal(err)
	}
	if n, err := q.eraseUser(ctx, "\xff", false); err == nil || n != 0 {
		t.Errorf("erase of an invalid UTF-8 id = %d, %v; want a refusal", n, err)
	}
	if n, err := q.eraseUser(ctx, "\xfe", true); err == nil {
		t.Errorf("dry run of an invalid UTF-8 id counted %d with no error", n)
	}
}
