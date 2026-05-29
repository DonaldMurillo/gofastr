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
    queue.WithWorkers(4),
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
    Priority:    0, // higher integers run first
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

`DBQueue` ships durable storage, priority ordering, scheduled jobs,
exponential retry, lease-based worker claim (safe under crashes), and
a `Browsable` view consumed by `battery/admin`'s `/admin/queue` page.

**Lease / crash-safety.** Dequeue claims a row by setting `status='claimed'`
and stamping `claimed_at`. If the worker dies before Ack/Nack the row would
otherwise be stranded; instead, a claimed row whose `claimed_at` is older than
the lease timeout (default 5m, set via `WithLeaseTimeout` / `SetLeaseTimeout`)
becomes eligible for re-dequeue again — as long as it still has attempts left.
A handler that **panics** is recovered and routed through Nack (retry /
dead-letter); the worker goroutine is respawned, so a poison message can never
permanently drain the pool or crash the process. `MemoryQueue` recovers handler
panics the same way.

`RedisQueue` records a visibility timeout per in-flight job; call
`RedisQueue.Reclaim(ctx)` periodically (e.g. from a ticker) to re-deliver jobs
whose worker crashed before Ack/Nack. A malformed list entry is quarantined to
the dead-letter queue rather than dropping the valid jobs queued behind it.

**Don't use `MemoryQueue` for real workloads.** Jobs die with the
process — fine for tests, dangerous for anything users can observe.
