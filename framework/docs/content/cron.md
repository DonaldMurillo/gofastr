# Cron / scheduled jobs

`framework.Scheduler` is a minimal in-process cron runner for
single-instance background work. For horizontally-scaled deployments,
use the `battery/queue` DB-backed queue instead.

## Cron vs Queue — which one?

They solve different problems and often pair up:

| | `framework.Scheduler` (cron) | `battery/queue` |
|---|---|---|
| Trigger | **Time** — "every 5 min", "0 3 * * *" | **Work** — a job enqueued by code |
| State | In-memory; runs in this process only | DB-backed; survives restart |
| Scale-out | Single instance (every replica fires — see "Behaviour" below) | Safe across replicas via DB locking |
| Use for | Periodic maintenance, polling, digests | Retries, async side-effects, fan-out, dead-letter |

Rule of thumb: **cron decides _when_, the queue decides _how_ reliably.**
A common pattern is a cron tick that *enqueues* durable jobs:

```go
sched.Every("@every 1m", func(ctx context.Context) error {
    return q.Enqueue(ctx, "send-due-reminders", nil) // queue does the durable work
})
```

On multiple replicas, gate the cron body behind a DB lock (or run the
scheduler on a single designated instance) so the tick fires once — then
let the queue distribute the actual work. See "Behaviour & guarantees".

### Cron expressions on the queue Scheduler

The `battery/queue` `Scheduler` accepts cron specs directly via
`sched.Cron(spec)` (alongside `sched.Every(interval)`), so a recurring
*queue* job can fire at a time of day rather than only on a fixed
interval. It reuses this package's parser — `cron.Parse(spec).Next(t)`
computes each next firing — so there is exactly one cron implementation
in the tree. Use `Cron` on the queue Scheduler when you want the durable,
retrying queue to own a time-of-day job end to end; use the in-process
`framework.Scheduler` (above) when the work is ephemeral and a missed
tick after a restart is acceptable. See
[the queue docs](queue.md) → "Scheduler".

## Quickstart

```go
sched := framework.NewScheduler()
sched.OnError = func(name string, err error) {
    log.Printf("cron %s failed: %v", name, err)
}

if err := sched.Register(framework.CronJob{
    Name: "purge_old_sessions",
    Spec: "@daily",
    Run: func(ctx context.Context) error {
        _, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < NOW()")
        return err
    },
}); err != nil {
    log.Fatal(err)
}

sched.Start(ctx)
defer sched.Stop()
```

## Spec syntax

Standard 5-field cron: `minute hour day-of-month month day-of-week`.

| Field          | Range        |
|----------------|--------------|
| Minute         | 0–59         |
| Hour           | 0–23         |
| Day of month   | 1–31         |
| Month          | 1–12         |
| Day of week    | 0–6 (Sun=0)  |

Supported within each field:

- `*` — every value in range.
- `a-b` — range, e.g. `1-5` for Mon–Fri.
- `a,b,c` — list, e.g. `0,15,30,45`.
- `*/N` — step, e.g. `*/15` for every 15 minutes.
- `a-b/N` — stepped range.

## Shortcuts

| Shortcut    | Equivalent to       |
|-------------|---------------------|
| `@hourly`   | `0 * * * *`         |
| `@daily`    | `0 0 * * *`         |
| `@midnight` | `0 0 * * *`         |
| `@weekly`   | `0 0 * * 0`         |
| `@monthly`  | `0 0 1 * *`         |
| `@yearly`   | `0 0 1 1 *`         |
| `@annually` | `0 0 1 1 *`         |

## Behaviour & guarantees

- **Minute resolution.** The scheduler wakes once per minute, aligned
  to the next minute boundary on `Start`.
- **Per-job goroutine.** Jobs run in their own goroutine; a slow job
  does not block the tick loop.
- **No overlap protection.** If a job runs longer than its interval,
  the next firing starts concurrently. Job code is responsible for
  guarding against overlap (e.g. via a `sync.Mutex` or a DB lock).
- **No persistence.** Pending firings are not durable. If the process
  restarts mid-minute, that minute's jobs are skipped.
- **No distributed coordination.** Every replica runs every job. If
  you run more than one process, either:
  - Run the scheduler on exactly one replica (typical for "primary
    worker" patterns), or
  - Use `battery/queue` with a DB lock instead.

## Stopping cleanly

```go
go func() {
    <-ctx.Done()
    sched.Stop() // blocks until the run loop exits
}()
```

`Stop` is idempotent — repeated calls return immediately after the
first one finishes.

## Error handling

A job's returned error is forwarded to `Scheduler.OnError` if set,
otherwise dropped. Errors do not crash the process. Set `OnError` to
plumb cron failures into your existing logger/metrics.

## Registering at app startup

`app.AddCron(scheduler)` is the lifecycle-managed wiring — the
scheduler starts when the app starts and stops when the app stops:

```go
func main() {
    app := framework.NewApp(framework.WithDB(db))
    // … entity registration …

    sched := framework.NewScheduler()
    sched.Register(framework.CronJob{Name: "purge", Spec: "@daily", Run: …})

    app.AddCron(sched)
    log.Fatal(app.Start(":8080")) // also calls sched.Start; Stop fires on shutdown
}
```

You can still manage the lifecycle yourself by calling `sched.Start`
and `sched.Stop` directly — `AddCron` is the convenience, not the
contract.

## Common mistakes

- **Running the scheduler on every replica.** Multiplies every job by
  N. Pick a primary or use the queue.
- **Long jobs without overlap guards.** A 2-minute job on a
  `* * * * *` spec runs twice in parallel after one minute.
- **Logging silently swallowed errors.** Always set `OnError`.
- **Counting on exact-minute timing.** The scheduler is aligned to
  the minute boundary but not to the second. Don't schedule things
  that depend on sub-minute precision.
