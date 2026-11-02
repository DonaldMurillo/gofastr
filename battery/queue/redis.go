package queue

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// RedisClient defines the minimal Redis operations needed by RedisQueue.
// This is an interface so callers can inject any Redis client (go-redis, redigo, etc.)
// without this package importing a specific driver.
type RedisClient interface {
	LPush(ctx context.Context, key string, values ...any) error
	RPop(ctx context.Context, key string) (string, error)
	// HGet returns the value stored at field in the hash at key. Like RPop,
	// a missing hash or field MUST be reported as ErrRedisEmpty (map the
	// driver's nil-sentinel, e.g. redis.Nil), the queue relies on it to
	// make Ack/Nack of a non-claimed job an idempotent no-op instead of a
	// hard error: a missing claim is normal (already completed, or the
	// lease expired and another worker took it) and must not be mistaken
	// for a backend outage.
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, values ...any) error
	// HGetAll returns every field→value pair in the hash. Used by Reclaim to
	// scan the processing hash for expired in-flight jobs.
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	// Del removes one or more keys entirely. RedisQueue's own lease
	// protocol never calls it, per-job bookkeeping uses HDel, but it
	// stays in the adapter contract for callers that hold a RedisClient
	// and need whole-key cleanup.
	Del(ctx context.Context, keys ...string) error
	// LRange returns the elements of the list at key in the inclusive range
	// [start, stop]; negative indices count from the tail (-1 is the last
	// element). Used by Replay to read the dead-letter list.
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	// LRem removes up to count occurrences of value from the list at key and
	// returns the number removed. Used by Replay to pull one entry off the
	// dead-letter list.
	LRem(ctx context.Context, key string, count int64, value any) (int64, error)
}

// CompareAndDeleter is the optional capability a RedisClient implements when
// it can delete a hash field atomically, and only while that field still
// holds an expected value. RedisQueue uses it to retire a processing entry:
// Ack/Nack read the entry, compare its ClaimToken in process, and then
// delete — and nothing binds the delete to the entry whose token was
// compared. One round trip of skew (a GC pause, a Redis failover) is enough
// for the lease to expire, Reclaim to run, and another worker to re-claim,
// at which point the unconditional delete removes the NEW claimant's entry:
// the job is then on no list, invisible to Reclaim, and silently lost.
//
// Implement it with a Lua script (or WATCH/MULTI) — go-redis:
//
//	if redis.call('HGET', KEYS[1], ARGV[1]) == ARGV[2] then
//	  return redis.call('HDEL', KEYS[1], ARGV[1]) end
//	return 0
//
// It returns whether the field was deleted; false means the value had
// changed, which the queue treats as a fenced no-op.
//
// A client that does not implement it still works: RedisQueue falls back to
// re-reading immediately before the delete, which narrows the window but
// cannot close it.
type CompareAndDeleter interface {
	HDelIfEqual(ctx context.Context, key, field, expect string) (bool, error)
}

// CompareAndMover is the optional capability a RedisClient implements when
// it can move a claimed job in one atomic step: push value onto list and
// delete the hash field, only while that field still holds an expected
// value. RedisQueue uses it for the requeues Nack and Reclaim decide from
// a snapshot of the processing entry (a retry push, a dead-letter push, a
// lease-expiry re-delivery). Checking the claim and then pushing in
// separate round trips lets the claim change in between: the lease
// expires, another worker re-claims, and the stale push lands a second
// copy on the main list. A third worker then claims that copy, consumes
// the same attempt as the current claimant, and overwrites its processing
// entry. [CompareAndDeleter] cannot close this: the push has already
// happened by the time the conditional delete refuses.
//
// Implement it with a Lua script — go-redis:
//
//	if redis.call('HGET', KEYS[1], ARGV[1]) == ARGV[2] then
//	  redis.call('LPUSH', KEYS[2], ARGV[3])
//	  redis.call('HDEL', KEYS[1], ARGV[1])
//	  return 1 end
//	return 0
//
// key and field name the processing entry, list the destination. On Redis
// Cluster a script may only touch keys in one slot, so give the queue a
// hash-tagged name ("{jobs}") so its lists and hash share one. It returns
// whether the move happened; false means the entry had changed, which the
// queue treats as a fenced no-op.
//
// A client that does not implement it still works: RedisQueue pushes and
// then releases the claim with the conditional delete, which leaves the
// window above open.
type CompareAndMover interface {
	MoveIfEqual(ctx context.Context, key, field, expect, list, value string) (bool, error)
}

// PopRecorder is the optional capability a RedisClient implements when it
// can pop the tail of a list and record the popped value in a hash in one
// atomic step. Dequeue takes a job off the main list and writes it to the
// processing hash in separate round trips, and a worker that dies between
// them (a deploy, an OOM kill) leaves the job on neither: Reclaim cannot
// see it, and it is lost. With PopRecorder the pop itself leaves a record
// of the job in the queue's pop hash ("<queue>:popped") under a field
// holding the pop time. Dequeue deletes the record once the job has its
// next home (the processing hash, the dead-letter list, or back on the
// main list), and Reclaim puts a record older than the visibility timeout
// back on the main list.
//
// Implement it with a Lua script — go-redis:
//
//	local v = redis.call('RPOP', KEYS[1])
//	if not v then return false end
//	redis.call('HSET', KEYS[2], ARGV[1], v)
//	return v
//
// list is the list to pop, hash and field where the record goes. An empty
// list MUST be reported as ErrRedisEmpty, as for RPop (go-redis returns
// redis.Nil for the script's false). On Redis Cluster give the queue a
// hash-tagged name, as for [CompareAndMover].
//
// A client that does not implement it still works: Dequeue pops with RPop,
// and a worker that dies between the pop and the processing write loses
// the job.
type PopRecorder interface {
	RPopHSet(ctx context.Context, list, hash, field string) (string, error)
}

// ScriptRunner is the capability RedisQueue needs to erase a user's jobs
// (App.EraseUserData reaches the queue through its store eraser): run a Lua
// script with EVAL and return its reply. The queue owns the script; the
// adapter only passes it through. go-redis:
//
//	func (c adapter) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
//		return c.rdb.Eval(ctx, script, keys, args...).Result()
//	}
//
// The erasure is one script because Redis runs a script with no other
// command in between. Read key by key, a worker's Dequeue or Nack can move
// a job into each key just after the sweep read it, so no number of
// passes proves the job gone; and a hash field read and then deleted in a
// second round trip can belong to another user's claim by then.
//
// The script names all five of the queue's keys, so on Redis Cluster give
// the queue a hash-tagged name ("{jobs}"), as for [CompareAndMover]. An
// integer reply MUST come back as an integer type (go-redis returns int64).
//
// With it, every move of a job from one of the queue's keys to another
// also runs as a script (moveScript) that writes only while the source
// still holds the entry: Dequeue's claim from its pop record into the
// processing hash, its dead-lettering and requeues, Nack and Reclaim
// without [CompareAndMover], Replay, and the promotion of due delayed
// jobs. A worker that read a job before an erasure removed it would
// otherwise write it back; with the script it drops the job instead.
//
// A client that does not implement it still runs the queue, but erasing a
// user's data from it fails with an error instead of sweeping key by key.
type ScriptRunner interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

// ErrRedisEmpty is the sentinel a RedisClient implementation returns (or
// wraps) when a read finds nothing: RPop on an empty list, HGet on a missing
// hash or field. It is how RedisQueue distinguishes a genuinely missing
// value from a real backend failure: "nothing there" is a normal signal (an
// empty poll → ErrNoJob; an Ack/Nack with nothing claimed → idempotent
// no-op), while any other error means the backend is unreachable and MUST be
// surfaced so an outage is not masked as idle.
//
// Adapter authors translating a real driver MUST map its own nil-sentinel
// (e.g. go-redis's redis.Nil, redigo's redis.ErrNil) onto this via errors.Is.
var ErrRedisEmpty = errors.New("redis: list is empty")

// RedisQueue implements the Queue interface backed by Redis lists and hashes.
// It supports a visibility timeout for in-flight jobs and a dead letter queue
// for jobs that exceed MaxAttempts.
type RedisQueue struct {
	client          RedisClient
	queueName       string
	processingQueue string
	deadLetterQueue string
	// delayedQueue holds jobs enqueued with a future ScheduledAt until
	// Dequeue promotes them onto the main list.
	delayedQueue string
	// poppedQueue is the hash a [PopRecorder] pop records each entry in
	// until the entry has its next home.
	poppedQueue string

	// visibilityTimeout is read by Dequeue and written by SetVisibilityTimeout
	// from independent goroutines, so it is stored as nanoseconds in an
	// atomic.Int64 to make that path race-free without a mutex on the hot
	// Dequeue path.
	visibilityTimeout atomic.Int64

	// now is the clock used for visibility-timeout stamps and expiry checks.
	// Defaults to time.Now; tests substitute a fake clock so reclaim
	// behaviour can be asserted without wall-clock sleeps.
	now func() time.Time

	// staleClaims counts completions (Ack/Nack) rejected because the claim
	// token they presented no longer matches the processing entry, i.e. the
	// job was re-claimed by another worker after this claimant's visibility
	// timeout expired. Atomic: workers on different goroutines complete
	// independently. Read via StaleClaimCount; a rising value means worker
	// handlers are outliving their visibility timeout.
	staleClaims atomic.Int64

	// eraseName is the name the store eraser is listed under, and
	// unregisterErase removes it;
	// Close calls it.
	eraseName       string
	unregisterErase func()
}

// StaleClaimCount returns how many Ack/Nack calls were rejected as coming
// from a stale claim (fenced out by Job.ClaimToken mismatch). Monitoring it
// distinguishes "handlers routinely exceed the visibility timeout" from
// silent double-delivery confusion.
func (q *RedisQueue) StaleClaimCount() int64 {
	return q.staleClaims.Load()
}

// NewRedisQueue creates a new Redis-backed queue.
func NewRedisQueue(client RedisClient, queueName string) *RedisQueue {
	q := &RedisQueue{
		client:          client,
		queueName:       queueName,
		processingQueue: queueName + ":processing",
		deadLetterQueue: queueName + ":dead",
		delayedQueue:    queueName + ":delayed",
		poppedQueue:     queueName + ":popped",
		now:             time.Now,
	}
	q.visibilityTimeout.Store(int64(30 * time.Second))
	// App.EraseUserData reaches the jobs a SQL eraser cannot: every entry
	// here is json.Marshal(job), its payload included.
	// Two queues of one name (on different servers) are both listed: the
	// later is queue_redis:<name>#2.
	q.eraseName, q.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "queue_redis:" + queueName,
		Source: "queue",
		Erase:  q.eraseUser,
	})
	return q
}

// eraseUser removes every job whose UserID is userID from the delayed,
// main and dead lists and the pop and processing hashes, and returns how
// many entries it removed (with dryRun, how many it would). It is one
// script (eraseScript), so no worker step lands between its reads and its
// deletes. A client that is not a [ScriptRunner] gets an error and nothing
// is removed. An entry that does not decode names nobody and stays. A
// worker still running an erased claim finds nothing to settle, and its
// Ack or Nack is a no-op.
func (q *RedisQueue) eraseUser(ctx context.Context, userID string, dryRun bool) (n int, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("redis queue: erase panicked: %s", textsafe.Recovered(v))
		}
	}()
	runner, ok := q.client.(ScriptRunner)
	if !ok {
		return 0, errors.New("redis queue: erasing a user's jobs needs a client that runs Lua scripts (queue.ScriptRunner); a key-by-key sweep cannot prove them gone")
	}
	// json.Marshal writes invalid UTF-8 as U+FFFD, so two such ids are
	// stored as one: matching either would erase the other's jobs.
	if !utf8.ValidString(userID) {
		return 0, errors.New("redis queue: erase: the user id is not valid UTF-8, and the queue stores ids as JSON, which cannot tell such ids apart")
	}
	token, err := json.Marshal(userID)
	if err != nil {
		return 0, fmt.Errorf("redis queue: erase: %w", err)
	}
	dry := "0"
	if dryRun {
		dry = "1"
	}
	reply, err := runner.Eval(ctx, eraseScript,
		[]string{q.poppedQueue, q.processingQueue, q.delayedQueue, q.queueName, q.deadLetterQueue},
		string(token), dry)
	if err != nil {
		return 0, fmt.Errorf("redis queue: erase: %w", err)
	}
	n, err = intReply(reply)
	if err != nil {
		return 0, fmt.Errorf("redis queue: erase: %w", err)
	}
	return n, nil
}

// intReply reads a script's integer reply.
func intReply(reply any) (int, error) {
	switch v := reply.(type) {
	case int64:
		return int(v), nil
	case int:
		return v, nil
	}
	return 0, fmt.Errorf("the script replied %T, want an integer", reply)
}

// moveEntry is one entry of a scripted move: the source field (empty for a
// list) and the bytes the source must still hold, and the destination
// field (empty for a list) and the value written there.
type moveEntry struct {
	from, expect, to, value string
}

// scriptMove runs moveScript: each entry moves from src to dst only while
// src still holds its expected bytes, and the script returns how many
// moved. Every move of a job from one key to another runs through it when
// the client is a [ScriptRunner], because the erasure script may run
// between a worker's read and its write: a job the worker carries in
// memory after the erasure removed it from Redis would otherwise be
// written back.
func (q *RedisQueue) scriptMove(ctx context.Context, runner ScriptRunner, src string, srcHash bool, dst string, dstHash bool, entries ...moveEntry) (int, error) {
	kinds := []byte("ll")
	if srcHash {
		kinds[0] = 'h'
	}
	if dstHash {
		kinds[1] = 'h'
	}
	args := make([]any, 0, 1+4*len(entries))
	args = append(args, string(kinds))
	for _, e := range entries {
		args = append(args, e.from, e.expect, e.to, e.value)
	}
	reply, err := runner.Eval(ctx, moveScript, []string{src, dst}, args...)
	if err != nil {
		return 0, err
	}
	return intReply(reply)
}

// moveScript moves entries from KEYS[1] to KEYS[2]. ARGV[1] names their
// kinds, source then destination, 'h' for a hash and 'l' for a list; then
// come four arguments per entry: the source field, the bytes the source
// must hold (the field's value, or a list element), the destination field
// and the value to write. An entry whose source no longer holds its bytes
// is skipped. A list source gives up one matching element (LREM 1). It
// returns how many entries moved.
const moveScript = `
local sh, dh = string.sub(ARGV[1], 1, 1) == 'h', string.sub(ARGV[1], 2, 2) == 'h'
local n = 0
for i = 2, #ARGV, 4 do
  local ok
  if sh then
    ok = redis.call('HGET', KEYS[1], ARGV[i]) == ARGV[i + 1]
    if ok then redis.call('HDEL', KEYS[1], ARGV[i]) end
  else
    ok = redis.call('LREM', KEYS[1], 1, ARGV[i + 1]) == 1
  end
  if ok then
    if dh then
      redis.call('HSET', KEYS[2], ARGV[i + 2], ARGV[i + 3])
    else
      redis.call('LPUSH', KEYS[2], ARGV[i + 3])
    end
    n = n + 1
  end
end
return n
`

// homePopped gives a popped entry, data recorded under rec, its next home:
// value into the hash dst under field, or onto the list dst when field is
// empty. With a [ScriptRunner] and a record it is one script that writes
// only while the record still holds data, and retires the record in the
// same step. moved false says the record was gone: an erasure removed the
// job, or Reclaim put it back on the main list after the worker held it
// past the visibility timeout. Either way the caller drops it. Without a
// script it is the write and then settle. A recovery write runs on a
// detached, bounded context, like recoveryPush.
func (q *RedisQueue) homePopped(ctx context.Context, rec, data, dst, field, value string, recovery bool) (moved bool, err error) {
	if recovery {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
		defer cancel()
	}
	if runner, ok := q.client.(ScriptRunner); ok && rec != "" {
		n, err := q.scriptMove(ctx, runner, q.poppedQueue, true, dst, field != "", moveEntry{rec, data, field, value})
		return n == 1, err
	}
	if field != "" {
		err = q.client.HSet(ctx, dst, field, value)
	} else {
		err = q.client.LPush(ctx, dst, value)
	}
	if err != nil {
		return false, err
	}
	q.settle(ctx, rec)
	return true, nil
}

// eraseScript removes the jobs of user ARGV[1] from the pop and processing
// hashes (KEYS[1], KEYS[2]) and the delayed, main and dead lists (KEYS[3]
// to KEYS[5]), and returns how many entries it removed; with ARGV[2] "1"
// it only counts them. ARGV[1] is json.Marshal(userID), and a job is the
// user's when the user_id key at the top of its JSON holds exactly that
// string token: json.Marshal writes both, so the bytes agree. A processing
// entry wraps the job JSON in its job key.
//
// The script never decodes a job. Redis's cjson refuses JSON nested past
// 1,000 levels, a payload Go encodes and decodes (its limit is 10,000), and
// a decode error read as "not this user" kept the job. userOf walks the
// top level instead, skipping strings and counting brackets, so a payload
// of any depth is passed over. Only the processing wrapper is decoded, and
// its value is a string, one level deep. An entry userOf cannot read (not
// an object, or cut short) names nobody and stays; Dequeue cannot decode it
// either.
const eraseScript = `
local uid, dry = ARGV[1], ARGV[2] == '1'
local function strEnd(s, i)
  local j = i + 1
  while true do
    local k = string.find(s, '["\\]', j)
    if not k then return nil end
    if string.byte(s, k) == 92 then j = k + 2 else return k end
  end
end
local function userOf(s)
  local i = string.find(s, '[^ \t\r\n]')
  if not i or string.byte(s, i) ~= 123 then return nil end
  local depth, wantKey, key, user = 1, true, nil, nil
  i = i + 1
  while true do
    local k = string.find(s, '[{}%[%],:"]', i)
    if not k then return nil end
    local c = string.byte(s, k)
    if c == 34 then
      local e = strEnd(s, k)
      if not e then return nil end
      if depth == 1 then
        if wantKey then
          key, wantKey = string.sub(s, k, e), false
          if key == '"user_id"' then user = nil end
        elseif key == '"user_id"' then
          user, key = string.sub(s, k, e), nil
        end
      end
      i = e + 1
    else
      if c == 123 or c == 91 then
        depth = depth + 1
      elseif c == 125 or c == 93 then
        depth = depth - 1
        if depth == 0 then return user end
      elseif c == 44 and depth == 1 then
        wantKey, key = true, nil
      end
      i = k + 1
    end
  end
end
local function names(raw)
  return userOf(raw) == uid
end
local function claimNames(raw)
  local ok, v = pcall(cjson.decode, raw)
  return ok and type(v) == 'table' and type(v['job']) == 'string' and names(v['job'])
end
local n = 0
for i = 1, 2 do
  local h = redis.call('HGETALL', KEYS[i])
  for j = 1, #h, 2 do
    local hit
    if i == 2 then hit = claimNames(h[j + 1]) else hit = names(h[j + 1]) end
    if hit then
      n = n + 1
      if not dry then redis.call('HDEL', KEYS[i], h[j]) end
    end
  end
end
for i = 3, 5 do
  local done = {}
  for _, raw in ipairs(redis.call('LRANGE', KEYS[i], 0, -1)) do
    if not done[raw] and names(raw) then
      if dry then
        n = n + 1
      else
        done[raw] = true
        n = n + redis.call('LREM', KEYS[i], 0, raw)
      end
    end
  end
end
return n
`

// SetVisibilityTimeout configures how long a job can be in-flight before it
// is considered abandoned and eligible for re-delivery. Defaults to 30
// seconds; d <= 0 leaves the timeout unchanged, since a zero window made
// Reclaim re-deliver a job the moment its worker claimed it. Safe for
// concurrent use with Dequeue.
func (q *RedisQueue) SetVisibilityTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	q.visibilityTimeout.Store(int64(d))
}

// Enqueue pushes a job onto the Redis list, applying defaults for ID,
// CreatedAt, and MaxAttempts when not set. A job whose ScheduledAt is ahead
// of the queue clock goes onto the delayed list instead, where Dequeue
// leaves it until it is due.
func (q *RedisQueue) Enqueue(ctx context.Context, job Job) (err error) {
	// q.client is host-pluggable (any RedisClient implementation), and
	// Enqueue runs on scheduler/dispatch loops with no per-request
	// recover net — a panicking client must surface as an error, not
	// kill the process.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("redis queue: enqueue panicked: %v", v)
		}
	}()
	if job.UniqueKey != "" {
		// RedisClient offers no atomic set-if-absent to hold the key
		// with, and a job enqueued without it would run as a duplicate.
		return ErrUniqueUnsupported
	}
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
	if job.ScheduledAt.After(q.now()) {
		return q.client.LPush(ctx, q.delayedQueue, data)
	}
	return q.client.LPush(ctx, q.queueName, data)
}

// redisRecoveryTimeout bounds one recovery write in Dequeue (restoring or
// quarantining a job already popped off the main list). A var so tests can
// shrink it.
var redisRecoveryTimeout = 5 * time.Second

// Dequeue pops a job from the Redis list and moves it to the processing queue.
// If types are specified, only jobs matching one of those types are returned;
// non-matching jobs are pushed back onto the list.
//
// Crash-safety contract for the pop→processing transition: a job is popped off
// the main list before it is recorded in the processing hash. If that recording
// fails, the popped payload is pushed back onto the main list so the job is
// re-delivered later instead of being permanently lost in the gap. A worker
// that dies in the gap runs no such push; with [PopRecorder] the pop's record
// survives it and Reclaim re-delivers the job, and without it the job is lost.
//
// Attempts are bumped at claim, matching DBQueue: a worker that crashes before
// Ack/Nack has still consumed a delivery, so a poison message cannot redeliver
// forever. A job whose attempts exceed MaxAttempts at claim is moved to the
// dead-letter queue instead of being handed out again.
//
// Before popping, Dequeue moves every delayed job that has come due onto the
// main list (see promoteDue), so a job enqueued with a future ScheduledAt is
// invisible until then and never sits in front of a due one.
func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	if err := q.promoteDue(ctx); err != nil {
		return Job{}, err
	}

	// RPop is destructive. Once an item has left the main list, caller
	// cancellation must not cancel the rollback or quarantine write that
	// gives it a durable next home. Each recovery write gets its own
	// detached, bounded context, minted when the write is needed: a deadline
	// started before RPop could expire while RPop ran and hand the restore an
	// already-dead context, and a quarantine write that spent its deadline
	// must not leave the rollback behind it none.
	recoveryPush := func(list string, values ...any) error {
		return q.recoveryPush(ctx, list, values...)
	}

	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	// Restoring a type-miss job is the same durability step as Nack's push:
	// the job was already RPop'd off the main list, so a discarded error
	// here leaves it in no list at all while Dequeue reports an ordinary
	// empty queue. Surface the failure instead. The skipped jobs go back in
	// one LPush under one deadline: a push per job, each with its own
	// deadline, could hold a worker for maxSkipDrain deadlines against a
	// dead backend. LPush of several values inserts them in argument order,
	// the same list order as pushing them one at a time.
	//
	// A skipped job's pop record goes only once the job is back on the main
	// list: until then the record is its one durable trace. With a script
	// the push and the record's retirement are one step per job, and a job
	// whose record an erasure removed is dropped (see homePopped).
	var skipped, skippedRecs []string
	requeueSkipped := func() error {
		if len(skipped) == 0 {
			return nil
		}
		if runner, ok := q.client.(ScriptRunner); ok && skippedRecs[0] != "" {
			entries := make([]moveEntry, len(skipped))
			for i, s := range skipped {
				entries[i] = moveEntry{skippedRecs[i], s, "", s}
			}
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
			defer cancel()
			if _, err := q.scriptMove(rctx, runner, q.poppedQueue, true, q.queueName, false, entries...); err != nil {
				return fmt.Errorf("restore skipped jobs: %w", err)
			}
			return nil
		}
		values := make([]any, len(skipped))
		for i, s := range skipped {
			values[i] = s
		}
		if err := recoveryPush(q.queueName, values...); err != nil {
			return fmt.Errorf("restore skipped jobs: %w", err)
		}
		q.settle(ctx, skippedRecs...)
		return nil
	}

	for {
		// Bound the type-miss drain: without a server-side filter a rare-type
		// request could otherwise RPop the entire list into process memory
		// (OOM). When the bound is hit, re-enqueue what we drained and report
		// no job, the caller retries.
		if len(skipped) >= maxSkipDrain {
			if rerr := requeueSkipped(); rerr != nil {
				return Job{}, rerr
			}
			return Job{}, ErrNoJob
		}

		// The pop's token names its record, and it becomes the claim token
		// if this entry is the one claimed: Reclaim tells a record whose
		// claim reached the processing hash by it.
		token := redisRandomID()
		data, rec, err := q.pop(ctx, token)
		if err != nil {
			// Distinguish a genuinely empty queue (sentinel) from a real
			// backend failure. Masking a backend error as "empty" would make
			// an outage look like an idle queue, so workers stop pulling jobs
			// with no signal that anything is wrong.
			isEmpty := errors.Is(err, ErrRedisEmpty)
			// A restore failure outranks "empty": reporting ErrNoJob here
			// would tell the caller the queue is idle while a valid job
			// sits in no list at all.
			if rerr := requeueSkipped(); rerr != nil {
				return Job{}, rerr
			}
			if isEmpty {
				return Job{}, ErrNoJob
			}
			return Job{}, fmt.Errorf("dequeue: pop: %w", err)
		}

		var job Job
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			// A malformed entry must not take down the valid jobs we already
			// RPop'd: re-enqueue them, then quarantine the bad entry to the
			// dead-letter queue instead of silently dropping it. If quarantine
			// is unavailable, restore the malformed entry to the main list so
			// it remains durable and observable on the next attempt.
			resultErr := fmt.Errorf("unmarshal job: %w", err)
			if _, qerr := q.homePopped(ctx, rec, data, q.deadLetterQueue, "", data, true); qerr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("quarantine malformed job: %w", qerr))
				if _, restoreErr := q.homePopped(ctx, rec, data, q.queueName, "", data, true); restoreErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("restore malformed job: %w", restoreErr))
				}
			}
			rerr := requeueSkipped()
			if rerr != nil {
				resultErr = errors.Join(resultErr, rerr)
			}
			return Job{}, resultErr
		}

		// Check type filter.
		if len(typeSet) > 0 {
			if _, ok := typeSet[job.Type]; !ok {
				skipped = append(skipped, data)
				skippedRecs = append(skippedRecs, rec)
				continue
			}
		}

		// Bump attempts at claim (DBQueue parity). The bumped payload becomes
		// the canonical record for the processing hash, Nack and Reclaim, so a
		// crash before Nack still consumes an attempt and a poison message
		// cannot redeliver forever.
		job.Attempts++
		if job.Attempts > job.MaxAttempts {
			// Exhausted: dead-letter instead of redelivering, then keep
			// looking so a following eligible job can still be handed out.
			// The job is already off the main list, a failed DLQ push
			// must restore it there and surface the error, or the job
			// vanishes (the same no-silent-loss contract as the
			// pop→processing transition below; the claim-time bump keeps
			// the restored job from redelivering forever once the
			// backend heals).
			dlqData, _ := json.Marshal(job)
			if _, err := q.homePopped(ctx, rec, data, q.deadLetterQueue, "", string(dlqData), true); err != nil {
				resultErr := fmt.Errorf("dequeue: dead-letter exhausted job: %w", err)
				if _, restoreErr := q.homePopped(ctx, rec, data, q.queueName, "", string(dlqData), true); restoreErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("restore exhausted job: %w", restoreErr))
				}
				rerr := requeueSkipped()
				if rerr != nil {
					resultErr = errors.Join(resultErr, rerr)
				}
				return Job{}, resultErr
			}
			continue
		}
		// A fresh claim token identifies THIS claim, so a stale worker whose
		// visibility timeout expired (and whose job was re-claimed by
		// someone else) cannot Ack/Nack the newer claim.
		job.ClaimToken = token
		bumped, _ := json.Marshal(job)

		// Track in processing queue for visibility timeout. The job was
		// already RPop'd off the main list, so a failure here MUST restore it.
		// Otherwise the pop→processing transition loses the job permanently
		// (it is neither on the main list nor visible to Reclaim).
		visTimeout := time.Duration(q.visibilityTimeout.Load())
		jobData, _ := json.Marshal(map[string]any{
			"job":       string(bumped),
			"expiresAt": q.now().Add(visTimeout).UnixNano(),
		})
		claimedIt, err := q.homePopped(ctx, rec, data, q.processingQueue, job.ID, string(jobData), false)
		if err != nil {
			// Restore the bumped payload to the main list so it is re-delivered
			// on a later Dequeue. If the restore itself fails the job is
			// unrecoverable, but the common single-failure case (pop ok,
			// processing write blip) no longer loses it.
			resultErr := fmt.Errorf("dequeue: track in processing: %w", err)
			if _, restoreErr := q.homePopped(ctx, rec, data, q.queueName, "", string(bumped), true); restoreErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore popped job: %w", restoreErr))
			}
			rerr := requeueSkipped()
			if rerr != nil {
				resultErr = errors.Join(resultErr, rerr)
			}
			return Job{}, resultErr
		}
		if !claimedIt {
			// The pop record was gone: an erasure removed the job while this
			// worker held it, or Reclaim put it back. Drop it.
			continue
		}

		// Re-enqueue skipped jobs. The claimed job is already recorded in
		// the processing hash, so it survives; a skipped job that failed to
		// restore does not, and the caller has to hear about it.
		if rerr := requeueSkipped(); rerr != nil {
			return job, rerr
		}

		return job, nil
	}
}

// pop takes the tail of the main list. With [PopRecorder] the same step
// records the entry in the pop hash under a field holding the pop time and
// token, and pop returns that field; the caller settles it once the entry
// has its next home. Without it, rec is empty.
func (q *RedisQueue) pop(ctx context.Context, token string) (data, rec string, err error) {
	pr, ok := q.client.(PopRecorder)
	if !ok {
		data, err = q.client.RPop(ctx, q.queueName)
		return data, "", err
	}
	rec = strconv.FormatInt(q.now().UnixNano(), 10) + ":" + token
	data, err = pr.RPopHSet(ctx, q.queueName, q.poppedQueue, rec)
	if err != nil {
		return "", "", err
	}
	return data, rec, nil
}

// settle deletes the pop records of entries that have their next home, on
// a detached, bounded context like recoveryPush. A record a failed delete
// leaves behind costs no job: Reclaim drops it when the processing hash
// holds its claim, and otherwise puts its entry back on the main list once
// it is older than the visibility timeout, a duplicate the at-least-once
// contract allows. So the error is not returned.
func (q *RedisQueue) settle(ctx context.Context, recs ...string) {
	recs = slices.DeleteFunc(slices.Clone(recs), func(r string) bool { return r == "" })
	if len(recs) == 0 {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
	defer cancel()
	_ = q.client.HDel(rctx, q.poppedQueue, recs...)
}

// recoveryPush is the LPush that gives an entry already taken off one list
// its next home. It runs on a detached, bounded context minted at the write:
// caller cancellation must not strand the entry in no list at all.
func (q *RedisQueue) recoveryPush(ctx context.Context, list string, values ...any) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
	defer cancel()
	return q.client.LPush(rctx, list, values...)
}

// promoteDue moves the delayed jobs whose ScheduledAt has passed onto the
// main list, oldest first. The delayed list is unordered by due time (the
// RedisClient interface has no sorted set), so it is read whole, a page of
// maxSkipDrain entries at a time from the tail so memory stays bounded; one
// LRange covers the common case of a short or empty list.
//
// Each due entry is pushed onto the main list before it is LRem'd from the
// delayed one, so a worker that dies between the two leaves the job on both
// lists, a duplicate the at-least-once contract allows, never on neither. Of
// two workers promoting the same entry, the one whose LRem removed nothing
// takes its own copy back off the main list. A failed push leaves the entry
// where it was. An entry that does not decode is promoted as-is, and the
// claim loop dead-letters it like any malformed job.
func (q *RedisQueue) promoteDue(ctx context.Context) error {
	now := q.now()
	kept := int64(0) // tail entries already read and left in place
	for {
		page, err := q.client.LRange(ctx, q.delayedQueue, -(kept + maxSkipDrain), -(kept + 1))
		if err != nil {
			if errors.Is(err, ErrRedisEmpty) {
				return nil
			}
			return fmt.Errorf("dequeue: read delayed jobs: %w", err)
		}
		// LPush adds at the head, so the tail end of the page is oldest.
		for i := len(page) - 1; i >= 0; i-- {
			raw := page[i]
			var job Job
			if json.Unmarshal([]byte(raw), &job) == nil && job.ScheduledAt.After(now) {
				kept++
				continue
			}
			if runner, ok := q.client.(ScriptRunner); ok {
				// One step, only while the delayed list still holds the
				// entry. Moved or not, it has left the delayed list: another
				// worker promoted it, or an erasure removed it after the read.
				if _, err := q.scriptMove(ctx, runner, q.delayedQueue, false, q.queueName, false, moveEntry{"", raw, "", raw}); err != nil {
					return fmt.Errorf("dequeue: promote delayed job: %w", err)
				}
				continue
			}
			if err := q.client.LPush(ctx, q.queueName, raw); err != nil {
				return fmt.Errorf("dequeue: promote delayed job: %w", err)
			}
			n, err := q.client.LRem(ctx, q.delayedQueue, 1, raw)
			if err != nil {
				return fmt.Errorf("dequeue: take due delayed job: %w", err)
			}
			if n == 0 {
				// Another worker promoted it first: take back this copy.
				// Counting it as read keeps the scan moving even against a
				// client whose LRem removes nothing; at worst one entry
				// waits a call longer.
				if _, err := q.client.LRem(ctx, q.queueName, 1, raw); err != nil {
					return fmt.Errorf("dequeue: drop duplicate promote: %w", err)
				}
				kept++
				continue
			}
		}
		if len(page) < maxSkipDrain {
			return nil
		}
	}
}

// maxSkipDrain bounds how many type-miss jobs Dequeue will pull off the list
// while looking for a matching type, so a rare-type filter cannot pull the
// whole queue into process memory.
const maxSkipDrain = 1024

// processingEntry is the per-claim record stored in the processing hash:
// the claimed job (with its ClaimToken) plus the lease's expiry stamp.
type processingEntry struct {
	Job       string `json:"job"`
	ExpiresAt int64  `json:"expiresAt"`
}

// currentClaim reads the processing entry for jobID and returns the job of
// the CURRENT claim. found is false when nothing is claimed under that ID.
func (q *RedisQueue) currentClaim(ctx context.Context, jobID string) (current Job, raw string, found bool, err error) {
	data, err := q.client.HGet(ctx, q.processingQueue, jobID)
	if err != nil {
		if errors.Is(err, ErrRedisEmpty) {
			return Job{}, "", false, nil
		}
		return Job{}, "", false, fmt.Errorf("read processing entry: %w", err)
	}
	var entry processingEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		return Job{}, "", false, fmt.Errorf("unmarshal processing entry: %w", err)
	}
	var job Job
	if err := json.Unmarshal([]byte(entry.Job), &job); err != nil {
		return Job{}, "", false, fmt.Errorf("unmarshal job: %w", err)
	}
	return job, data, true, nil
}

// releaseClaim removes the processing entry for jobID, but only while it
// still holds exactly the bytes the caller checked. See [CompareAndDeleter]
// for why an unconditional HDel here loses jobs: the claim can change
// between the completion's token check and its delete.
func (q *RedisQueue) releaseClaim(ctx context.Context, jobID, expect string) error {
	stale, err := q.releaseField(ctx, q.processingQueue, jobID, expect)
	if stale {
		// The entry changed under us: another worker re-claimed the job.
		// Its record was left alone; count the stale completion.
		q.staleClaims.Add(1)
	}
	return err
}

// releaseField deletes field from hash while it still holds expect. stale
// reports that it held something else and was left alone.
func (q *RedisQueue) releaseField(ctx context.Context, hash, field, expect string) (stale bool, err error) {
	if cas, ok := q.client.(CompareAndDeleter); ok {
		deleted, err := cas.HDelIfEqual(ctx, hash, field, expect)
		if err != nil {
			return false, err
		}
		return !deleted, nil
	}
	// No atomic capability: re-read as late as possible and skip the delete
	// if the field already moved on. This narrows the window to the final
	// round trip; it does not close it.
	cur, err := q.client.HGet(ctx, hash, field)
	if err != nil {
		if errors.Is(err, ErrRedisEmpty) {
			return false, nil
		}
		return false, err
	}
	if cur != expect {
		return true, nil
	}
	return false, q.client.HDel(ctx, hash, field)
}

// moveClaim pushes value onto list and retires the processing entry for
// jobID, only while that entry still holds exactly expect. With
// [CompareAndMover] the check, the push and the delete are one atomic
// step, and a claim that moved on is left alone and counted as stale.
// Without it the push lands first and the conditional release follows,
// which narrows the window but cannot close it (see CompareAndMover).
// moved is false when nothing was pushed: on a refused move, or when the
// fallback's push failed. A fallback release error comes back with moved
// true, since the job already has its next home.
func (q *RedisQueue) moveClaim(ctx context.Context, jobID, expect, list, value string) (moved bool, err error) {
	moved, stale, err := q.moveField(ctx, q.processingQueue, jobID, expect, list, value)
	if stale {
		q.staleClaims.Add(1)
	}
	return moved, err
}

// moveField pushes value onto list and deletes field from hash, only while
// the field still holds expect; moveClaim's move for any hash. stale
// reports that the field held something else.
func (q *RedisQueue) moveField(ctx context.Context, hash, field, expect, list, value string) (moved, stale bool, err error) {
	if mover, ok := q.client.(CompareAndMover); ok {
		moved, err := mover.MoveIfEqual(ctx, hash, field, expect, list, value)
		if err != nil {
			return false, false, fmt.Errorf("move to %s: %w", list, err)
		}
		return moved, !moved, nil
	}
	if runner, ok := q.client.(ScriptRunner); ok {
		n, err := q.scriptMove(ctx, runner, hash, true, list, false, moveEntry{field, expect, "", value})
		if err != nil {
			return false, false, fmt.Errorf("move to %s: %w", list, err)
		}
		return n == 1, n != 1, nil
	}
	if err := q.client.LPush(ctx, list, value); err != nil {
		return false, false, fmt.Errorf("push to %s: %w", list, err)
	}
	stale, err = q.releaseField(ctx, hash, field, expect)
	return true, stale, err
}

// ownsClaim reports whether the completion being presented (job) matches the
// current processing entry for that ID. A mismatch means the presenter's
// visibility timeout expired and another worker re-claimed the job, their
// completion must not touch the newer claim. Counts a rejected completion in
// q.staleClaims so the fencing is observable, and returns false. A missing
// entry surfaces as found=false for the caller to treat as an idempotent
// no-op.
func (q *RedisQueue) ownsClaim(job, current Job, found bool) bool {
	if !found {
		return false
	}
	// Constant-time compare: ClaimToken is a credential fencing stale
	// completions, and a short-circuit == leaks match status through
	// timing. The tokens are fixed-length crypto/rand hex, so the
	// length precheck inside ConstantTimeCompare leaks nothing.
	if subtle.ConstantTimeCompare([]byte(current.ClaimToken), []byte(job.ClaimToken)) == 1 {
		return true
	}
	q.staleClaims.Add(1)
	return false
}

// Ack removes the job's processing entry after successful handling. The
// claim is fenced by Job.ClaimToken: an Ack from a worker whose visibility
// timeout already expired (the job was re-claimed elsewhere) is a no-op,
// deleting the newer claimant's entry would make the job unreclaimable
// if that claimant then crashed. Acking an ID nothing is claimed
// under (already acked) is an idempotent no-op.
func (q *RedisQueue) Ack(ctx context.Context, job Job) error {
	current, raw, found, err := q.currentClaim(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	if !q.ownsClaim(job, current, found) {
		return nil // stale claim (counted) or nothing claimed, no-op
	}
	if err := q.releaseClaim(ctx, job.ID, raw); err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	return nil
}

// Nack handles a failed job. If retries remain, it re-enqueues the job;
// otherwise it moves it to the dead letter queue. Like Ack it is fenced by
// Job.ClaimToken: a stale claimant's Nack must neither re-enqueue the newer
// claimant's in-flight job (concurrent double-run) nor delete its entry
// (unreclaimable loss). Nacking an ID nothing is claimed under is an
// idempotent no-op.
func (q *RedisQueue) Nack(ctx context.Context, claimed Job) error {
	job, raw, found, err := q.currentClaim(ctx, claimed.ID)
	if err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	if !q.ownsClaim(claimed, job, found) {
		return nil // stale claim (counted) or nothing claimed, no-op
	}

	// Attempts were bumped at claim (Dequeue), so the processing entry
	// already carries the post-claim count. Nack only decides retry vs
	// dead-letter. Bumping here too would double-count and let a poison
	// message that only ever crashes before Nack evade MaxAttempts.
	dest := q.queueName
	if job.Attempts >= job.MaxAttempts {
		dest = q.deadLetterQueue
	}

	// Write the job to its next home BEFORE dropping the processing entry.
	// The processing hash is the only durable copy of a claimed job: deleting
	// it first and then failing this push left the job on no list and
	// invisible to Reclaim, silently lost.
	//
	// The reverse order can duplicate instead: if the push lands and the HDel
	// fails, Reclaim re-delivers the job once its visibility timeout expires.
	// This queue is at-least-once, so a rare duplicate is within contract and
	// losing the job is not.
	//
	// The token check above is a snapshot: the lease can expire and another
	// worker re-claim before the push. moveClaim binds the push to the
	// entry that was checked (atomically, with [CompareAndMover]), so a
	// stale Nack pushes nothing rather than a second copy of a job that
	// another worker is running.
	payload, _ := json.Marshal(job)
	if _, err := q.moveClaim(ctx, claimed.ID, raw, dest, string(payload)); err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	return nil
}

// Reclaim scans the processing set for in-flight jobs whose visibility
// timeout has passed (the worker that claimed them crashed before Ack/Nack),
// re-enqueues them onto the main list, and removes the stale processing
// entry. It first puts back the jobs a worker died holding inside Dequeue
// (see [PopRecorder]). Returns the number of jobs re-delivered. Call it
// periodically (e.g. from a background ticker) to make in-flight Redis work
// crash-safe.
func (q *RedisQueue) Reclaim(ctx context.Context) (int, error) {
	// Pops first: a processing entry re-delivered here, then seen as a pop
	// record with no claim, would be put back twice.
	reclaimed, err := q.reclaimPops(ctx)
	if err != nil {
		return reclaimed, err
	}
	entries, err := q.client.HGetAll(ctx, q.processingQueue)
	if err != nil {
		return reclaimed, fmt.Errorf("reclaim: scan processing: %w", err)
	}
	now := q.now().UnixNano()
	for jobID, raw := range entries {
		var entry processingEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			// Corrupt processing entry: quarantine the raw bytes to the
			// dead-letter list — the same no-silent-loss stance as
			// Dequeue's bad-JSON path on the main list — instead of
			// deleting them outright. This hash holds the claimed job's
			// ONLY durable copy. Push first and HDel only if the push
			// landed; on push failure the entry stays in the processing
			// hash, still observable by the next sweep.
			//
			// Fenced like the expiry path below: the snapshot's bytes
			// are what get quarantined, so only that entry goes. A
			// newer claim that landed under this ID meanwhile (a
			// Replay re-fired the job and a worker claimed it) keeps
			// its processing entry.
			_, _ = q.moveClaim(ctx, jobID, raw, q.deadLetterQueue, raw)
			continue
		}
		if entry.ExpiresAt > now {
			continue // still within its lease
		}
		// Re-enqueue the original job, then clear the processing entry. Order
		// matters: enqueue first so a crash between the two ops re-delivers
		// (at-least-once) rather than loses the job. Both are bound to the
		// snapshot's bytes (moveClaim): the claimant can Nack and another
		// worker re-claim after the HGetAll, and then a push would land a
		// second copy of a job that worker is running, while an
		// unconditional HDel would remove the NEW claimant's record — the
		// job then sits on no list, invisible to the next Reclaim, and is
		// silently lost.
		moved, err := q.moveClaim(ctx, jobID, raw, q.queueName, entry.Job)
		if err != nil && !moved {
			return reclaimed, fmt.Errorf("reclaim: re-enqueue %s: %w", jobID, err)
		}
		if !moved {
			continue // the claim moved on since the scan (counted as stale)
		}
		reclaimed++
	}
	return reclaimed, nil
}

// reclaimPops puts back on the main list the entries of pop records older
// than the visibility timeout: jobs a worker took off the main list and
// died holding before they had their next home. A younger record may
// belong to a Dequeue still running and is left alone. A record whose job
// the processing hash holds under the pop's token is a claim that landed;
// only the record goes, and the processing entry is Reclaim's to judge.
func (q *RedisQueue) reclaimPops(ctx context.Context) (int, error) {
	recs, err := q.client.HGetAll(ctx, q.poppedQueue)
	if err != nil {
		return 0, fmt.Errorf("reclaim: scan pops: %w", err)
	}
	cutoff := q.now().Add(-time.Duration(q.visibilityTimeout.Load())).UnixNano()
	reclaimed := 0
	// Field order is pop order (the field starts with the pop time), so the
	// oldest goes back first.
	for _, field := range slices.Sorted(maps.Keys(recs)) {
		raw := recs[field]
		at, token, _ := strings.Cut(field, ":")
		// A field that does not parse holds no time to wait for; its entry
		// goes back rather than sit in the hash for good.
		if popped, err := strconv.ParseInt(at, 10, 64); err == nil && popped > cutoff {
			continue
		}
		var job Job
		if json.Unmarshal([]byte(raw), &job) == nil {
			cur, _, found, err := q.currentClaim(ctx, job.ID)
			if err != nil {
				// Unknown whether the claim landed, so the record stays for
				// the next pass. Returning would also skip the processing
				// scan below, which quarantines a corrupt entry; an outage
				// fails that scan's own read.
				continue
			}
			if found && subtle.ConstantTimeCompare([]byte(cur.ClaimToken), []byte(token)) == 1 {
				if _, err := q.releaseField(ctx, q.poppedQueue, field, raw); err != nil {
					return reclaimed, fmt.Errorf("reclaim: drop pop record: %w", err)
				}
				continue
			}
		}
		// An entry that does not decode goes back as-is, and Dequeue
		// dead-letters it like any malformed job.
		moved, _, err := q.moveField(ctx, q.poppedQueue, field, raw, q.queueName, raw)
		if err != nil && !moved {
			return reclaimed, fmt.Errorf("reclaim: put back a popped job: %w", err)
		}
		if moved {
			reclaimed++
		}
	}
	return reclaimed, nil
}

// Replay implements [Replayable]: it pulls a terminally-failed job off the
// dead-letter list and re-enqueues it onto the main queue with its attempts
// counter reset, so it gets a full set of retries again. It is idempotent,
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

		// Reset for a fresh set of retries, then re-marshal. ClaimToken is
		// cleared too, it identified a claim that is now terminal; the next
		// Dequeue mints a fresh one.
		job.Attempts = 0
		job.ClaimToken = ""
		requeued, err := json.Marshal(job)
		if err != nil {
			return fmt.Errorf("replay: marshal job: %w", err)
		}

		// With a script the move is one step, and only while the dead list
		// still holds the entry: an erasure that removed it after the read
		// above leaves nothing to replay.
		if runner, ok := q.client.(ScriptRunner); ok {
			if _, err := q.scriptMove(ctx, runner, q.deadLetterQueue, false, q.queueName, false, moveEntry{"", raw, "", string(requeued)}); err != nil {
				return fmt.Errorf("replay: move job: %w", err)
			}
			return nil
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

	// No matching dead-lettered job, idempotent no-op.
	return nil
}

// ListJobs implements [Browsable] for the Redis backend. The only durable
// job state accessible without a scan of the full main/processing lists is
// the dead-letter queue, so this returns dead jobs for status "failed" (or
// an empty/"all" status) and nothing for any other status value. Jobs are
// returned newest-first (head of the Redis list) up to limit entries,
// after skipping offset of them. limit <= 0 defaults to 100; a negative
// offset reads as zero.
func (q *RedisQueue) ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, error) {
	if status != "" && status != "all" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	start := int64(max(offset, 0))
	if start > math.MaxInt64-int64(limit) {
		// Past any list Redis can hold; the stop index would wrap
		// negative, which LRANGE reads as counting from the tail.
		return nil, nil
	}
	entries, err := q.client.LRange(ctx, q.deadLetterQueue, start, start+int64(limit)-1)
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
// Reclaim is called; 0 keeps the 30 second default. A negative interval
// panics — Start returns no error, and a sign/unit error must not
// silently become the slowest reclaim cadence. Typical use:
//
//	q.Start(ctx, 30*time.Second)
func (q *RedisQueue) Start(ctx context.Context, interval time.Duration) {
	if interval < 0 {
		panic(fmt.Sprintf("queue: RedisQueue.Start: interval must be >= 0 (got %v)", interval))
	}
	if interval == 0 {
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

// Close removes the queue from App.EraseUserData's reach. The caller
// manages the Redis connection.
func (q *RedisQueue) Close() error {
	if q.unregisterErase != nil {
		q.unregisterErase()
	}
	return nil
}

// redisRandomID generates a 16-byte hex string ID.
func redisRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
