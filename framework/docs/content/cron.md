# Cron / scheduled jobs

`framework.Scheduler` is a minimal in-process cron runner for background
work. By default every replica fires every tick, which is fine for a
single instance. For multiple replicas, install a leader-election lease
(`Scheduler.WithLeaderElection`, e.g. `NewPostgresAdvisoryLease`) so only
one replica fires each tick; for durable, exactly-once work, use the
`battery/queue` DB-backed queue instead.

## Cron vs queue: which one?

They solve different problems and often pair up:

| Aspect | `framework.Scheduler` (cron) | `battery/queue` |
|---|---|---|
| Trigger | **Time**: "every 5 min", "0 3 * * *" | **Work**: a job enqueued by code |
| State | In-memory; runs in this process only | DB-backed; survives restart |
| Scale-out | Default: every replica fires. Opt into leader election (`WithLeaderElection`) so one replica fires per tick | Safe across replicas via DB locking |
| Use for | Periodic maintenance, polling, digests | Retries, async side-effects, fan-out, dead-letter |

Rule of thumb: **cron decides _when_, the queue decides _how_ reliably.**
A common pattern is a cron tick that *enqueues* durable jobs:

```go
sched.Register(framework.CronJob{
    Name: "send-due-reminders",
    Spec: "* * * * *", // every minute
    Run: func(ctx context.Context) error {
        return q.Enqueue(ctx, queue.Job{Type: "send-due-reminders"}) // queue does the durable work
    },
})
```

On multiple replicas, either install leader election on the scheduler
(`sched.WithLeaderElection(cron.NewPostgresAdvisoryLease(db, key))`) so
only one replica fires the tick, or run the scheduler on a single
designated instance, then let the queue distribute the actual work. See
"Behaviour & guarantees" and [Leader election](#leader-election).

### Cron expressions on the queue Scheduler

The `battery/queue` `Scheduler` accepts cron specs directly via
`sched.Cron(spec)` (alongside `sched.Every(interval)`), so a recurring
*queue* job can fire at a time of day rather than only on a fixed
interval. It reuses this package's parser: `cron.Parse(spec).Next(t)`
computes each next firing, so there is exactly one cron implementation
in the tree. Use `Cron` on the queue Scheduler when you want the durable,
retrying queue to own a time-of-day job end to end; use the in-process
`framework.Scheduler` (above) when the work is ephemeral and a missed
tick after a restart is acceptable. See
[the queue docs](queue.md) → "Scheduler".

A `cron.Schedule` answers these questions about a spec:

| Method | Returns |
|---|---|
| `Next(t)` | The first firing strictly after `t`, minute-aligned, in `t`'s location. The zero `Time` when the spec never fires. The search covers `cron.HorizonYears` (401) years, one full Gregorian cycle, so a rare spec such as `"0 0 29 2 */7"` (February 29 on a Sunday, up to 40 years apart) still has an answer. |
| `Matches(t)` | Whether the spec fires in the minute holding `t`. |
| `MatchesDay(t)` | Whether `t`'s date passes the month and day fields, whatever the minute and hour say. |
| `SkipDay(t)` | The next minute a forward walk from `t` must test: 21:00 local of the same date when `MatchesDay(t)` is false and that is later than `t`, else `t` plus a minute. |
| `Satisfiable()` | Whether the spec ever fires. `"0 0 30 2 *"` does not. |
| `FixedTime()` | Whether neither the minute nor the hour field begins with `*`. The durable queue scheduler gives such a spec vixie cron's once-only handling across a DST change. |

## Quickstart

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
import "log"
import "context"
import "database/sql"
var db *sql.DB
var ctx = context.Background()
-->
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

- `*`: every value in range.
- `a-b`: range, e.g. `1-5` for Mon–Fri.
- `a,b,c`: list, e.g. `0,15,30,45`.
- `*/N`: step, e.g. `*/15` for every 15 minutes.
- `a-b/N`: stepped range.
- `a/N`: step from `a` through the field maximum; minute `5/10` means
  `5,15,25,35,45,55`.

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
- **Default: no distributed coordination.** Every replica runs every
  job. For more than one process, either:
  - Install leader election (`Scheduler.WithLeaderElection`) so only the
    lease holder fires each tick (see [Leader election](#leader-election)
    below), or
  - Run the scheduler on exactly one replica (typical for "primary
    worker" patterns), or
  - Use `battery/queue` with a DB lock instead.

## Leader election

`Scheduler.WithLeaderElection` installs a lease so only one replica fires
each tick. That makes cron safe across replicas without a separate worker
tier. The built-in `NewPostgresAdvisoryLease(db, key)` coordinates over a
Postgres advisory lock (`pg_try_advisory_lock`): every replica pointing at
the same Postgres and using the same `key` races for the lock; the holder
fires, the others skip the tick.

```go
import "github.com/DonaldMurillo/gofastr/framework/cron"

sched := framework.NewScheduler()
sched.WithLeaderElection(cron.NewPostgresAdvisoryLease(db, 70_011)) // fixed key
sched.Register(framework.CronJob{Name: "purge", Spec: "@daily", Run: …})
sched.Start(ctx)
```

Pick a fixed `key` that does not collide with the framework's own
advisory locks (the migration advisory lock).

A lock alone does not stop a second replica whose timer fires for the
same minute a moment after the leader let go. `NewPostgresAdvisoryLease`
also records the minute it fired in a `gofastr_cron_ticks` table (one row
per key, created on first use) with a conditional upsert, and a replica
that finds the minute, or a later one, already claimed skips the tick.
That claim is what keeps the minute from running twice, so the scheduler
releases the lock as soon as the claim is made, before the jobs start. A
long job does not hold the lock: a 20-minute `@hourly` report runs while
the `* * * * *` jobs keep firing every minute, and its own next firing
starts concurrently if it overruns (see "No overlap protection" above).
The lock's connection is pinned only for the lock and the claim. Each
tick fires **at most once** across replicas:

- It is not exactly once. A replica that claims a minute and then crashes
  leaves that minute unrun, and a failed claim skips the tick (reported to
  `OnError` as `"(leader-election)"`) rather than risking a second run.
- Claims only move forward. A replica whose clock runs behind cannot fire
  a minute older than the last claim; one whose clock runs ahead fires its
  minute early by the skew. Skew of a minute or more skips minutes.
- One claim covers the whole tick, so every replica sharing a key must
  register the same jobs.

For durable, exactly-once background work with retries, dead-letters, and
survival across restarts, use the DB-backed queue (`battery/queue`)
instead. A custom lease (Redis, etcd, …) only needs to satisfy the
`LeaderElection` interface; to get the at-most-once tick it also
implements `TickLeaderElection`, whose `AcquireTick(ctx, tick)` receives
the tick minute (UTC) and must hold only when it took the lease and
atomically recorded that minute as newer than the last claim; the
scheduler releases it as soon as `AcquireTick` holds. A lease with
`Acquire` alone keeps mutual exclusion while it is held, and nothing
more: the scheduler holds it until that tick's jobs finish (never a job
from an earlier tick), so on such a lease a long job can block the
ticks that come due while it runs.

A custom lease's `Acquire` and the per-tick `release` it returns are
treated like every other scheduler callback: a panic in either is
recovered and routed to `OnError` (as the pseudo-job
`"(leader-election)"`) instead of killing the process, and the affected
tick is skipped.

## Stopping cleanly

```go
go func() {
    <-ctx.Done()
    sched.Stop() // blocks until the run loop exits AND in-flight jobs finish
}()
```

`Stop` is idempotent: repeated calls return immediately after the
first one finishes. It joins in-flight job goroutines without a
deadline; when the join must be bounded, use `StopContext`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := sched.StopContext(ctx); err != nil {
    // a job ignored its cancelled context past the deadline; it was
    // abandoned so shutdown can proceed
}
```

`StopContext` bounds both the tick-loop join and the in-flight job join.
A blocked custom leader-election callback cannot extend that deadline.

`App.AddCron` wires the stop side through `StopContext` automatically,
bounded by the app's shutdown drain deadline, so a hung job cannot stall
`SIGTERM` forever.

## Error handling

A job's returned error is forwarded to `Scheduler.OnError` if set,
otherwise dropped. Errors do not crash the process. Set `OnError` to
plumb cron failures into your existing logger/metrics.

## Registering at app startup

`app.AddCron(scheduler)` is the lifecycle-managed wiring. The
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
and `sched.Stop` directly. `AddCron` is the convenience, not the
contract.

## Common mistakes

- **Running the scheduler on every replica without a lease.** Multiplies
  every job by N. Install `WithLeaderElection`, pick a primary, or use
  the queue.
- **Long jobs without overlap guards.** A 2-minute job on a
  `* * * * *` spec runs twice in parallel after one minute.
- **Logging silently swallowed errors.** Always set `OnError`.
- **Counting on exact-minute timing.** The scheduler is aligned to
  the minute boundary but not to the second. Don't schedule things
  that depend on sub-minute precision.
