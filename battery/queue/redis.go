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

// RedisClient is the minimal Redis client RedisQueue needs.
type RedisClient interface {
	LPush(ctx context.Context, key string, values ...any) error
	RPop(ctx context.Context, key string) (string, error)
	// HGet returns the value at field in the hash at key.
	HGet(ctx context.Context, key, field string) (string, error)
	HSet(ctx context.Context, key string, values ...any) error
	// HGetAll returns every field and value in the hash.
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	// Del removes keys.
	Del(ctx context.Context, keys ...string) error
	// LRange returns the elements of the list at key in [start, stop];
	// negative indices count from the tail.
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	// LRem removes up to count occurrences of value from the list and returns
	// the number removed.
	LRem(ctx context.Context, key string, count int64, value any) (int64, error)
}

// CompareAndDeleter is the optional client capability to delete a hash field
// only while it holds an expected value.
type CompareAndDeleter interface {
	HDelIfEqual(ctx context.Context, key, field, expect string) (bool, error)
}

// CompareAndMover is the optional client capability to move a claimed job back
// to a list in one atomic step.
type CompareAndMover interface {
	MoveIfEqual(ctx context.Context, key, field, expect, list, value string) (bool, error)
}

// PopRecorder is the optional client capability to pop a list's tail and
// record it in a hash atomically.
type PopRecorder interface {
	RPopHSet(ctx context.Context, list, hash, field string) (string, error)
}

// ListLengther is the optional client capability to read a list's length
// (LLEN) without reading the list.
type ListLengther interface {
	LLen(ctx context.Context, key string) (int64, error)
}

// ScriptRunner is the client capability RedisQueue needs to erase a user's
// jobs atomically.
type ScriptRunner interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

// ErrRedisEmpty is the sentinel a RedisClient returns or wraps when a read
// finds nothing.
var ErrRedisEmpty = errors.New("redis: list is empty")

// RedisQueue implements Queue on Redis lists and hashes.
type RedisQueue struct {
	client          RedisClient
	queueName       string
	processingQueue string
	deadLetterQueue string
	delayedQueue    string
	poppedQueue     string

	visibilityTimeout atomic.Int64

	now func() time.Time

	staleClaims atomic.Int64

	eraseName       string
	unregisterErase func()
}

// StaleClaimCount returns how many Acks and Nacks were refused as coming from
// a stale claim.
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
	q.eraseName, q.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "queue_redis:" + queueName,
		Source: "queue",
		Erase:  q.eraseUser,
	})
	return q
}

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

func intReply(reply any) (int, error) {
	switch v := reply.(type) {
	case int64:
		return int(v), nil
	case int:
		return v, nil
	}
	return 0, fmt.Errorf("the script replied %T, want an integer", reply)
}

type moveEntry struct {
	from, expect, to, value string
}

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

// SetVisibilityTimeout sets how long a job may stay in flight before Reclaim
// re-delivers it.
func (q *RedisQueue) SetVisibilityTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	q.visibilityTimeout.Store(int64(d))
}

// Enqueue pushes a job onto the Redis list, defaulting its ID, CreatedAt and
// MaxAttempts.
func (q *RedisQueue) Enqueue(ctx context.Context, job Job) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("redis queue: enqueue panicked: %s", textsafe.Recovered(v))
		}
	}()
	if job.UniqueKey != "" {
		return ErrUniqueUnsupported
	}
	if job.SequenceKey != "" {
		return ErrSequenceUnsupported
	}
	if job.MaxAge < 0 {
		return errNegativeMaxAge
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

var redisRecoveryTimeout = 5 * time.Second

// Dequeue pops a job and records it in the processing hash.
func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	if err := q.promoteDue(ctx); err != nil {
		return Job{}, err
	}

	recoveryPush := func(list string, values ...any) error {
		return q.recoveryPush(ctx, list, values...)
	}

	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

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
		if len(skipped) >= maxSkipDrain {
			if rerr := requeueSkipped(); rerr != nil {
				return Job{}, rerr
			}
			return Job{}, ErrNoJob
		}

		token := redisRandomID()
		data, rec, err := q.pop(ctx, token)
		if err != nil {
			isEmpty := errors.Is(err, ErrRedisEmpty)
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

		if len(typeSet) > 0 {
			if _, ok := typeSet[job.Type]; !ok {
				skipped = append(skipped, data)
				skippedRecs = append(skippedRecs, rec)
				continue
			}
		}

		job.Attempts++
		if job.Attempts > job.MaxAttempts {
			dlqData, err := json.Marshal(job)
			if err != nil {
				resultErr := fmt.Errorf("dequeue: marshal exhausted job: %w", err)
				if _, restoreErr := q.homePopped(ctx, rec, data, q.queueName, "", data, true); restoreErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("restore exhausted job: %w", restoreErr))
				}
				rerr := requeueSkipped()
				if rerr != nil {
					resultErr = errors.Join(resultErr, rerr)
				}
				return Job{}, resultErr
			}
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
		job.ClaimToken = token
		bumped, err := json.Marshal(job)
		if err != nil {
			resultErr := fmt.Errorf("dequeue: marshal bumped job: %w", err)
			if _, restoreErr := q.homePopped(ctx, rec, data, q.queueName, "", data, true); restoreErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore popped job: %w", restoreErr))
			}
			rerr := requeueSkipped()
			if rerr != nil {
				resultErr = errors.Join(resultErr, rerr)
			}
			return Job{}, resultErr
		}

		visTimeout := time.Duration(q.visibilityTimeout.Load())
		jobData, _ := json.Marshal(map[string]any{
			"job":       string(bumped),
			"expiresAt": q.now().Add(visTimeout).UnixNano(),
		})
		claimedIt, err := q.homePopped(ctx, rec, data, q.processingQueue, job.ID, string(jobData), false)
		if err != nil {
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
			continue
		}

		if rerr := requeueSkipped(); rerr != nil {
			return job, rerr
		}

		return job, nil
	}
}

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

func (q *RedisQueue) settle(ctx context.Context, recs ...string) {
	recs = slices.DeleteFunc(slices.Clone(recs), func(r string) bool { return r == "" })
	if len(recs) == 0 {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
	defer cancel()
	_ = q.client.HDel(rctx, q.poppedQueue, recs...)
}

func (q *RedisQueue) recoveryPush(ctx context.Context, list string, values ...any) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisRecoveryTimeout)
	defer cancel()
	return q.client.LPush(rctx, list, values...)
}

func (q *RedisQueue) promoteDue(ctx context.Context) error {
	now := q.now()
	kept := int64(0)
	for {
		page, err := q.client.LRange(ctx, q.delayedQueue, -(kept + maxSkipDrain), -(kept + 1))
		if err != nil {
			if errors.Is(err, ErrRedisEmpty) {
				return nil
			}
			return fmt.Errorf("dequeue: read delayed jobs: %w", err)
		}
		for i := len(page) - 1; i >= 0; i-- {
			raw := page[i]
			var job Job
			if json.Unmarshal([]byte(raw), &job) == nil && job.ScheduledAt.After(now) {
				kept++
				continue
			}
			if runner, ok := q.client.(ScriptRunner); ok {
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

const maxSkipDrain = 1024

type processingEntry struct {
	Job       string `json:"job"`
	ExpiresAt int64  `json:"expiresAt"`
}

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

func (q *RedisQueue) releaseClaim(ctx context.Context, jobID, expect string) error {
	stale, err := q.releaseField(ctx, q.processingQueue, jobID, expect)
	if stale {
		q.staleClaims.Add(1)
	}
	return err
}

func (q *RedisQueue) releaseField(ctx context.Context, hash, field, expect string) (stale bool, err error) {
	if cas, ok := q.client.(CompareAndDeleter); ok {
		deleted, err := cas.HDelIfEqual(ctx, hash, field, expect)
		if err != nil {
			return false, err
		}
		return !deleted, nil
	}
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

func (q *RedisQueue) moveClaim(ctx context.Context, jobID, expect, list, value string) (moved bool, err error) {
	moved, stale, err := q.moveField(ctx, q.processingQueue, jobID, expect, list, value)
	if stale {
		q.staleClaims.Add(1)
	}
	return moved, err
}

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

func (q *RedisQueue) ownsClaim(job, current Job, found bool) bool {
	if !found {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(current.ClaimToken), []byte(job.ClaimToken)) == 1 {
		return true
	}
	q.staleClaims.Add(1)
	return false
}

// Ack removes the job's processing entry.
func (q *RedisQueue) Ack(ctx context.Context, job Job) error {
	current, raw, found, err := q.currentClaim(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	if !q.ownsClaim(job, current, found) {
		return nil
	}
	if err := q.releaseClaim(ctx, job.ID, raw); err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	return nil
}

// Nack retries or dead-letters a failed job.
func (q *RedisQueue) Nack(ctx context.Context, claimed Job) error {
	job, raw, found, err := q.currentClaim(ctx, claimed.ID)
	if err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	if !q.ownsClaim(claimed, job, found) {
		return nil
	}

	dest := q.queueName
	if job.Attempts >= job.MaxAttempts || pastMaxAge(job, q.now()) {
		dest = q.deadLetterQueue
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("nack: marshal job: %w", err)
	}
	if _, err := q.moveClaim(ctx, claimed.ID, raw, dest, string(payload)); err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	return nil
}

// Reclaim re-enqueues in-flight jobs whose visibility timeout has passed.
func (q *RedisQueue) Reclaim(ctx context.Context) (reclaimed int, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("redis queue: reclaim panicked: %s", textsafe.Recovered(v))
		}
	}()
	reclaimed, err = q.reclaimPops(ctx)
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
			_, _ = q.moveClaim(ctx, jobID, raw, q.deadLetterQueue, raw)
			continue
		}
		if entry.ExpiresAt > now {
			continue
		}
		moved, err := q.moveClaim(ctx, jobID, raw, q.queueName, entry.Job)
		if err != nil && !moved {
			return reclaimed, fmt.Errorf("reclaim: re-enqueue %s: %w", jobID, err)
		}
		if !moved {
			continue
		}
		reclaimed++
	}
	return reclaimed, nil
}

func (q *RedisQueue) reclaimPops(ctx context.Context) (int, error) {
	recs, err := q.client.HGetAll(ctx, q.poppedQueue)
	if err != nil {
		return 0, fmt.Errorf("reclaim: scan pops: %w", err)
	}
	cutoff := q.now().Add(-time.Duration(q.visibilityTimeout.Load())).UnixNano()
	reclaimed := 0
	for _, field := range slices.Sorted(maps.Keys(recs)) {
		raw := recs[field]
		at, token, _ := strings.Cut(field, ":")
		if popped, err := strconv.ParseInt(at, 10, 64); err == nil && popped > cutoff {
			continue
		}
		var job Job
		if json.Unmarshal([]byte(raw), &job) == nil {
			cur, _, found, err := q.currentClaim(ctx, job.ID)
			if err != nil {
				continue
			}
			if found && subtle.ConstantTimeCompare([]byte(cur.ClaimToken), []byte(token)) == 1 {
				if _, err := q.releaseField(ctx, q.poppedQueue, field, raw); err != nil {
					return reclaimed, fmt.Errorf("reclaim: drop pop record: %w", err)
				}
				continue
			}
		}
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

// Replay implements Replayable.
func (q *RedisQueue) Replay(ctx context.Context, jobID string) error {
	_, err := q.ReplayJob(ctx, jobID)
	return err
}

// ReplayJob implements ReplayReporter.
func (q *RedisQueue) ReplayJob(ctx context.Context, jobID string) (bool, error) {
	raw, job, found, err := q.findDead(ctx, jobID)
	if err != nil || !found {
		return false, err
	}
	job.Attempts = 0
	job.ClaimToken = ""
	requeued, err := json.Marshal(job)
	if err != nil {
		return false, fmt.Errorf("replay: marshal job: %w", err)
	}

	if runner, ok := q.client.(ScriptRunner); ok {
		moved, err := q.scriptMove(ctx, runner, q.deadLetterQueue, false, q.queueName, false, moveEntry{"", raw, "", string(requeued)})
		if err != nil {
			return false, fmt.Errorf("replay: move job: %w", err)
		}
		return moved > 0, nil
	}
	if err := q.client.LPush(ctx, q.queueName, requeued); err != nil {
		return false, fmt.Errorf("replay: re-enqueue job: %w", err)
	}
	if _, err := q.client.LRem(ctx, q.deadLetterQueue, 1, raw); err != nil {
		return false, fmt.Errorf("replay: remove from dead-letter queue: %w", err)
	}
	return true, nil
}

const deadScanTries = 4

var errDeadListMoved = errors.New("replay: the dead-letter list kept changing while it was read; try again")

const deadScanFirst = 32

func (q *RedisQueue) findDead(ctx context.Context, jobID string) (raw string, job Job, found bool, err error) {
	for range deadScanTries {
		var moved bool
		raw, job, found, moved, err = q.scanDead(ctx, jobID)
		if err != nil || !moved {
			return raw, job, found, err
		}
	}
	return "", Job{}, false, errDeadListMoved
}

func (q *RedisQueue) scanDead(ctx context.Context, jobID string) (raw string, job Job, found, moved bool, err error) {
	anchor := ""
	size := int64(deadScanFirst)
	for start := int64(0); ; start, size = start+size, min(size*2, maxSkipDrain) {
		page, err := q.client.LRange(ctx, q.deadLetterQueue, max(start-1, 0), start+size-1)
		if err != nil && !errors.Is(err, ErrRedisEmpty) {
			return "", Job{}, false, false, fmt.Errorf("replay: read dead-letter queue: %w", err)
		}
		if start > 0 {
			if len(page) == 0 || page[0] != anchor {
				return "", Job{}, false, true, nil
			}
			page = page[1:]
		}
		for _, raw := range page {
			var job Job
			if json.Unmarshal([]byte(raw), &job) == nil && job.ID == jobID {
				return raw, job, true, false, nil
			}
		}
		if int64(len(page)) < size {
			return "", Job{}, false, false, nil
		}
		anchor = page[len(page)-1]
	}
}

// ListJobs implements Browsable; it lists dead-letter jobs only.
func (q *RedisQueue) ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, error) {
	if status != "" && status != "all" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	start := int64(max(offset, 0))
	if start > math.MaxInt64-int64(limit) {
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
			continue
		}
		out = append(out, job)
	}
	return out, nil
}

// Stats implements Browsable; it needs a ListLengther client.
func (q *RedisQueue) Stats(ctx context.Context) (JobStats, error) {
	n, err := q.listLen(ctx, q.deadLetterQueue)
	if err != nil {
		return nil, fmt.Errorf("stats: length of the dead-letter queue: %w", err)
	}
	stats := JobStats{}
	if n > 0 {
		stats["failed"] = n
	}
	return stats, nil
}

func (q *RedisQueue) pendingDepth(ctx context.Context) (int, error) {
	total := 0
	for _, key := range []string{q.queueName, q.delayedQueue} {
		n, err := q.listLen(ctx, key)
		if err != nil {
			return 0, fmt.Errorf("depth: length of %s: %w", key, err)
		}
		total += n
	}
	return total, nil
}

var errNoListLength = errors.New("redis queue: the client has no LLEN (queue.ListLengther)")

func (q *RedisQueue) listLen(ctx context.Context, key string) (int, error) {
	lengther, ok := q.client.(ListLengther)
	if !ok {
		return 0, errNoListLength
	}
	n, err := lengther.LLen(ctx, key)
	if err != nil && !errors.Is(err, ErrRedisEmpty) {
		return 0, err
	}
	return int(max(n, 0)), nil
}

// Start runs Reclaim on every tick until ctx is cancelled.
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

var (
	_ Queue      = (*RedisQueue)(nil)
	_ Browsable  = (*RedisQueue)(nil)
	_ Replayable = (*RedisQueue)(nil)

	_ ReplayReporter = (*RedisQueue)(nil)
)

// Close removes the queue from App.EraseUserData's reach.
func (q *RedisQueue) Close() error {
	if q.unregisterErase != nil {
		q.unregisterErase()
	}
	return nil
}

func redisRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
