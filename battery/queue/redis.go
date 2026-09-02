package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
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
		now:             time.Now,
	}
	q.visibilityTimeout.Store(int64(30 * time.Second))
	return q
}

// SetVisibilityTimeout configures how long a job can be in-flight before it
// is considered abandoned and eligible for re-delivery. Safe for concurrent
// use with Dequeue.
func (q *RedisQueue) SetVisibilityTimeout(d time.Duration) {
	q.visibilityTimeout.Store(int64(d))
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
//
// Crash-safety contract for the pop→processing transition: a job is RPop'd off
// the main list before it is recorded in the processing hash. If that recording
// fails, the popped payload is pushed back onto the main list so the job is
// re-delivered later instead of being permanently lost in the gap. (An atomic
// RPOPLPUSH-style move would close the window entirely, but the RedisClient
// interface exposes no single-round-trip move primitive, error propagation
// that leaves the job on the queue is the safe option within this interface.)
//
// Attempts are bumped at claim, matching DBQueue: a worker that crashes before
// Ack/Nack has still consumed a delivery, so a poison message cannot redeliver
// forever. A job whose attempts exceed MaxAttempts at claim is moved to the
// dead-letter queue instead of being handed out again.
func (q *RedisQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}

	// Restoring a type-miss job is the same durability step as Nack's push:
	// the job was already RPop'd off the main list, so a discarded error
	// here leaves it in no list at all while Dequeue reports an ordinary
	// empty queue. Surface the first failure instead.
	requeueSkipped := func(skipped []string) error {
		var firstErr error
		for _, s := range skipped {
			if err := q.client.LPush(ctx, q.queueName, s); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("restore skipped job: %w", err)
			}
		}
		return firstErr
	}

	var skipped []string
	for {
		// Bound the type-miss drain: without a server-side filter a rare-type
		// request could otherwise RPop the entire list into process memory
		// (OOM). When the bound is hit, re-enqueue what we drained and report
		// no job, the caller retries.
		if len(skipped) >= maxSkipDrain {
			if rerr := requeueSkipped(skipped); rerr != nil {
				return Job{}, rerr
			}
			return Job{}, ErrNoJob
		}

		data, err := q.client.RPop(ctx, q.queueName)
		if err != nil {
			// Distinguish a genuinely empty queue (sentinel) from a real
			// backend failure. Masking a backend error as "empty" would make
			// an outage look like an idle queue, so workers stop pulling jobs
			// with no signal that anything is wrong.
			isEmpty := errors.Is(err, ErrRedisEmpty)
			// A restore failure outranks "empty": reporting ErrNoJob here
			// would tell the caller the queue is idle while a valid job
			// sits in no list at all.
			if rerr := requeueSkipped(skipped); rerr != nil {
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
			// dead-letter queue instead of silently dropping it.
			rerr := requeueSkipped(skipped)
			_ = q.client.LPush(ctx, q.deadLetterQueue, data)
			if rerr != nil {
				return Job{}, errors.Join(fmt.Errorf("unmarshal job: %w", err), rerr)
			}
			return Job{}, fmt.Errorf("unmarshal job: %w", err)
		}

		// Check type filter.
		if len(typeSet) > 0 {
			if _, ok := typeSet[job.Type]; !ok {
				skipped = append(skipped, data)
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
			if err := q.client.LPush(ctx, q.deadLetterQueue, dlqData); err != nil {
				_ = q.client.LPush(ctx, q.queueName, string(dlqData))
				rerr := requeueSkipped(skipped)
				if rerr != nil {
					return Job{}, errors.Join(fmt.Errorf("dequeue: dead-letter exhausted job: %w", err), rerr)
				}
				return Job{}, fmt.Errorf("dequeue: dead-letter exhausted job: %w", err)
			}
			continue
		}
		// Mint a fresh claim token: it identifies THIS claim, so a stale
		// worker whose visibility timeout expired (and whose job was
		// re-claimed by someone else) cannot Ack/Nack the newer claim.
		job.ClaimToken = redisRandomID()
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
		if err := q.client.HSet(ctx, q.processingQueue, job.ID, jobData); err != nil {
			// Restore the bumped payload to the main list so it is re-delivered
			// on a later Dequeue. If the restore itself fails the job is
			// unrecoverable, but the common single-failure case (pop ok,
			// processing write blip) no longer loses it.
			_ = q.client.LPush(ctx, q.queueName, string(bumped))
			rerr := requeueSkipped(skipped)
			if rerr != nil {
				return Job{}, errors.Join(fmt.Errorf("dequeue: track in processing: %w", err), rerr)
			}
			return Job{}, fmt.Errorf("dequeue: track in processing: %w", err)
		}

		// Re-enqueue skipped jobs. The claimed job is already recorded in
		// the processing hash, so it survives; a skipped job that failed to
		// restore does not, and the caller has to hear about it.
		if rerr := requeueSkipped(skipped); rerr != nil {
			return job, rerr
		}

		return job, nil
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
	if cas, ok := q.client.(CompareAndDeleter); ok {
		deleted, err := cas.HDelIfEqual(ctx, q.processingQueue, jobID, expect)
		if err != nil {
			return err
		}
		if !deleted {
			// The entry changed under us: another worker re-claimed the
			// job. Leave its record alone and count the stale completion.
			q.staleClaims.Add(1)
		}
		return nil
	}
	// No atomic capability: re-read as late as possible and skip the delete
	// if the claim already moved on. This narrows the window to the final
	// round trip; it does not close it.
	cur, err := q.client.HGet(ctx, q.processingQueue, jobID)
	if err != nil {
		if errors.Is(err, ErrRedisEmpty) {
			return nil
		}
		return err
	}
	if cur != expect {
		q.staleClaims.Add(1)
		return nil
	}
	return q.client.HDel(ctx, q.processingQueue, jobID)
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
	if current.ClaimToken == job.ClaimToken {
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
	payload, _ := json.Marshal(job)
	if err := q.client.LPush(ctx, dest, payload); err != nil {
		return fmt.Errorf("nack: push to %s: %w", dest, err)
	}

	if err := q.releaseClaim(ctx, claimed.ID, raw); err != nil {
		return fmt.Errorf("nack: %w", err)
	}
	return nil
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
		var entry processingEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			// Corrupt processing entry: quarantine the raw bytes to the
			// dead-letter list — the same no-silent-loss stance as
			// Dequeue's bad-JSON path on the main list — instead of
			// deleting them outright. This hash holds the claimed job's
			// ONLY durable copy. Push first and HDel only if the push
			// landed; on push failure the entry stays in the processing
			// hash, still observable by the next sweep.
			if perr := q.client.LPush(ctx, q.deadLetterQueue, raw); perr != nil {
				continue
			}
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

// Close is a no-op for RedisQueue, the caller manages the Redis connection.
func (q *RedisQueue) Close() error {
	return nil
}

// redisRandomID generates a 16-byte hex string ID.
func redisRandomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
