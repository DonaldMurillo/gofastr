# battery/queue

Background-job queue with three backends behind a single `Queue`
interface: `MemoryQueue` (tests/dev), `DBQueue` (durable Postgres/SQLite
— the default for real apps), `RedisQueue`.

**Use this when** the prompt mentions: background job, async task,
"run later", "send email in the background", scheduled job, retry on
failure, job queue, dead-letter, worker pool.

**Import:** `github.com/DonaldMurillo/gofastr/battery/queue`

**Shape (durable, recommended):**
```go
q, err := queue.NewDBQueue(db,
    queue.WithTable("jobs"),
    queue.WithWorkers(4),                  // shared pool — claims any lane
    queue.WithDBLaneWorkers("high", 2),    // reserved lane workers (optional)
)
if err != nil { return err }
q.RegisterHandler("send-welcome", func(ctx context.Context, j queue.Job) error {
    return sendWelcome(ctx, j.Payload)
})
q.Start(ctx)

// Enqueue from anywhere:
_ = q.Enqueue(ctx, queue.Job{
    Type:        "send-welcome",
    Payload:     []byte(`{"user_id":"42"}`),
    Priority:    0, // higher integers run first (DBQueue + MemoryQueue)
    Lane:        "", // "" = default lane; set to reserve capacity (see below)
    ScheduledAt: time.Now().Add(1 * time.Hour),
    MaxAttempts: 5,
})
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `DBQueue` instead:
- `go func() { for { doWork(); time.Sleep(N) } }()` in main
- A `jobs` table you wrote yourself with `status TEXT` + a polling
  loop that does `SELECT ... WHERE status = 'pending'`
- `time.AfterFunc(delay, func() { ... })` to "schedule" a job
- A retry helper like `for i := 0; i < 3; i++ { try(); time.Sleep(...) }`
- `_, _ = db.Exec("INSERT INTO jobs ...")` because "enqueue can fail"

`DBQueue` ships durable storage, priority ordering, **lane reservations**
(`WithDBLaneWorkers`), scheduled jobs, exponential retry, lease-based worker
claim (safe under crashes), and a `Browsable` view consumed by
`battery/admin`'s `/admin/queue` page.
`RedisQueue` and `MemoryQueue` both implement `Browsable` as well —
they surface dead-lettered jobs under the `"failed"` key so the admin
queue page works with any backend.

**Lanes / starvation.** `Priority` only chooses among *pending* jobs when a
worker frees up — it cannot preempt a running handler, so a bulk backfill can
starve urgent jobs by saturating every worker. `WithDBLaneWorkers(lane, n)`
(DBQueue) / `WithLaneWorkers(lane, n)` (MemoryQueue) reserve dedicated workers
that only claim `Job.Lane`-matching jobs; shared workers still take any lane.
`RedisQueue` has no worker loop — run one instance per lane via `queueName`.
MemoryQueue now honours `Priority` too (priority heap), not just DBQueue.

**Lease / crash-safety.** Dequeue claims a row by setting `status='claimed'`
and stamping `claimed_at`. If the worker dies before Ack/Nack the row would
otherwise be stranded; instead, a claimed row whose `claimed_at` is older than
the lease timeout (default 5m, set via `WithLeaseTimeout` / `SetLeaseTimeout`)
becomes eligible for re-dequeue again — as long as it still has attempts left.
A handler that **panics** is recovered and routed through Nack (retry /
dead-letter); the worker goroutine is respawned, so a poison message can never
permanently drain the pool or crash the process. `MemoryQueue` recovers handler
panics the same way.

**Retry backoff.** By default a Nack'd `DBQueue` job retries immediately. Pass
`queue.WithBackoff(base, max)` to delay each retry by `base*2^(attempts-1)`,
capped at `max`, so a flapping handler can't burn through every attempt in a
tight loop. `RegisterHandler` and `SetLeaseTimeout` are safe to call while the
worker loop is running. `MemoryQueue.Nack(jobID)` re-enqueues a manually
dequeued job (incrementing `Attempts`) when retries remain, rather than
silently dropping it.

`RedisQueue` records a visibility timeout per in-flight job; call
`RedisQueue.Reclaim(ctx)` periodically (e.g. from a ticker) to re-deliver jobs
whose worker crashed before Ack/Nack. A malformed list entry is quarantined to
the dead-letter queue rather than dropping the valid jobs queued behind it.

**MemoryQueue handler timeout.** The automatic worker pool times out each
handler after 30 s by default. Pass `queue.WithHandlerTimeout(d)` to
`NewMemoryQueue` for longer-running jobs:
```go
q := queue.NewMemoryQueue(4, queue.WithHandlerTimeout(5*time.Minute))
```

**Choose the scheduler mode deliberately.** `NewInMemoryScheduler` (and its
compatible alias `NewScheduler`) keeps `NextRun` in process memory. Use it only
when one process owns evaluation and restart continuity is irrelevant.
`NewSchedulerWithLogger` routes that mode's enqueue errors to a custom logger.

For multi-replica or restart-safe schedules, use `NewDurableScheduler` with a
`DBQueue`. Give every schedule a stable ID through `Every(id, interval)` or
`Cron(id, spec)`. Definitions and next-run watermarks persist; a heartbeat
lease issues monotonic fence tokens; and each unique `(schedule ID, tick)`
occurrence, watermark advance, and queue job commit in one transaction. Late
evaluation records old ticks as skipped and enqueues only the newest due tick.
The resulting `Job.OccurrenceID` is the stable run-correlation key.

**Per-schedule options.** Both `ScheduleBuilder` and `DurableScheduleBuilder`
expose fluent `Lane`, `Priority`, `MaxAttempts` methods after `Job`. They are
carried unchanged into every fired `Job`: `Lane("bulk")` tags the job for
bulk-lane workers and any shared catch-all worker (tagging alone does not
reserve capacity or keep bulk off interactive workers — see Lanes above),
`Priority(n)` nudges dequeue order, `MaxAttempts(k)` bounds per-occurrence
retries. Omit them for today's defaults (empty lane, 0, 3). On the durable
builder the values PERSIST with the schedule row; re-registering the same ID
updates them without resetting the next-run watermark, and the columns are
added to existing tables by an idempotent migration.

**Don't use `MemoryQueue` for real workloads.** Jobs die with the
process — fine for tests, dangerous for anything users can observe.
