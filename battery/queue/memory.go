package queue

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// defaultHandlerTimeout is the handler execution timeout used when none is
// configured. Kept at 30 s for backward compatibility.
const defaultHandlerTimeout = 30 * time.Second

// MemoryQueueOption is a functional option for NewMemoryQueue.
type MemoryQueueOption func(*MemoryQueue)

// WithHandlerTimeout sets the per-job execution timeout for the automatic
// worker pool. The job's context is cancelled at the deadline and the
// worker stops waiting there: a handler that ignores its context cannot
// wedge its worker forever (one stuck job would otherwise stall the whole
// queue); the job is retried as a failure and a late side effect counts
// as the at-least-once duplicate. Defaults to 30 s.
func WithHandlerTimeout(d time.Duration) MemoryQueueOption {
	return func(q *MemoryQueue) { q.handlerTimeout = d }
}

// WithLogger sets the logger used for handler-failure (WARN) and
// dead-letter (ERROR) records emitted from the worker pool. Defaults to
// slog.Default(); passing nil restores the default. Unprefixed to match
// the MemoryQueue's WithHandlerTimeout (the DBQueue uses WithDBLogger).
func WithLogger(l *slog.Logger) MemoryQueueOption {
	return func(q *MemoryQueue) {
		if l == nil {
			l = slog.Default()
		}
		q.logger = l
	}
}

// WithLaneWorkers adds n dedicated worker goroutines that ONLY take jobs
// whose Lane equals the given lane, on top of the shared workers passed to
// NewMemoryQueue. This mirrors DBQueue.WithLaneWorkers and prevents a bulk
// backfill from starving urgent jobs even under worker saturation. Multiple
// calls for different lanes each add their own workers; multiple calls for
// the same lane sum. Panics if n <= 0 or lane is "" (use the shared worker
// count for the default lane).
func WithLaneWorkers(lane string, n int) MemoryQueueOption {
	if n <= 0 {
		panic(fmt.Sprintf("queue.WithLaneWorkers: n must be > 0, got %d", n))
	}
	if lane == "" {
		panic("queue.WithLaneWorkers: lane must be non-empty (use the shared worker count for the default lane)")
	}
	return func(q *MemoryQueue) {
		if q.laneWorkers == nil {
			q.laneWorkers = map[string]int{}
		}
		q.laneWorkers[lane] += n
	}
}

// MemoryQueue is an in-memory queue backed by a goroutine pool. Pending jobs
// live in a priority heap (Priority DESC, enqueue-order ASC tiebreak) so
// higher-priority jobs are always taken first, priority is honoured, not
// ignored.
type MemoryQueue struct {
	workers        int
	laneWorkers    map[string]int
	handlerTimeout time.Duration
	logger         *slog.Logger
	handlers       map[string]Handler
	wg             sync.WaitGroup
	mu             sync.RWMutex

	// gate, when set, is checked in processJob after handlerFor. A false
	// return re-enqueues the job with a short delay so it runs when the
	// module re-enables. Framework code uses it to defer jobs owned by a
	// disabled module.
	gate func(jobType string) bool

	// moveMu holds the queue still for an erasure. The manual moves that
	// carry a job from one store to the next (Dequeue: heap to dequeued
	// set; Nack: dequeued set to heap or dead list; Replay: dead list to
	// heap) hold its read side across the whole move, so between their
	// steps the job is in no store; eraseUser holds the write side, so no
	// such move is half done while it reads. Taken before pmu, inflightMu
	// and deadMu, never after.
	moveMu sync.RWMutex

	// pmu guards the pending store, the sequence counter, the closed flag,
	// and the wake cond. cond is bound to pmu.
	pmu     sync.Mutex
	cond    *sync.Cond
	pending pendingHeap
	seq     uint64
	closed  bool

	// now is the clock pending jobs' ScheduledAt is compared against.
	// Defaults to time.Now; tests substitute a fake clock.
	now func() time.Time

	// wake broadcasts on cond when the earliest job held back by its
	// ScheduledAt comes due, so idle workers sleep until then instead of
	// polling. wakeAt is the instant it is armed for, zero when it is not.
	// Guarded by pmu.
	wake   *time.Timer
	wakeAt time.Time

	// held is the set of UniqueKeys live jobs hold, from Enqueue until the
	// job succeeds or is dead-lettered. A retry or a gate deferral puts
	// the job back without taking its key again. Guarded by pmu.
	held map[uniqueSlot]struct{}

	// inflight tracks jobs handed out by manual Dequeue but not yet
	// Ack'd/Nack'd, so Nack(jobID) can re-enqueue the right job. The
	// automatic worker pool processes jobs in-line and never touches this
	// map.
	inflightMu sync.Mutex
	inflight   map[string]Job

	// dead retains jobs that exhausted MaxAttempts (terminally failed) so they
	// can be inspected via Browsable and re-queued via Replay, instead of being
	// silently dropped. Ordered oldest-first. It is BOUNDED at maxDeadJobs:
	// when the cap is reached, the oldest dead job is evicted so a flood of
	// failing jobs can never grow memory without limit. Guarded by deadMu.
	deadMu  sync.Mutex
	dead    []deadJob
	deadSeq uint64

	// eraseName is the name the store eraser NewMemoryQueue registers is
	// listed under; unregisterErase removes it, on Close.
	eraseName       string
	unregisterErase func()

	// replayEnqueued is a deterministic seam for package tests: Replay
	// calls it between its re-enqueue and retiring the dead record.
	// Production code leaves it nil.
	replayEnqueued func()

	// eraseScanned is a seam for package tests: eraseUser calls it after it
	// has read the pending heap, to put a worker step in that gap.
	eraseScanned func()
}

// deadJob is one retained dead-letter record. gen identifies this
// retention: a replayed job that fails again gets a new gen under the same
// ID, so Replay retires only the record it replayed.
type deadJob struct {
	job Job
	gen uint64
}

// uniqueSlot scopes a UniqueKey to its job type.
type uniqueSlot struct{ jobType, key string }

// maxDeadJobs caps the in-memory dead-letter store. Beyond this, the oldest
// retained failed job is dropped to keep memory bounded. The durable DBQueue
// has no such cap (rows persist); this is the price of an in-memory backend.
const maxDeadJobs = 1000

// pendingJob pairs a Job with the monotonic sequence number assigned at
// enqueue, used as the FIFO tiebreak when two jobs share a Priority.
type pendingJob struct {
	job Job
	seq uint64
}

// pendingHeap is a max-priority heap ordered by Priority DESC then enqueue
// sequence ASC. It implements heap.Interface; the root is always the
// highest-priority (earliest-enqueued-among-equal) job.
type pendingHeap struct {
	items []pendingJob
}

func (h pendingHeap) Len() int { return len(h.items) }
func (h pendingHeap) Less(i, j int) bool {
	if h.items[i].job.Priority != h.items[j].job.Priority {
		// Higher priority sorts "less" so it rises to the root.
		return h.items[i].job.Priority > h.items[j].job.Priority
	}
	// Equal priority: earlier enqueue sequence first (FIFO).
	return h.items[i].seq < h.items[j].seq
}
func (h pendingHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *pendingHeap) Push(x any)   { h.items = append(h.items, x.(pendingJob)) }
func (h *pendingHeap) Pop() any {
	n := len(h.items)
	x := h.items[n-1]
	h.items[n-1] = pendingJob{}
	h.items = h.items[:n-1]
	return x
}

// removeMatching scans the heap for the highest-priority job accepted by
// match that is due at now (ScheduledAt zero or not after now) and
// removes+returns it. Because heap storage order is not fully sorted, this
// walks every item and tracks the best via Less. When no due item matches it
// returns ok false and nextDue, the earliest ScheduledAt among the matching
// jobs still held back (zero when there are none). Callers must hold pmu.
func (h *pendingHeap) removeMatching(match func(Job) bool, now time.Time) (pj pendingJob, nextDue time.Time, ok bool) {
	best := -1
	for i := range h.items {
		job := h.items[i].job
		if !match(job) {
			continue
		}
		if job.ScheduledAt.After(now) {
			if nextDue.IsZero() || job.ScheduledAt.Before(nextDue) {
				nextDue = job.ScheduledAt
			}
			continue
		}
		if best == -1 || h.Less(i, best) {
			best = i
		}
	}
	if best == -1 {
		return pendingJob{}, nextDue, false
	}
	return heap.Remove(h, best).(pendingJob), time.Time{}, true
}

// NewMemoryQueue creates a new in-memory queue with the given number of
// shared workers. Optional functional options (e.g. WithHandlerTimeout,
// WithLaneWorkers) may be passed. Shared workers take jobs from any lane by
// priority; dedicated lane workers (added via WithLaneWorkers) take only
// their own lane.
func NewMemoryQueue(workers int, opts ...MemoryQueueOption) *MemoryQueue {
	q := &MemoryQueue{
		workers:        workers,
		handlerTimeout: defaultHandlerTimeout,
		logger:         slog.Default(),
		handlers:       make(map[string]Handler),
		inflight:       make(map[string]Job),
		now:            time.Now,
	}
	q.cond = sync.NewCond(&q.pmu)
	for _, opt := range opts {
		opt(q)
	}
	// App.EraseUserData reaches the jobs this process holds: their
	// payloads are the user's data as much as a queue_jobs row's.
	q.eraseName, q.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "queue_memory",
		Source: "queue",
		Erase:  q.eraseUser,
	})
	return q
}

// eraserName is the name the queue's store eraser is listed under.
func (q *MemoryQueue) eraserName() string { return q.eraseName }

// eraseUser removes every job whose UserID is userID from the pending
// heap (delayed jobs included), the set Dequeue handed out, and the dead
// list, freeing the unique keys they held, and returns how many it
// removed (with dryRun, how many it would). It holds all three stores and
// moveMu, so no Dequeue, Nack or Replay carries a job from a store it has
// not read yet into one it already has. A dequeued job's later Ack or
// Nack finds nothing and does nothing. A job a worker is running when
// the sweep passes is not stopped; if it fails and is retried, it is
// back on the heap until the next erasure.
func (q *MemoryQueue) eraseUser(_ context.Context, userID string, dryRun bool) (int, error) {
	// No manual move is half done, and none starts, until the erasure has
	// read and cleared all three stores.
	q.moveMu.Lock()
	defer q.moveMu.Unlock()
	q.pmu.Lock()
	defer q.pmu.Unlock()
	q.inflightMu.Lock()
	defer q.inflightMu.Unlock()
	q.deadMu.Lock()
	defer q.deadMu.Unlock()

	var gone []Job
	kept := make([]pendingJob, 0, len(q.pending.items))
	for _, pj := range q.pending.items {
		if pj.job.UserID == userID {
			gone = append(gone, pj.job)
		} else {
			kept = append(kept, pj)
		}
	}
	if !dryRun && len(kept) != len(q.pending.items) {
		q.pending.items = kept
		heap.Init(&q.pending)
	}
	if q.eraseScanned != nil {
		q.eraseScanned()
	}

	for _, id := range slices.Sorted(maps.Keys(q.inflight)) {
		if job := q.inflight[id]; job.UserID == userID {
			gone = append(gone, job)
			if !dryRun {
				delete(q.inflight, id)
			}
		}
	}

	n := len(gone)
	keptDead := make([]deadJob, 0, len(q.dead))
	for _, d := range q.dead {
		if d.job.UserID == userID {
			n++
		} else {
			keptDead = append(keptDead, d)
		}
	}
	if !dryRun {
		q.dead = keptDead
		// release, inline: pmu is held.
		for _, job := range gone {
			if job.UniqueKey != "" {
				delete(q.held, uniqueSlot{job.Type, job.UniqueKey})
			}
		}
	}
	return n, nil
}

// RegisterHandler registers a handler function for a given job type.
func (q *MemoryQueue) RegisterHandler(jobType string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[jobType] = handler
}

// SetGate installs a gate checked in processJob after handlerFor. When
// gate returns false the job is re-enqueued with a short delay so it
// runs when the module re-enables. Framework code uses it to defer jobs
// owned by a disabled module. Pass nil to clear.
func (q *MemoryQueue) SetGate(gate func(jobType string) bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gate = gate
}

// Start launches the shared worker goroutines plus one goroutine per
// dedicated lane worker (WithLaneWorkers). Must be called before enqueuing
// jobs if you want automatic processing.
func (q *MemoryQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker("")
	}
	for _, lane := range q.sortedLanes() {
		for i := 0; i < q.laneWorkers[lane]; i++ {
			q.wg.Add(1)
			go q.worker(lane)
		}
	}
}

// sortedLanes returns lane names with dedicated workers in sorted order, for
// deterministic spawn/iteration.
func (q *MemoryQueue) sortedLanes() []string {
	lanes := make([]string, 0, len(q.laneWorkers))
	for lane := range q.laneWorkers {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	return lanes
}

// worker is the per-goroutine loop. lane is "" for a shared worker (takes any
// lane by priority) or the dedicated lane name (takes only that lane). It
// blocks on the cond until a claimable job arrives or Close drains+shuts it
// down.
func (q *MemoryQueue) worker(lane string) {
	defer q.wg.Done()
	for {
		job, ok := q.waitAndPop(lane)
		if !ok {
			return
		}
		q.processJob(job)
	}
}

// waitAndPop blocks until a job claimable by this worker is available, then
// removes and returns it. lane "" claims any job; a non-empty lane claims
// only jobs whose Lane matches. On Close, shared workers drain every
// remaining due job (any lane) before exiting; lane workers exit once their
// lane holds no due job (shared workers pick up the rest). A job whose
// ScheduledAt is still ahead is not claimable: the worker arms the wake
// timer for it and sleeps, and Close does not wait for it.
func (q *MemoryQueue) waitAndPop(lane string) (Job, bool) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	for {
		pj, nextDue, ok := q.pending.removeMatching(matchLane(lane), q.now())
		if ok {
			return pj.job, true
		}
		if q.closed {
			// Nothing claimable remains for this worker. Shared workers
			// already drained every due job via removeMatching above; lane
			// workers leave other lanes to the shared pool.
			return Job{}, false
		}
		if !nextDue.IsZero() {
			q.armWakeLocked(nextDue)
		}
		q.cond.Wait()
	}
}

// armWakeLocked makes sure the wake timer fires no later than at. A timer
// already armed for an earlier instant is left alone: its broadcast makes
// every worker rescan and re-arm for whatever is still held back. Caller
// must hold pmu.
func (q *MemoryQueue) armWakeLocked(at time.Time) {
	if !q.wakeAt.IsZero() && !at.Before(q.wakeAt) {
		return
	}
	q.wakeAt = at
	d := at.Sub(q.now())
	if q.wake == nil {
		q.wake = time.AfterFunc(d, q.fireWake)
		return
	}
	q.wake.Reset(d)
}

// fireWake is the wake timer's callback: it disarms the timer and wakes
// every worker to rescan for jobs that just came due.
func (q *MemoryQueue) fireWake() {
	q.pmu.Lock()
	q.wakeAt = time.Time{}
	q.cond.Broadcast()
	q.pmu.Unlock()
}

// matchLane returns a predicate selecting jobs for a worker: any job when
// lane is "" (shared worker), or only jobs whose Lane equals lane.
func matchLane(lane string) func(Job) bool {
	if lane == "" {
		return func(Job) bool { return true }
	}
	return func(j Job) bool { return j.Lane == lane }
}

// matchTypes returns a predicate selecting jobs by type: any when types is
// empty, or only jobs whose Type is in the set.
func matchTypes(types []string) func(Job) bool {
	if len(types) == 0 {
		return func(Job) bool { return true }
	}
	set := make(map[string]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return func(j Job) bool {
		_, ok := set[j.Type]
		return ok
	}
}

func (q *MemoryQueue) processJob(job Job) {
	q.mu.RLock()
	handler, ok := q.handlers[job.Type]
	gate := q.gate
	q.mu.RUnlock()

	if !ok {
		// No handler registered. Dropping the job here destroyed it with
		// no trace: a producer-first deploy (the queue is fed a type the
		// running binary does not yet know) silently lost every such job,
		// and ListJobs/Stats showed nothing to reconcile from. Retain it
		// as terminally failed instead, where Browsable can see it and
		// Replay can re-run it once the handler ships.
		q.logger.Error("queue: no handler registered; job dead-lettered",
			"job_id", job.ID,
			"job_type", job.Type)
		q.retainDead(job)
		return
	}

	// Gate: defer jobs whose owning module is disabled. Re-enqueue after
	// a short delay so the job runs when the module re-enables without
	// hot-looping the worker.
	if gate != nil && !gateAllows(q.logger, gate, job.Type, job.ID) {
		q.deferGated(job)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), q.handlerTimeout)
	defer cancel()
	err := q.awaitHandler(ctx, handler, job)
	if err == nil {
		q.release(job)
	} else {
		job.Attempts++
		q.logger.Warn("queue: handler failed",
			"job_id", job.ID,
			"job_type", job.Type,
			"attempt", job.Attempts,
			"max_attempts", job.MaxAttempts,
			"err", err)
		if job.Attempts < job.MaxAttempts {
			// Re-enqueue for retry on a FRESH context. `ctx` is the
			// per-attempt one that just timed out / was cancelled; using
			// it here made Enqueue fail on the very jobs retries exist for
			// (the error was discarded, so the retry silently vanished).
			if enqErr := q.requeue(context.Background(), job); enqErr != nil {
				q.logger.Error("queue: job dead-lettered",
					"job_id", job.ID,
					"job_type", job.Type,
					"attempt", job.Attempts,
					"max_attempts", job.MaxAttempts,
					"err", enqErr)
				q.retainDead(job)
			}
		} else {
			// Retries exhausted, retain as terminally-failed for inspection
			// and replay instead of dropping it.
			q.logger.Error("queue: job dead-lettered",
				"job_id", job.ID,
				"job_type", job.Type,
				"attempt", job.Attempts,
				"max_attempts", job.MaxAttempts,
				"err", err)
			q.retainDead(job)
		}
	}
}

// awaitHandler runs the handler with the per-job budget and returns its
// error. The invocation runs on its OWN goroutine, mirroring
// DBQueue.runHandler and the outbox relay's runHandler: a handler that
// ignores its cancelled context cannot be made to return (Go has no
// goroutine termination), and the inline call let one such handler wedge
// its worker forever — one stuck job stalled the whole queue, every later
// job (emails, exports, erasures) waited indefinitely. The worker instead
// stops WAITING at the deadline, treats the job as failed (a duplicate
// side effect on a late completion is the at-least-once duplicate the
// retry path already documents), and keeps draining. The result channel
// is buffered so a late return never leaks the goroutine on the send; a
// handler that never returns holds one goroutine per retry cycle, bounded
func (q *MemoryQueue) awaitHandler(ctx context.Context, handler Handler, job Job) error {
	type outcome struct{ err error }
	done := make(chan outcome, 1)
	// The handler gets its own payload bytes: an edit it makes, or a late
	// write after the budget expires, must not reach the retry's copy.
	job.Payload = slices.Clone(job.Payload)
	go func() { done <- outcome{invokeRecovering(ctx, handler, job)} }()
	timer := time.NewTimer(q.handlerTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.err
	case <-timer.C:
		return fmt.Errorf("queue: handler for %q exceeded its %s budget (context cancelled; job retried)",
			job.Type, q.handlerTimeout)
	}
}

// Enqueue adds a job to the priority-ordered pending store. If the job has no
// ID, one is generated. The store is unbounded, so Enqueue never blocks on
// capacity (unlike the old buffered channel); it returns ErrQueueClosed only
// when the queue has been closed, and ErrDuplicateJob when a live job holds
// the job's UniqueKey.
func (q *MemoryQueue) Enqueue(ctx context.Context, job Job) error {
	return q.enqueue(ctx, job, true)
}

// requeue puts a failed job back for its retry. The job still holds its
// UniqueKey, so it skips the key check that would refuse it.
func (q *MemoryQueue) requeue(ctx context.Context, job Job) error {
	return q.enqueue(ctx, job, false)
}

func (q *MemoryQueue) enqueue(ctx context.Context, job Job, acquire bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if job.ID == "" {
		job.ID = randomID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = q.now()
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	q.pmu.Lock()
	defer q.pmu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	if acquire && job.UniqueKey != "" {
		slot := uniqueSlot{job.Type, job.UniqueKey}
		if _, ok := q.held[slot]; ok {
			return ErrDuplicateJob
		}
		if q.held == nil {
			q.held = map[uniqueSlot]struct{}{}
		}
		q.held[slot] = struct{}{}
	}
	q.pushLocked(job)
	return nil
}

// release frees the job's UniqueKey once the job has succeeded or been
// dead-lettered.
func (q *MemoryQueue) release(job Job) {
	if job.UniqueKey == "" {
		return
	}
	q.pmu.Lock()
	delete(q.held, uniqueSlot{job.Type, job.UniqueKey})
	q.pmu.Unlock()
}

// pushLocked appends a job to the pending heap and wakes one waiting worker.
// Caller must hold pmu.
func (q *MemoryQueue) pushLocked(job Job) {
	job.Payload = slices.Clone(job.Payload)
	q.seq++
	heap.Push(&q.pending, pendingJob{job: job, seq: q.seq})
	// Broadcast rather than Signal so a lane worker whose lane just got a
	// job is guaranteed to wake even when a different (non-matching) worker
	// is at the head of the cond's wait queue.
	q.cond.Broadcast()
}

// Dequeue retrieves the highest-priority job from the pending store,
// optionally filtered by type. Useful for manual consumption without the
// automatic worker pool. It is non-blocking: returns ErrNoJob when nothing
// matches. Jobs of every lane are eligible (dedicated lane filtering is a
// worker-pool concept, not a manual-consumption one); a job whose
// ScheduledAt is still ahead is not.
func (q *MemoryQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	select {
	case <-ctx.Done():
		return Job{}, ctx.Err()
	default:
	}
	q.moveMu.RLock()
	defer q.moveMu.RUnlock()
	q.pmu.Lock()
	if q.closed {
		q.pmu.Unlock()
		return Job{}, ErrQueueClosed
	}
	pj, _, ok := q.pending.removeMatching(matchTypes(types), q.now())
	q.pmu.Unlock()
	if !ok {
		return Job{}, ErrNoJob
	}
	q.trackInflight(pj.job)
	return pj.job, nil
}

// trackInflight records a manually-dequeued job so a later Nack(jobID) can
// re-enqueue it. Jobs with no ID are skipped (Enqueue assigns one, so this is
// only hit for externally-constructed jobs that bypassed Enqueue).
func (q *MemoryQueue) trackInflight(job Job) {
	if job.ID == "" {
		return
	}
	q.inflightMu.Lock()
	job.Payload = slices.Clone(job.Payload)
	q.inflight[job.ID] = job
	q.inflightMu.Unlock()
}

// takeInflight removes and returns a tracked in-flight job by ID.
func (q *MemoryQueue) takeInflight(jobID string) (Job, bool) {
	q.inflightMu.Lock()
	defer q.inflightMu.Unlock()
	job, ok := q.inflight[jobID]
	if ok {
		delete(q.inflight, jobID)
	}
	return job, ok
}

// deferGated puts a gate-deferred job back on the pending heap, not due
// until gateDeferDelay from now, the way DBQueue pushes scheduled_at
// forward. The heap already holds jobs that are not yet due, so a worker
// arms its wake for it, and an erasure that sweeps the heap finds it. A
// job held in a timer closure instead was out of every store eraseUser
// reads, and ran for an erased user once its module came back. After
// Close the job is dropped, the same as everything else on the heap.
func (q *MemoryQueue) deferGated(job Job) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	if q.closed {
		return
	}
	job.ScheduledAt = q.now().Add(gateDeferDelay)
	q.pushLocked(job)
}

// randomID generates a 16-byte hex string ID.
func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Ack confirms a manually-dequeued job is done, discarding any tracked
// in-flight copy. For jobs processed by the automatic worker pool it is a
// no-op (those are auto-acknowledged after successful handler execution).
func (q *MemoryQueue) Ack(_ context.Context, job Job) error {
	if stored, ok := q.takeInflight(job.ID); ok {
		q.release(stored)
	}
	return nil
}

// Nack marks a manually-dequeued job as failed: it increments the attempt
// counter and re-enqueues the job when retries remain, otherwise drops it.
// The job must have been handed out by Dequeue (the in-flight set is consulted
// by ID). Jobs processed by the automatic worker pool retry internally and are
// never in the in-flight set; calling Nack for one is a harmless no-op.
//
// The re-enqueue is lossless, the same write-to-next-home-before-drop rule
// Replay, RedisQueue.Nack, and processJob's own retry path follow: when
// Enqueue fails (typically because the CALLER's context is dead — a request
// disconnect fires r.Context(), which battery/admin passes through here),
// the job has already left the in-flight set, so it is retained in the
// dead-letter set (ListJobs("failed")/Replay can recover it) and the failure
// is logged before the error is returned. Without this, a cancelled request
// context destroyed the job outright: not pending, not dead, not in-flight,
// and a retried Nack is the documented no-op against a vanished job.
func (q *MemoryQueue) Nack(ctx context.Context, job Job) error {
	q.moveMu.RLock()
	defer q.moveMu.RUnlock()
	stored, ok := q.takeInflight(job.ID)
	if !ok {
		// Unknown job (auto-pool retry, or already acked), nothing to requeue.
		return nil
	}
	stored.Attempts++
	if stored.MaxAttempts > 0 && stored.Attempts >= stored.MaxAttempts {
		// Retries exhausted, retain as terminally-failed (inspectable via
		// ListJobs/Stats and re-queuable via Replay) rather than dropping it.
		q.retainDead(stored)
		return nil
	}
	if err := q.requeue(ctx, stored); err != nil {
		q.logger.Error("queue: job dead-lettered",
			"job_id", stored.ID,
			"job_type", stored.Type,
			"attempt", stored.Attempts,
			"max_attempts", stored.MaxAttempts,
			"err", err)
		q.retainDead(stored)
		return err
	}
	return nil
}

// retainDead stores a terminally-failed job in the bounded dead-letter set.
// When the cap is reached, the oldest dead job is evicted so memory stays
// bounded under a flood of failures. The job's Attempts is left at its
// exhausted value so inspection reflects how many tries it took.
func (q *MemoryQueue) retainDead(job Job) {
	q.release(job)
	if job.ID == "" {
		// An ID is required to inspect/replay a job; skip ID-less jobs.
		return
	}
	job.Payload = slices.Clone(job.Payload)
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	q.deadSeq++
	entry := deadJob{job: job, gen: q.deadSeq}
	// De-dupe by ID so a re-failed replayed job doesn't appear twice.
	for i, d := range q.dead {
		if d.job.ID == job.ID {
			q.dead[i] = entry
			return
		}
	}
	q.dead = append(q.dead, entry)
	if len(q.dead) > maxDeadJobs {
		// Drop the oldest. Copy the retained tail into a fresh slice so the
		// dropped head can be garbage-collected (a reslice would keep it alive).
		drop := len(q.dead) - maxDeadJobs
		kept := make([]deadJob, maxDeadJobs)
		copy(kept, q.dead[drop:])
		q.dead = kept
	}
}

// ListJobs implements [Browsable] for the in-memory backend. The only state it
// can enumerate is the retained dead-letter set, so it returns those jobs for
// status "failed" (or an empty/"all" status), newest-first, and nothing for any
// other status, pending/claimed jobs live transiently on the pending heap.
// It skips the newest offset of them. limit <= 0 defaults to 100; a
// negative offset reads as zero.
func (q *MemoryQueue) ListJobs(_ context.Context, status string, limit, offset int) ([]Job, error) {
	if status != "" && status != "all" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	skip := min(max(offset, 0), len(q.dead))
	out := make([]Job, 0, min(limit, len(q.dead)-skip))
	// Newest-first: walk the oldest-first slice in reverse.
	for i := len(q.dead) - 1 - skip; i >= 0 && len(out) < limit; i-- {
		job := q.dead[i].job
		job.Payload = slices.Clone(job.Payload)
		out = append(out, job)
	}
	return out, nil
}

// Stats implements [Browsable]. The in-memory backend can only count the
// retained dead-letter jobs (pending/claimed jobs are transient on the
// heap), so it reports those under "failed".
func (q *MemoryQueue) Stats(_ context.Context) (JobStats, error) {
	q.deadMu.Lock()
	n := len(q.dead)
	q.deadMu.Unlock()
	stats := JobStats{}
	if n > 0 {
		stats["failed"] = n
	}
	return stats, nil
}

// Replay implements [Replayable]: it moves a retained terminally-failed job
// back onto the pending set with Attempts reset to 0 so Dequeue returns it
// again. Idempotent and safe: replaying an unknown or non-failed id matches no
// retained job and is a no-op (nil), never a double-enqueue.
func (q *MemoryQueue) Replay(ctx context.Context, jobID string) error {
	q.moveMu.RLock()
	defer q.moveMu.RUnlock()
	q.deadMu.Lock()
	idx := -1
	for i, d := range q.dead {
		if d.job.ID == jobID {
			idx = i
			break
		}
	}
	if idx == -1 {
		q.deadMu.Unlock()
		return nil // unknown / non-failed id, no-op
	}
	job, gen := q.dead[idx].job, q.dead[idx].gen
	q.deadMu.Unlock()

	// Re-enqueue BEFORE dropping the terminal record. Removing it first
	// and then failing the Enqueue (cancelled context, closed queue)
	// destroyed the job outright: the caller sees an error and retries,
	// but the retry now matches no dead entry and takes the documented
	// idempotent no-op path, so the loss is permanent and silent.
	job.Attempts = 0
	if err := q.Enqueue(ctx, job); err != nil {
		return err
	}
	if q.replayEnqueued != nil {
		q.replayEnqueued()
	}

	// The job is queued; retire the terminal record. Re-find it rather
	// than trusting idx: the dead list may have shifted while the lock
	// was released. Match the generation, not just the ID: a worker can
	// run the replayed job out of attempts before this point, and its
	// new dead record carries the same ID — deleting that one left the
	// job in no queue and no dead list. A concurrent Replay that already
	// removed the record leaves nothing to do here, and the duplicate
	// enqueue is inside this queue's at-least-once contract.
	q.deadMu.Lock()
	for i, d := range q.dead {
		if d.gen == gen {
			q.dead = slices.Delete(q.dead, i, i+1)
			break
		}
	}
	q.deadMu.Unlock()
	return nil
}

// Compile-time interface assertions for MemoryQueue.
var (
	_ Queue      = (*MemoryQueue)(nil)
	_ Browsable  = (*MemoryQueue)(nil)
	_ Replayable = (*MemoryQueue)(nil)
)

// Close drains pending jobs and waits for all workers to finish. It signals
// the cond so every waiting worker wakes, processes any remaining claimable
// job, then exits when the store is empty. Shared workers drain every lane;
// lane workers drain only their own. No goroutine leaks, no panics.
//
// Jobs whose ScheduledAt is still ahead are not run early and not waited
// for: an in-memory queue cannot keep them past Close, so they are dropped
// and their count is logged.
//
// Close also removes the queue's store eraser, which NewMemoryQueue
// registered and which otherwise keeps the queue reachable.
func (q *MemoryQueue) Close() error {
	if q.unregisterErase != nil {
		q.unregisterErase()
	}
	q.pmu.Lock()
	if q.closed {
		q.pmu.Unlock()
		return nil
	}
	q.closed = true
	if q.wake != nil {
		q.wake.Stop()
	}
	q.cond.Broadcast()
	q.pmu.Unlock()

	q.wg.Wait()

	q.pmu.Lock()
	now, held := q.now(), 0
	for _, pj := range q.pending.items {
		if pj.job.ScheduledAt.After(now) {
			held++
		}
	}
	q.pmu.Unlock()
	if held > 0 {
		q.logger.Warn("queue: closed with jobs not yet due; dropped", "count", held)
	}
	return nil
}
