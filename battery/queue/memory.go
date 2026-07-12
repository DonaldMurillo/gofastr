package queue

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// defaultHandlerTimeout is the handler execution timeout used when none is
// configured. Kept at 30 s for backward compatibility.
const defaultHandlerTimeout = 30 * time.Second

// MemoryQueueOption is a functional option for NewMemoryQueue.
type MemoryQueueOption func(*MemoryQueue)

// WithHandlerTimeout sets the per-job execution timeout for the automatic
// worker pool. Jobs that run longer than the timeout have their context
// cancelled, which the handler should respect (the job is then retried or
// dead-lettered as usual). Defaults to 30 s.
func WithHandlerTimeout(d time.Duration) MemoryQueueOption {
	return func(q *MemoryQueue) {
		q.handlerTimeout = d
	}
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
// higher-priority jobs are always taken first — priority is honoured, not
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

	// pmu guards the pending store, the sequence counter, the closed flag,
	// and the wake cond. cond is bound to pmu.
	pmu     sync.Mutex
	cond    *sync.Cond
	pending pendingHeap
	seq     uint64
	closed  bool

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
	deadMu sync.Mutex
	dead   []Job
}

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
	h.items = h.items[:n-1]
	return x
}

// removeMatching scans the heap for the highest-priority job accepted by
// match and removes+returns it. Because heap storage order is not fully
// sorted, this walks every item and tracks the best via Less. Returns
// (zero, false) when no item matches. Callers must hold pmu.
func (h *pendingHeap) removeMatching(match func(Job) bool) (pendingJob, bool) {
	best := -1
	for i := range h.items {
		if !match(h.items[i].job) {
			continue
		}
		if best == -1 || h.Less(i, best) {
			best = i
		}
	}
	if best == -1 {
		return pendingJob{}, false
	}
	return heap.Remove(h, best).(pendingJob), true
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
	}
	q.cond = sync.NewCond(&q.pmu)
	for _, opt := range opts {
		opt(q)
	}
	return q
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
// remaining job (any lane) before exiting; lane workers exit once their lane
// is empty (shared workers pick up the rest).
func (q *MemoryQueue) waitAndPop(lane string) (Job, bool) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	for {
		if pj, ok := q.pending.removeMatching(matchLane(lane)); ok {
			return pj.job, true
		}
		if q.closed {
			// Nothing claimable remains for this worker. Shared workers
			// already drained everything via removeMatching above; lane
			// workers leave other lanes to the shared pool.
			return Job{}, false
		}
		q.cond.Wait()
	}
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
		// No handler registered — nothing to do.
		return
	}

	// Gate: defer jobs whose owning module is disabled. Re-enqueue after
	// a short delay so the job runs when the module re-enables without
	// hot-looping the worker.
	if gate != nil && !gate(job.Type) {
		q.deferGated(job)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), q.handlerTimeout)
	defer cancel()

	err := safeHandle(ctx, handler, job)
	if err != nil {
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
			if enqErr := q.Enqueue(context.Background(), job); enqErr != nil {
				q.logger.Error("queue: job dead-lettered",
					"job_id", job.ID,
					"job_type", job.Type,
					"attempt", job.Attempts,
					"max_attempts", job.MaxAttempts,
					"err", enqErr)
				q.retainDead(job)
			}
		} else {
			// Retries exhausted — retain as terminally-failed for inspection
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

// safeHandle invokes a handler, converting a panic into an error so a
// poison-message job cannot unwind the worker goroutine and crash the whole
// process. The panicked job follows the normal retry path.
func safeHandle(ctx context.Context, handler Handler, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: handler for %q panicked: %v", job.Type, r)
		}
	}()
	return handler(ctx, job)
}

// Enqueue adds a job to the priority-ordered pending store. If the job has no
// ID, one is generated. The store is unbounded, so Enqueue never blocks on
// capacity (unlike the old buffered channel); it returns ErrQueueClosed only
// when the queue has been closed.
func (q *MemoryQueue) Enqueue(ctx context.Context, job Job) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if job.ID == "" {
		job.ID = randomID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	q.pmu.Lock()
	defer q.pmu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	q.pushLocked(job)
	return nil
}

// pushLocked appends a job to the pending heap and wakes one waiting worker.
// Caller must hold pmu.
func (q *MemoryQueue) pushLocked(job Job) {
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
// worker-pool concept, not a manual-consumption one).
func (q *MemoryQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	select {
	case <-ctx.Done():
		return Job{}, ctx.Err()
	default:
	}
	q.pmu.Lock()
	if q.closed {
		q.pmu.Unlock()
		return Job{}, ErrQueueClosed
	}
	pj, ok := q.pending.removeMatching(matchTypes(types))
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

// deferGated re-enqueues a gate-deferred job after gateDeferDelay. Because the
// pending store is unbounded the push always succeeds, so there is no
// re-arm-on-full dance; the only failure mode is Close racing the timer,
// which the closed-check under pmu handles cleanly (no panic).
func (q *MemoryQueue) deferGated(job Job) {
	time.AfterFunc(gateDeferDelay, func() {
		q.pmu.Lock()
		defer q.pmu.Unlock()
		if q.closed {
			return
		}
		q.pushLocked(job)
	})
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
func (q *MemoryQueue) Ack(_ context.Context, jobID string) error {
	q.takeInflight(jobID)
	return nil
}

// Nack marks a manually-dequeued job as failed: it increments the attempt
// counter and re-enqueues the job when retries remain, otherwise drops it.
// The job must have been handed out by Dequeue (the in-flight set is consulted
// by ID). Jobs processed by the automatic worker pool retry internally and are
// never in the in-flight set; calling Nack for one is a harmless no-op.
func (q *MemoryQueue) Nack(ctx context.Context, jobID string) error {
	job, ok := q.takeInflight(jobID)
	if !ok {
		// Unknown job (auto-pool retry, or already acked) — nothing to requeue.
		return nil
	}
	job.Attempts++
	if job.MaxAttempts > 0 && job.Attempts >= job.MaxAttempts {
		// Retries exhausted — retain as terminally-failed (inspectable via
		// ListJobs/Stats and re-queuable via Replay) rather than dropping it.
		q.retainDead(job)
		return nil
	}
	return q.Enqueue(ctx, job)
}

// retainDead stores a terminally-failed job in the bounded dead-letter set.
// When the cap is reached, the oldest dead job is evicted so memory stays
// bounded under a flood of failures. The job's Attempts is left at its
// exhausted value so inspection reflects how many tries it took.
func (q *MemoryQueue) retainDead(job Job) {
	if job.ID == "" {
		// An ID is required to inspect/replay a job; skip ID-less jobs.
		return
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	// De-dupe by ID so a re-failed replayed job doesn't appear twice.
	for i, d := range q.dead {
		if d.ID == job.ID {
			q.dead[i] = job
			return
		}
	}
	q.dead = append(q.dead, job)
	if len(q.dead) > maxDeadJobs {
		// Drop the oldest. Copy the retained tail into a fresh slice so the
		// dropped head can be garbage-collected (a reslice would keep it alive).
		drop := len(q.dead) - maxDeadJobs
		kept := make([]Job, maxDeadJobs)
		copy(kept, q.dead[drop:])
		q.dead = kept
	}
}

// ListJobs implements [Browsable] for the in-memory backend. The only state it
// can enumerate is the retained dead-letter set, so it returns those jobs for
// status "failed" (or an empty/"all" status), newest-first, and nothing for any
// other status — pending/claimed jobs live transiently on the pending heap.
// limit <= 0 defaults to 100.
func (q *MemoryQueue) ListJobs(_ context.Context, status string, limit int) ([]Job, error) {
	if status != "" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	out := make([]Job, 0, min(limit, len(q.dead)))
	// Newest-first: walk the oldest-first slice in reverse.
	for i := len(q.dead) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, q.dead[i])
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
	q.deadMu.Lock()
	idx := -1
	for i, d := range q.dead {
		if d.ID == jobID {
			idx = i
			break
		}
	}
	if idx == -1 {
		q.deadMu.Unlock()
		return nil // unknown / non-failed id — no-op
	}
	job := q.dead[idx]
	q.dead = append(q.dead[:idx], q.dead[idx+1:]...)
	q.deadMu.Unlock()

	job.Attempts = 0
	return q.Enqueue(ctx, job)
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
func (q *MemoryQueue) Close() error {
	q.pmu.Lock()
	if q.closed {
		q.pmu.Unlock()
		return nil
	}
	q.closed = true
	q.cond.Broadcast()
	q.pmu.Unlock()

	q.wg.Wait()
	return nil
}
