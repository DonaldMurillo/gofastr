# Job queue (`battery/queue`)

`battery/queue` is a pluggable job queue with three backends — in-memory,
SQL (SQLite + Postgres), and Redis. It handles enqueue, dequeue, retry
with optional exponential backoff, dead-letter capture, inspection, and
replay, and pairs with a `Scheduler` for recurring jobs.

## Backends at a glance

| | `MemoryQueue` | `DBQueue` | `RedisQueue` |
|---|---|---|---|
| Durable across restart | No | Yes | Yes |
| Multiple workers | Yes | Yes | Manual |
| Worker loop built in | Yes | Yes | No (bring your own, or use `Start`) |
| Auto-reclaim crashed workers | — | Yes (lease expiry in SQL) | Yes (visibility timeout + `Start`) |
| Dead-letter capture | Yes (bounded, 1 000 jobs) | Yes (status `failed`) | Yes (Redis list) |
| `Browsable` | Yes | Yes | Yes (dead-letter only) |
| `Replayable` | Yes | Yes | Yes |
| Scheduler integration | Yes | Yes | Yes |

Pick `MemoryQueue` for tests and single-process prototypes. Use `DBQueue`
when you need durability and multi-replica safety (Postgres `FOR UPDATE
SKIP LOCKED`). Use `RedisQueue` when you already run Redis and want the
visibility-timeout model.

## Quickstart — MemoryQueue

```go
import "github.com/DonaldMurillo/gofastr/battery/queue"

q := queue.NewMemoryQueue(4) // 4 worker goroutines

q.RegisterHandler("send-email", func(ctx context.Context, job queue.Job) error {
    return sendEmail(ctx, job.Payload)
})

q.Start()
defer q.Close()

_ = q.Enqueue(ctx, queue.Job{
    Type:    "send-email",
    Payload: json.RawMessage(`{"to":"user@example.com"}`),
})
```

## Quickstart — DBQueue

```go
db, _ := sql.Open("postgres", dsn)

q, err := queue.NewDBQueue(db,
    queue.WithWorkers(4),
    queue.WithLeaseTimeout(2*time.Minute),
    queue.WithBackoff(5*time.Second, 5*time.Minute),
    queue.WithDBHandlerTimeout(30*time.Second), // cancel a stuck handler's ctx
)
if err != nil {
    log.Fatal(err)
}

q.RegisterHandler("process-upload", func(ctx context.Context, job queue.Job) error {
    return processUpload(ctx, job.Payload)
})

q.Start(ctx)
defer q.Close()
```

`NewDBQueue` creates the `queue_jobs` table and its index if they do not
exist. Pass `WithTable("my_jobs")` to use a custom table name.

## Quickstart — RedisQueue

```go
// client implements queue.RedisClient — wrap go-redis, redigo, etc.
q := queue.NewRedisQueue(client, "myapp:jobs")
q.SetVisibilityTimeout(30 * time.Second)

// Launch the auto-reclaim ticker (re-delivers crashed-worker jobs).
q.Start(ctx, 30*time.Second)

// Enqueue
_ = q.Enqueue(ctx, queue.Job{Type: "notify", Payload: payload})

// Dequeue + process manually (no built-in worker pool for Redis)
for {
    job, err := q.Dequeue(ctx)
    if errors.Is(err, queue.ErrNoJob) {
        time.Sleep(time.Second)
        continue
    }
    if err := handle(ctx, job); err != nil {
        _ = q.Nack(ctx, job.ID)
    } else {
        _ = q.Ack(ctx, job.ID)
    }
}
```

`RedisQueue` does not include a built-in worker loop — you drive
Dequeue/Ack/Nack yourself, or integrate with a third-party pool. Call
`Start` to enable the auto-reclaim ticker (see "Crash safety" below).

## Job struct

```go
type Job struct {
    ID          string          // auto-filled by Enqueue if empty
    Type        string          // required — selects the handler
    Payload     json.RawMessage // arbitrary JSON for the handler
    Priority    int             // higher = dequeued first (DBQueue only)
    Attempts    int             // incremented on each claim
    MaxAttempts int             // auto-defaults to 3; 0 means 3
    CreatedAt   time.Time       // auto-filled if zero
    ScheduledAt time.Time       // auto-filled to now; set to delay execution
}
```

Scheduled jobs (future `ScheduledAt`) are invisible to `Dequeue` until
the moment passes. This lets you implement delayed processing without a
separate scheduler.

## Retry and backoff

By default, a `Nack` with attempts remaining makes the job immediately
eligible again (next `Dequeue` can pick it up).

`WithBackoff(base, max)` turns on exponential backoff for `DBQueue`:

```go
queue.WithBackoff(5*time.Second, 5*time.Minute)
```

The `n`-th retry delay is `base × 2^(n-1)`, capped at `max`. A job that
Nacks on attempt 1 waits `~5s`; attempt 2 waits `~10s`; attempt 3 waits
`~20s`; etc., up to `5m`.

Once `Attempts >= MaxAttempts`, the job moves to the dead-letter state
instead of being retried.

## Dead-letter and replay

When a job exhausts `MaxAttempts`, it is retained as a terminally-failed
job (never silently dropped):

- **MemoryQueue**: stored in a bounded in-memory slice (cap 1 000; oldest
  evicted on overflow).
- **DBQueue**: row status set to `'failed'`.
- **RedisQueue**: appended to the `<queue>:dead` Redis list.

Replay a failed job (reset attempts to 0 and re-enqueue):

```go
// Type-assert the Replayable capability (all three backends implement it).
if r, ok := q.(queue.Replayable); ok {
    if err := r.Replay(ctx, jobID); err != nil {
        log.Printf("replay failed: %v", err)
    }
}
```

`Replay` is idempotent: replaying an unknown ID or a non-failed job is a
no-op (returns nil, no side effect).

## Inspecting jobs (Browsable)

All three backends implement `Browsable`:

```go
if b, ok := q.(queue.Browsable); ok {
    jobs, _ := b.ListJobs(ctx, "failed", 50)
    stats, _ := b.Stats(ctx)
    fmt.Println("failed:", stats["failed"])
}
```

`ListJobs` accepts a status string (`"pending"`, `"failed"`, `""` for
all) and a limit. Jobs are returned newest-first. `Stats` returns a
`JobStats` map (status → count).

MemoryQueue and RedisQueue can only enumerate their dead-letter store,
so only `"failed"` (or `""`) returns results. DBQueue can enumerate any
status.

## Crash safety and auto-reclaim

**DBQueue** reclaims stale-claimed jobs automatically inside `Dequeue`:
a row in `claimed` status whose `claimed_at` has passed the configured
lease timeout (default 5 min) becomes eligible again. No extra
configuration needed.

**RedisQueue** uses a visibility timeout: while a job is in-flight it
sits in a processing hash with an expiry timestamp. Call
`RedisQueue.Start(ctx, interval)` to run an auto-reclaim ticker:

```go
q.Start(ctx, 30*time.Second) // checks every 30 s; 0 defaults to 30 s
```

The ticker calls `q.Reclaim(ctx)` on each tick, which scans the
processing hash and re-enqueues any job whose `expiresAt` has passed.
Without `Start`, crashed-worker jobs strand silently until you call
`Reclaim` manually.

You can also call `Reclaim` directly from your own ticker:

```go
n, err := q.Reclaim(ctx)
fmt.Printf("reclaimed %d jobs\n", n)
```

## Scheduler

`Scheduler` enqueues recurring jobs onto one or more queue backends:

```go
sched := queue.NewScheduler(q)           // or NewSchedulerWithLogger(q, logger)

// Fixed interval — fires every 5 minutes.
sched.Every(5 * time.Minute).
    Job("send-digest", json.RawMessage(`{}`)).
    Register()

// Cron expression — fires every day at 02:00.
if err := sched.Cron("0 2 * * *").
    Job("nightly-rollup", nil).
    Register(); err != nil {
    log.Fatalf("bad cron spec: %v", err)
}

go sched.Start(ctx) // blocks until ctx is cancelled
```

`Every(d)` schedules fire on a fixed interval; `Cron(spec)` schedules
fire when the cron expression's next time arrives — use it for
time-of-day work like "every day at 02:00" that an interval cannot
express. The spec is parsed by [`framework/cron`](cron.md) (`cron.Parse`),
so the queue does not carry a second cron parser; it accepts the same
5-field syntax and `@shortcuts` (e.g. `@daily`). The two kinds coexist
in one scheduler.

`Register()` returns an `error` only when a `Cron` spec is invalid —
`Every` schedules never error, so existing callers that ignore the
return value are unaffected. `RegisterAt(base)` is the deterministic
variant: it anchors the first run to `base` instead of `time.Now()`,
which is handy for tests and replayed fixtures.

When the scheduler runs, the wake interval is the smallest of the
interval schedules and one minute (cron resolution); a cron-only
scheduler wakes once per minute. **Jobs registered after `Start`
still fire** — the loop re-reads the schedule set each tick and a
`Register` nudges it to re-arm immediately, so the natural "start
subsystems, then register jobs" wiring works (it previously snapshotted
once at `Start` and dropped everything registered later).

**Handler timeout.** By default a DBQueue handler runs unbounded — a
black-holed dependency (an SMTP host that never answers, a hung HTTP
call) wedges the worker forever, and with the default single worker
that stalls the whole queue. Pass `WithDBHandlerTimeout(d)` to cancel
the handler's context at the deadline. The bundled SMTP sender
(`battery/email`) also bounds its own dial at 10s (`SMTPConfig.
DialTimeout`), so it can't hang even without a handler timeout.

Multiple queues can be passed to `NewScheduler` — the job is enqueued
onto all of them. Enqueue errors are logged via `slog.Default()`.
`NewSchedulerWithLogger` lets you supply a custom `*slog.Logger`.

`Scheduler` fires in-process, not via a distributed lock. On multiple
replicas, either run the scheduler on one instance only or gate the
handler behind a lock so the actual work is done once.

## Handler registration

Handlers are registered by job type. Unregistered types are acknowledged
(dropped) so they never loop. Handlers are safe to register concurrently
with a running worker loop.

```go
q.RegisterHandler("resize-image", func(ctx context.Context, job queue.Job) error {
    // Return a non-nil error to Nack (retry or dead-letter).
    return resizeImage(ctx, job.Payload)
})
```

A handler panic is recovered and treated as an error — the job follows the
normal retry path and the worker goroutine is respawned, so a poison
message cannot drain the worker pool.

## RedisClient interface

`RedisQueue` accepts any client that implements `queue.RedisClient`:

```go
type RedisClient interface {
    LPush(ctx, key string, values ...interface{}) error
    RPop(ctx, key string) (string, error)
    HSet(ctx, key string, values ...interface{}) error
    HGet(ctx, key, field string) (string, error)
    HGetAll(ctx, key string) (map[string]string, error)
    HDel(ctx, key string, fields ...string) error
    Del(ctx, keys ...string) error
    LRange(ctx, key string, start, stop int64) ([]string, error)
    LRem(ctx, key string, count int64, value interface{}) (int64, error)
}
```

Wrap your preferred driver (go-redis, redigo, etc.) with a thin adapter
that maps to this interface.

## Sentinel errors

```go
queue.ErrNoJob       // Dequeue: nothing ready right now
queue.ErrQueueClosed // Enqueue: queue was already closed
```

## Common mistakes

- **Not calling `q.Start(ctx, interval)` on RedisQueue.** Without it,
  crashed-worker jobs strand in the processing hash indefinitely.
- **Closing MemoryQueue before workers drain.** `Close` waits for
  in-flight handlers to finish — call it after all producers are done.
- **Replaying a job that is still pending.** `Replay` only touches
  terminal (`failed`) entries — replaying a pending job is a no-op.
- **Running the Scheduler on every replica.** Multiple replicas fire
  the same tick. Either pin the scheduler to one instance or use a DB
  advisory lock to ensure the enqueued work is done once.
- **Ignoring `Nack` errors.** A `Nack` failure means the job stays in
  the processing hash (Redis) or claimed state (DB) and will be
  auto-reclaimed later — but log the error so you can spot connection
  issues early.
