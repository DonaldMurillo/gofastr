# Cron / scheduled jobs

`framework.Scheduler` is a lightweight in-process cron runner for periodic maintenance, digests, and polling.

- **Cron decides *when*; `battery/queue` decides *how reliably*.** For durable retries and dead-letter handling, either have a cron tick enqueue a `DBQueue` job or use `queue.Scheduler.Cron(spec)` (which shares the same `cron.Parse` engine).
- **Single vs. multi-replica:** by default every replica fires every tick. On multiple replicas, install leader election (`Scheduler.WithLeaderElection(cron.NewPostgresAdvisoryLease(db, key))`), run a single `GOFASTR_ROLE=worker` replica, or enqueue idempotent `DBQueue` jobs.

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

Wire lifecycle management automatically with `app.AddCron(sched)` — `App.Start` starts the scheduler and `App.Shutdown` stops it cleanly via `StopContext` bounded by the shutdown drain deadline.

## Spec syntax and shortcuts

Standard 5-field cron (`minute hour day-of-month month day-of-week`, supporting `*`, `a-b`, `a,b,c`, `*/N`, `a-b/N`, `a/N`) plus shortcuts: `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`.

## Behavior, DST, and leader election

- **Minute resolution & per-job goroutines:** wakes on minute boundaries and runs each job in its own goroutine without overlap protection (guard long jobs with a mutex or DB lock if overlap is unsafe).
- **Vixie-cron DST & late ticks:** fixed-time specs (like `"30 1 * * *"`) fire once per wall-clock day across DST shifts and catch up missed minutes up to 2 hours; wildcard/step specs (`"*/15 * * * *"`) evaluate at the current minute only.
- **Postgres leader election:** `cron.NewPostgresAdvisoryLease(db, key)` combines an advisory lock with a `gofastr_cron_ticks` minute-claim row so each tick fires **at most once** across replicas without holding the lock while long jobs run. Pick a fixed `key` distinct from migration locks and keep replicas in the same timezone.
- **Tracing & error handling:** with `framework.WithTracing()`, each run starts a `cron <name>` trace. A job's returned error is forwarded to `Scheduler.OnError` if set, otherwise logged to `slog.Default()`.

## Common mistakes

- **Running `framework.Scheduler` on multiple replicas without leader election:** runs every job N times. Use `WithLeaderElection`, a single `GOFASTR_ROLE=worker`, or `DBQueue`.
- **Scheduling a slow job every minute without an overlap guard:** a 2-minute job on `* * * * *` runs concurrently with itself.
- **Forgetting to set `OnError`:** errors fall back to `slog.Default()` instead of your metrics/alerting pipeline.
- **Expecting sub-minute precision or crash persistence:** `framework.Scheduler` is minute-aligned and in-memory; use `battery/queue` for durable work.
