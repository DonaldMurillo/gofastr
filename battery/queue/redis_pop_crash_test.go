package queue

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// A worker process that dies inside Dequeue (a deploy, an OOM kill) runs
// none of Dequeue's recovery writes. crashAt stands in for that death: at
// the chosen call it ends the calling goroutine with runtime.Goexit, so
// nothing after the call runs. The job must still be found by Reclaim on a
// restarted queue, once, and never sit on no list at all.
//
// crashAt runs the queue's paths for a client without scripts; crashScripts
// adds Eval for the scripted ones.
type crashAt struct {
	*mockRedis
	noScripts
	step  string // "pop#<n>", "HSet <key>", "HDel <key>" or "Eval <destination key>"
	pops  int
	fired bool
}

// crashScripts is crashAt with Eval: the step "Eval <key>" dies before a
// move script whose destination is key runs.
type crashScripts struct{ *crashAt }

func (c crashScripts) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if len(keys) > 1 {
		c.die("Eval " + keys[1])
	}
	return c.mockRedis.Eval(ctx, script, keys, args...)
}

// client is c for the paths without scripts, or c with Eval.
func (c *crashAt) client(scripts bool) RedisClient {
	if scripts {
		return crashScripts{c}
	}
	return c
}

// Each wrapper that hides mockRedis's Eval behind noScripts is no
// ScriptRunner, so its test covers the paths of a client without scripts.
func TestNoScriptsHidesEval(t *testing.T) {
	for _, c := range []RedisClient{
		&crashAt{}, &failingWrites{}, lostPromoteRedis{}, mainPushFailRedis{}, mainPushDiesRedis{},
	} {
		if _, ok := c.(ScriptRunner); ok {
			t.Errorf("%T runs scripts", c)
		}
	}
	for _, c := range []RedisClient{crashScripts{&crashAt{}}, &failingScripts{&failingWrites{}}} {
		if _, ok := c.(ScriptRunner); !ok {
			t.Errorf("%T does not run scripts", c)
		}
	}
}

func (c *crashAt) die(step string) {
	if !c.fired && c.step == step {
		c.fired = true
		runtime.Goexit()
	}
}

func (c *crashAt) pop() {
	c.pops++
	c.die("pop#" + strconv.Itoa(c.pops))
}

func (c *crashAt) RPop(ctx context.Context, key string) (string, error) {
	c.pop()
	return c.mockRedis.RPop(ctx, key)
}

func (c *crashAt) RPopHSet(ctx context.Context, list, hash, field string) (string, error) {
	c.pop()
	return c.mockRedis.RPopHSet(ctx, list, hash, field)
}

func (c *crashAt) HSet(ctx context.Context, key string, values ...any) error {
	c.die("HSet " + key)
	return c.mockRedis.HSet(ctx, key, values...)
}

func (c *crashAt) HDel(ctx context.Context, key string, fields ...string) error {
	c.die("HDel " + key)
	return c.mockRedis.HDel(ctx, key, fields...)
}

// dieInDequeue runs one Dequeue on q in a goroutine of its own, which the
// crash ends, and fails the test if the crash never fired.
func dieInDequeue(t *testing.T, c *crashAt, q *RedisQueue, types ...string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = q.Dequeue(context.Background(), types...)
	}()
	<-done
	if !c.fired {
		t.Fatalf("the worker never died at %s", c.step)
	}
}

// restarted is the queue a new worker process opens on the same Redis, its
// clock at *now.
func restarted(r *mockRedis, now *time.Time) *RedisQueue {
	q := NewRedisQueue(r, "test")
	q.SetVisibilityTimeout(time.Minute)
	q.now = func() time.Time { return *now }
	return q
}

func mustDequeueID(t *testing.T, q *RedisQueue, want string, types ...string) {
	t.Helper()
	job, err := q.Dequeue(context.Background(), types...)
	if err != nil {
		t.Fatalf("dequeue %s: %v", want, err)
	}
	if job.ID != want {
		t.Fatalf("dequeued %q, want %q", job.ID, want)
	}
}

func mustBeEmpty(t *testing.T, q *RedisQueue, types ...string) {
	t.Helper()
	if job, err := q.Dequeue(context.Background(), types...); !errors.Is(err, ErrNoJob) {
		t.Fatalf("dequeue: %q, %v; want ErrNoJob (a second copy)", job.ID, err)
	}
}

func TestDeathAfterPopKeepsJob(t *testing.T) {
	t.Run("plain", func(t *testing.T) { deathAfterPop(t, false, "HSet test:processing") })
	t.Run("scripts", func(t *testing.T) { deathAfterPop(t, true, "Eval test:processing") })
}

func deathAfterPop(t *testing.T, scripts bool, step string) {
	r := newMockRedis()
	now := time.Now()
	c := &crashAt{mockRedis: r, step: step}
	q := restarted(r, &now)
	q.client = c.client(scripts)
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j", Type: "x"}); err != nil {
		t.Fatal(err)
	}
	dieInDequeue(t, c, q)

	q2 := restarted(r, &now)
	// Inside the lease the record is left alone: a live Dequeue may hold it.
	if n, err := q2.Reclaim(ctx); err != nil || n != 0 {
		t.Fatalf("reclaim inside the lease = %d, %v; want 0, nil", n, err)
	}
	mustBeEmpty(t, q2)
	now = now.Add(2 * time.Minute)
	if n, err := q2.Reclaim(ctx); err != nil || n != 1 {
		t.Fatalf("reclaim = %d, %v; want 1, nil", n, err)
	}
	mustDequeueID(t, q2, "j")
	mustBeEmpty(t, q2)
}

func TestDeathWhileSkippingKeepsJob(t *testing.T) {
	t.Run("plain", func(t *testing.T) { deathWhileSkipping(t, false) })
	t.Run("scripts", func(t *testing.T) { deathWhileSkipping(t, true) })
}

func deathWhileSkipping(t *testing.T, scripts bool) {
	r := newMockRedis()
	now := time.Now()
	c := &crashAt{mockRedis: r, step: "pop#2"}
	q := restarted(r, &now)
	q.client = c.client(scripts)
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "a", Type: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, Job{ID: "b", Type: "b"}); err != nil {
		t.Fatal(err)
	}
	// The worker wants "b", pops "a" first and holds it, and dies popping
	// "b".
	dieInDequeue(t, c, q, "b")

	q2 := restarted(r, &now)
	now = now.Add(2 * time.Minute)
	if _, err := q2.Reclaim(ctx); err != nil {
		t.Fatal(err)
	}
	mustDequeueID(t, q2, "a", "a")
	mustDequeueID(t, q2, "b", "b")
	mustBeEmpty(t, q2)
}

func TestDeathAfterClaimDeliversOnce(t *testing.T) {
	r := newMockRedis()
	now := time.Now()
	c := &crashAt{mockRedis: r, step: "HDel test:popped"}
	q := restarted(r, &now)
	q.client = c
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	if err := q.Enqueue(ctx, Job{ID: "j", Type: "x"}); err != nil {
		t.Fatal(err)
	}
	// The claim reached the processing hash; the worker dies retiring the
	// pop record.
	dieInDequeue(t, c, q)

	q2 := restarted(r, &now)
	now = now.Add(2 * time.Minute)
	if n, err := q2.Reclaim(ctx); err != nil || n != 1 {
		t.Fatalf("reclaim = %d, %v; want 1, nil", n, err)
	}
	mustDequeueID(t, q2, "j")
	mustBeEmpty(t, q2)
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.hashes["test:popped"]); n != 0 {
		t.Fatalf("%d pop records left behind", n)
	}
}

func TestClaimLeavesNoPopRecord(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: id}); err != nil {
			t.Fatal(err)
		}
	}
	// "c" is claimed after "a" and "b" were popped and put back.
	mustDequeueID(t, q, "c", "c")
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.hashes["test:popped"]); n != 0 {
		t.Fatalf("%d pop records left behind: %v", n, r.hashes["test:popped"])
	}
	if n := len(r.lists["test"]); n != 2 {
		t.Fatalf("main list holds %d, want the 2 skipped jobs", n)
	}
}

// failingWrites is mockRedis, PopRecorder included, with the chosen writes
// failing. Its HDel honours a cancelled context, as a real client does.
type failingWrites struct {
	*mockRedis
	noScripts
	hsetFail bool
	pushFail string // the list whose LPush fails
}

// failingScripts is failingWrites with Eval: a move script fails where the
// write it stands for would.
type failingScripts struct{ *failingWrites }

func (f *failingScripts) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if script == moveScript && ((f.hsetFail && keys[1] == "test:processing") || keys[1] == f.pushFail) {
		return nil, errors.New("script down")
	}
	return f.mockRedis.Eval(ctx, script, keys, args...)
}

func (f *failingWrites) HSet(ctx context.Context, key string, values ...any) error {
	if f.hsetFail && key == "test:processing" {
		return errors.New("hset down")
	}
	return f.mockRedis.HSet(ctx, key, values...)
}

func (f *failingWrites) LPush(ctx context.Context, key string, values ...any) error {
	if key == f.pushFail {
		return errors.New("push down")
	}
	return f.mockRedis.LPush(ctx, key, values...)
}

func (f *failingWrites) HDel(ctx context.Context, key string, fields ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.mockRedis.HDel(ctx, key, fields...)
}

// Every way Dequeue gives a popped entry its next home retires the pop
// record; one left behind would come back on the main list a second time.
func TestHomedEntryLeavesNoPopRecord(t *testing.T) {
	exhausted := Job{ID: "j", Type: "x", Attempts: 3, MaxAttempts: 3}
	cases := []struct {
		name       string
		job        *Job // nil: a malformed entry
		f          failingWrites
		cancelled  bool
		main, dead int
	}{
		{name: "claimed", job: &Job{ID: "j", Type: "x"}},
		{name: "claimed, caller cancelled", job: &Job{ID: "j", Type: "x"}, cancelled: true},
		{name: "processing write failed", job: &Job{ID: "j", Type: "x"}, f: failingWrites{hsetFail: true}, main: 1},
		{name: "dead-lettered", job: &exhausted, dead: 1},
		{name: "dead-letter push failed", job: &exhausted, f: failingWrites{pushFail: "test:dead"}, main: 1},
		{name: "malformed", dead: 1},
		{name: "quarantine failed", f: failingWrites{pushFail: "test:dead"}, main: 1},
	}
	for _, tc := range cases {
		for _, scripts := range []bool{false, true} {
			name := tc.name
			if scripts {
				name += ", scripts"
			}
			t.Run(name, func(t *testing.T) { homedEntry(t, tc.job, tc.f, tc.cancelled, scripts, tc.main, tc.dead) })
		}
	}
}

// homedEntry dequeues job (nil: a malformed entry) through f, with Eval
// when scripts, and checks no pop record is left and the main and dead
// lists hold wantMain and wantDead entries.
func homedEntry(t *testing.T, job *Job, f failingWrites, cancelled, scripts bool, wantMain, wantDead int) {
	r := newMockRedis()
	f.mockRedis = r
	var c RedisClient = &f
	if scripts {
		c = &failingScripts{&f}
	}
	q := NewRedisQueue(c, "test")
	t.Cleanup(func() { _ = q.Close() })
	if job != nil {
		if err := q.Enqueue(context.Background(), *job); err != nil {
			t.Fatal(err)
		}
	} else {
		_ = r.LPush(context.Background(), "test", "{not json")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if cancelled {
		cancel()
	}
	defer cancel()
	_, _ = q.Dequeue(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.hashes["test:popped"]); n != 0 {
		t.Fatalf("%d pop records left behind", n)
	}
	if m, d := len(r.lists["test"]), len(r.lists["test:dead"]); m != wantMain || d != wantDead {
		t.Fatalf("main %d, dead %d; want %d, %d", m, d, wantMain, wantDead)
	}
}

// A pop record whose field holds no time is put back at once rather than
// kept for good.
func TestBadPopFieldGoesBack(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	t.Cleanup(func() { _ = q.Close() })
	_ = r.HSet(context.Background(), "test:popped", "junk", `{"id":"j","type":"x","max_attempts":3}`)
	if n, err := q.Reclaim(context.Background()); err != nil || n != 1 {
		t.Fatalf("reclaim = %d, %v; want 1, nil", n, err)
	}
	mustDequeueID(t, q, "j")
}

// A corrupt processing entry under a recorded job's ID does not stop
// Reclaim: the processing scan still quarantines it, and the record goes
// back on the next pass.
func TestCorruptClaimDoesNotStopReclaim(t *testing.T) {
	r := newMockRedis()
	q := NewRedisQueue(r, "test")
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	_ = r.HSet(ctx, "test:popped", "junk", `{"id":"j","type":"x","max_attempts":3}`)
	_ = r.HSet(ctx, "test:processing", "j", "{corrupt")
	if _, err := q.Reclaim(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if n, err := q.Reclaim(ctx); err != nil || n != 1 {
		t.Fatalf("second reclaim = %d, %v; want 1, nil", n, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := len(r.lists["test:dead"]); d != 1 {
		t.Fatalf("dead list holds %d, want the corrupt entry", d)
	}
}

// bareClient is mockRedis behind the bare RedisClient interface: no
// PopRecorder, so Dequeue pops with RPop and leaves no record.
type bareClient struct {
	RedisClient
	poppedDels int
}

func (b *bareClient) HDel(ctx context.Context, key string, fields ...string) error {
	if key == "test:popped" {
		b.poppedDels++
	}
	return b.RedisClient.HDel(ctx, key, fields...)
}

// Without PopRecorder there is no record, and no delete is sent for one.
func TestBareClientSendsNoRecordDelete(t *testing.T) {
	b := &bareClient{RedisClient: newMockRedis()}
	q := NewRedisQueue(b, "test")
	t.Cleanup(func() { _ = q.Close() })
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := q.Enqueue(ctx, Job{ID: id, Type: id}); err != nil {
			t.Fatal(err)
		}
	}
	mustDequeueID(t, q, "b", "b")
	if b.poppedDels != 0 {
		t.Fatalf("%d deletes sent to the pop hash", b.poppedDels)
	}
}
