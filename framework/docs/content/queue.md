# Background jobs (`battery/queue`)

`battery/queue` provides durable background jobs behind one `Queue` interface across three backends: `MemoryQueue` (tests/single-process), `DBQueue` (SQLite/PostgreSQL production default with `FOR UPDATE SKIP LOCKED`), and `RedisQueue` (any `RedisClient`-shaped adapter).

## Backend comparison

| Feature | `MemoryQueue` | `DBQueue` | `RedisQueue` |
| --- | --- | --- | --- |
| Durability | In-process only | SQL table | Redis lists/hashes |
| Multi-replica safe | No | Yes (`FOR UPDATE SKIP LOCKED`) | Yes (visibility timeout + reclaim) |
| Unique jobs (`UniqueKey`) | Yes | Yes (partial unique index) | No (`ErrUniqueUnsupported`) |
| Ordered jobs (`SequenceKey`) | No (`ErrSequenceUnsupported`) | Yes | No (`ErrSequenceUnsupported`) |
| Age limit (`MaxAge`) | Yes | Yes (failed attempts and expired leases) | Yes (checked in `Nack`) |
| Handler retry errors (`Permanent`, `RetryAfter`) | Yes | Yes | No worker loop |
| Retry schedule and jitter | No (retries at once) | Yes | No |
| `Browsable` | Yes (dead-letter only) | Yes | Yes (dead-letter only) |
| `Replayable` | Yes | Yes | Yes |
| `ReplayReporter` | Yes | Yes | Yes |
| Scheduler integration | Yes | Yes | Yes |

Pick `MemoryQueue` for tests and quick prototypes, `DBQueue` for most production apps (zero extra infrastructure), and `RedisQueue` when you already run Redis and want high-throughput external dispatch.

## Quickstart (`DBQueue`)

```go
import "github.com/DonaldMurillo/gofastr/battery/queue"

q, err := queue.NewDBQueue(db, queue.WithWorkers(4))
if err != nil {
	return err
}

q.RegisterHandler("send_welcome", func(ctx context.Context, job queue.Job) error {
	var payload struct{ UserID string }
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return queue.Permanent(err) // dead-letter immediately, do not retry
	}
	return sendWelcomeEmail(ctx, payload.UserID)
})

app.AddQueue(q) // starts workers on App.Start, drains cleanly on Shutdown
```

Enqueue a job from any handler or hook:

```go
err := q.Enqueue(ctx, queue.Job{
	Type:       "send_welcome",
	Payload:    rawJSON,
	UniqueKey:  "welcome:" + userID, // deduplicate while live
	MaxRetries: 5,
})
```

## Retries, permanent errors, and dead-letter replay

- Return a normal error to retry with exponential backoff and jitter (up to `MaxRetries`, then moved to dead-letter `failed` state).
- Wrap non-retryable failures (bad payload, deleted target) in `queue.Permanent(err)` (matching `queue.ErrPermanent`) to dead-letter immediately.
- Return `queue.RetryAfter(delay, err)` to respect upstream rate-limit `Retry-After` headers.
- Set `MaxAge` to drop stale jobs instead of running them hours late.
- Set `SequenceKey` (on `DBQueue`) to serialize jobs sharing a key (for example per-customer billing updates) while other keys run in parallel.
- Replay dead-letter jobs via `Replayable` / `ReplayReporter` (`ReplayJob(ctx, jobID)`), which `battery/admin` exposes on its Jobs page. Replaying a job whose `UniqueKey` is already held by a live job returns `queue.ErrDuplicateJob`.

## Inspecting jobs (`Browsable`)

All three backends implement `queue.Browsable` (call the methods directly on a concrete `*DBQueue`, or assert when holding a `queue.Queue` interface):

```go
var qIface queue.Queue = q
if b, ok := qIface.(queue.Browsable); ok {
	jobs, _ := b.ListJobs(ctx, "failed", 50, 0)
	stats, _ := b.Stats(ctx)
	fmt.Println("failed:", stats["failed"])
}
```

## Scheduler

`queue.NewScheduler(q)` enqueues recurring jobs on intervals (`Every`) or standard 5-field cron specs (`Cron(spec)`), reusing GoFastr's cron parser (`cron.Parse`) with vixie-cron DST semantics. Each job gets its own payload copy, so changing a job's payload doesn't affect other queues or later runs. For restart continuity and cross-replica occurrence deduplication, use `queue.DurableScheduler.Cron(id, spec)` (see [Cron](cron.md)).

## Redis backend and optional interfaces

`queue.NewRedisQueue(client, queueName, opts...)` accepts a queue name string and any client implementing `queue.RedisClient` (no hardcoded Redis driver dependency). Adapters **must** translate their driver's empty-list sentinel (such as `redis.Nil`) to `queue.ErrRedisEmpty` so worker loops distinguish an empty queue from a backend outage.

### Optional: `ListLengther`

Implement `LLen(ctx, key)` on your Redis adapter so `Browsable.Stats` can report pending and dead-letter depths in `O(1)`.

### Optional: `ScriptRunner`

Implement `Eval` / `EvalSha` on your Redis adapter to enable atomic Lua pop-and-lease / visibility-timeout reclaim scripts across competing workers (use hash-tagged queue keys like `{myapp}:jobs` on Redis Cluster).

## Sentinel errors

```go
queue.ErrQueueClosed         // Enqueue on a stopped queue
queue.ErrDuplicateJob        // Live job already holds UniqueKey
queue.ErrUniqueUnsupported   // UniqueKey on RedisQueue
queue.ErrSequenceUnsupported // SequenceKey on MemoryQueue or RedisQueue
queue.ErrPermanent           // Dead-letter immediately, no retry
queue.ErrRedisEmpty          // Empty Redis list (adapters map driver nil here)
```

## Common mistakes

- **Using `MemoryQueue` in a multi-replica deployment:** jobs vanish on restart and cannot be shared across replicas. Switch to `DBQueue`.
- **Retrying malformed payloads forever:** wrap unmarshal or domain-invalid errors with `queue.Permanent(err)` so they go straight to dead-letter.
- **Forgetting to map `redis.Nil` to `queue.ErrRedisEmpty` in a custom Redis adapter:** the worker loop will treat every empty poll as a transport error.
- **Skipping `UniqueKey` on cron-enqueued jobs:** if multiple replicas schedule the same tick, `UniqueKey` prevents duplicate executions.
- **Holding open database transactions across `q.Enqueue` with a separate connection:** enqueue after commit or use an outbox when enqueueing must be atomic with an entity write.
