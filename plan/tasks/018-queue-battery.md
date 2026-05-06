# 018 — Queue Battery

**Phase:** 2 (Batteries) | **Depends on:** 004

## Goal
Pluggable job queue. Interface in core, goroutine pool implementation in battery.

## Deliverables
- [ ] `Queue` interface: `Enqueue(ctx, job) (jobID, error)`, `Schedule(ctx, job, at time.Time)`, `RegisterHandler(jobType, fn)`
- [ ] `Job` struct: ID, Type, Payload ([]byte), Attempts, MaxAttempts, Queue, ScheduledAt, CreatedAt
- [ ] `GoroutinePool` implementation: buffered channel + N worker goroutines
- [ ] Job handler registration: map job type → handler func(ctx, Job) error
- [ ] Retry with exponential backoff: 1s, 2s, 4s, 8s, 16s
- [ ] Scheduled jobs: enqueue with `at` time, worker checks and picks up when ready
- [ ] Graceful shutdown: drain in-progress jobs on SIGTERM
- [ ] Metrics: jobs processed, failed, retried

## Acceptance Criteria
- Enqueue + handler executes job
- Failed jobs retry with backoff up to MaxAttempts
- Scheduled jobs don't execute before scheduled time
- Graceful shutdown waits for in-progress jobs
- Race condition safe under concurrent enqueue
