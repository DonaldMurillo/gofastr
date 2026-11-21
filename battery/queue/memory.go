package queue

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

const defaultHandlerTimeout = 30 * time.Second

// MemoryQueueOption is a functional option for NewMemoryQueue.
type MemoryQueueOption func(*MemoryQueue)

// WithHandlerTimeout sets the per-job execution timeout of the worker pool.
func WithHandlerTimeout(d time.Duration) MemoryQueueOption {
	return func(q *MemoryQueue) {
		if d > 0 {
			q.handlerTimeout = d
		}
	}
}

// WithLogger sets the logger for the worker pool's handler-failure and
// dead-letter records.
func WithLogger(l *slog.Logger) MemoryQueueOption {
	return func(q *MemoryQueue) {
		if l == nil {
			l = slog.Default()
		}
		q.logger = l
	}
}

// WithLaneWorkers adds n workers that take only jobs of lane, on top of the
// shared workers.
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

// MemoryQueue is an in-process queue backed by a goroutine pool.
type MemoryQueue struct {
	workers        int
	laneWorkers    map[string]int
	handlerTimeout time.Duration
	logger         *slog.Logger
	handlers       map[string]Handler
	wg             sync.WaitGroup
	mu             sync.RWMutex

	gate func(jobType string) bool

	moveMu sync.RWMutex

	pmu     sync.Mutex
	cond    *sync.Cond
	pending pendingHeap
	seq     uint64
	closed  bool

	now func() time.Time

	wake   *time.Timer
	wakeAt time.Time

	held map[uniqueSlot]struct{}

	inflightMu sync.Mutex
	inflight   map[string]Job

	running []*runningJob

	deadMu  sync.Mutex
	dead    []deadJob
	deadSeq uint64

	eraseName       string
	unregisterErase func()

	replayEnqueued func()

	eraseScanned func()
}

type runningJob struct {
	job    Job
	erased bool
}

type deadJob struct {
	job Job
	gen uint64
}

type uniqueSlot struct{ jobType, key string }

const maxDeadJobs = 1000

type pendingJob struct {
	job Job
	seq uint64
}

type pendingHeap struct {
	items []pendingJob
}

func (h pendingHeap) Len() int { return len(h.items) }
func (h pendingHeap) Less(i, j int) bool {
	if h.items[i].job.Priority != h.items[j].job.Priority {
		return h.items[i].job.Priority > h.items[j].job.Priority
	}
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

// NewMemoryQueue creates an in-memory queue with the given number of shared
// workers.
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
	q.eraseName, q.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "queue_memory",
		Source: "queue",
		Erase:  q.eraseUser,
	})
	return q
}

func (q *MemoryQueue) eraserName() string { return q.eraseName }

func (q *MemoryQueue) eraseUser(_ context.Context, userID string, dryRun bool) (int, error) {
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
	for _, rj := range q.running {
		if rj.job.UserID == userID && !rj.erased {
			gone = append(gone, rj.job)
			if !dryRun {
				rj.erased = true
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

// SetGate installs a gate checked before each job runs.
func (q *MemoryQueue) SetGate(gate func(jobType string) bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gate = gate
}

// Start launches the shared workers and one goroutine per lane worker.
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

func (q *MemoryQueue) sortedLanes() []string {
	lanes := make([]string, 0, len(q.laneWorkers))
	for lane := range q.laneWorkers {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	return lanes
}

func (q *MemoryQueue) worker(lane string) {
	defer q.wg.Done()
	for {
		rj, ok := q.waitAndPop(lane)
		if !ok {
			return
		}
		q.processJob(rj)
	}
}

func (q *MemoryQueue) waitAndPop(lane string) (*runningJob, bool) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	for {
		pj, nextDue, ok := q.pending.removeMatching(matchLane(lane), q.now())
		if ok {
			rj := &runningJob{job: pj.job}
			q.inflightMu.Lock()
			q.running = append(q.running, rj)
			q.inflightMu.Unlock()
			return rj, true
		}
		if q.closed {
			return nil, false
		}
		if !nextDue.IsZero() {
			q.armWakeLocked(nextDue)
		}
		q.cond.Wait()
	}
}

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

func (q *MemoryQueue) fireWake() {
	q.pmu.Lock()
	q.wakeAt = time.Time{}
	q.cond.Broadcast()
	q.pmu.Unlock()
}

func matchLane(lane string) func(Job) bool {
	if lane == "" {
		return func(Job) bool { return true }
	}
	return func(j Job) bool { return j.Lane == lane }
}

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

func (q *MemoryQueue) settleRunning(rj *runningJob, settle func()) {
	q.moveMu.RLock()
	defer q.moveMu.RUnlock()
	q.inflightMu.Lock()
	erased := rj.erased
	if i := slices.Index(q.running, rj); i >= 0 {
		q.running = slices.Delete(q.running, i, i+1)
	}
	q.inflightMu.Unlock()
	if !erased {
		settle()
	}
}

func (q *MemoryQueue) processJob(rj *runningJob) {
	job := rj.job
	q.mu.RLock()
	handler, ok := q.handlers[job.Type]
	gate := q.gate
	q.mu.RUnlock()

	if !ok {
		q.settleRunning(rj, func() {
			q.logger.Error("queue: no handler registered; job dead-lettered",
				"job_id", job.ID,
				"job_type", job.Type)
			q.retainDead(job)
		})
		return
	}

	if gate != nil && !gateAllows(q.logger, gate, job.Type, job.ID) {
		q.settleRunning(rj, func() { q.deferGated(job) })
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), q.handlerTimeout)
	defer cancel()
	err := q.awaitHandler(ctx, handler, job)
	if err == nil {
		q.settleRunning(rj, func() { q.release(job) })
		return
	}
	q.settleRunning(rj, func() {
		job.Attempts++
		q.logger.Warn("queue: handler failed",
			"job_id", job.ID,
			"job_type", job.Type,
			"attempt", job.Attempts,
			"max_attempts", job.MaxAttempts,
			"err", err)
		retry := job.Attempts < job.MaxAttempts && !errors.Is(err, ErrPermanent)
		if retry {
			next := q.now()
			d, delayed := retryDelayFrom(err)
			if delayed {
				next = next.Add(d)
			}
			retry = !pastMaxAge(job, next)
			if retry && delayed {
				job.ScheduledAt = next
			}
		}
		if retry {
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
			q.logger.Error("queue: job dead-lettered",
				"job_id", job.ID,
				"job_type", job.Type,
				"attempt", job.Attempts,
				"max_attempts", job.MaxAttempts,
				"err", err)
			q.retainDead(job)
		}
	})
}

func (q *MemoryQueue) awaitHandler(ctx context.Context, handler Handler, job Job) error {
	type outcome struct{ err error }
	done := make(chan outcome, 1)
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

// Enqueue adds a job to the priority-ordered pending store.
func (q *MemoryQueue) Enqueue(ctx context.Context, job Job) error {
	return q.enqueue(ctx, job, true)
}

func (q *MemoryQueue) requeue(ctx context.Context, job Job) error {
	return q.enqueue(ctx, job, false)
}

func (q *MemoryQueue) enqueue(ctx context.Context, job Job, acquire bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if job.SequenceKey != "" {
		return ErrSequenceUnsupported
	}
	if err := normalizeJob(&job, func(job *Job) { defaultCreatedAt(job, q.now) }); err != nil {
		return err
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

func (q *MemoryQueue) release(job Job) {
	if job.UniqueKey == "" {
		return
	}
	q.pmu.Lock()
	delete(q.held, uniqueSlot{job.Type, job.UniqueKey})
	q.pmu.Unlock()
}

func (q *MemoryQueue) pushLocked(job Job) {
	job.Payload = slices.Clone(job.Payload)
	q.seq++
	heap.Push(&q.pending, pendingJob{job: job, seq: q.seq})
	q.cond.Broadcast()
}

// Dequeue takes the highest-priority pending job, optionally of jobType.
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

func (q *MemoryQueue) trackInflight(job Job) {
	if job.ID == "" {
		return
	}
	q.inflightMu.Lock()
	job.Payload = slices.Clone(job.Payload)
	q.inflight[job.ID] = job
	q.inflightMu.Unlock()
}

func (q *MemoryQueue) takeInflight(jobID string) (Job, bool) {
	q.inflightMu.Lock()
	defer q.inflightMu.Unlock()
	job, ok := q.inflight[jobID]
	if ok {
		delete(q.inflight, jobID)
	}
	return job, ok
}

func (q *MemoryQueue) deferGated(job Job) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	if q.closed {
		return
	}
	job.ScheduledAt = q.now().Add(gateDeferDelay)
	q.pushLocked(job)
}

// Ack confirms a manually dequeued job is done.
func (q *MemoryQueue) Ack(_ context.Context, job Job) error {
	if stored, ok := q.takeInflight(job.ID); ok {
		q.release(stored)
	}
	return nil
}

// counter and re-enqueues the job when retries remain, otherwise retains it
// as terminally failed in the dead-letter set.
func (q *MemoryQueue) Nack(ctx context.Context, job Job) error {
	q.moveMu.RLock()
	defer q.moveMu.RUnlock()
	stored, ok := q.takeInflight(job.ID)
	if !ok {
		return nil
	}
	stored.Attempts++
	if (stored.MaxAttempts > 0 && stored.Attempts >= stored.MaxAttempts) || pastMaxAge(stored, q.now()) {
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

func (q *MemoryQueue) retainDead(job Job) {
	q.release(job)
	if job.ID == "" {
		return
	}
	job.Payload = slices.Clone(job.Payload)
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	q.deadSeq++
	entry := deadJob{job: job, gen: q.deadSeq}
	for i, d := range q.dead {
		if d.job.ID == job.ID {
			q.dead[i] = entry
			return
		}
	}
	q.dead = append(q.dead, entry)
	if len(q.dead) > maxDeadJobs {
		drop := len(q.dead) - maxDeadJobs
		kept := make([]deadJob, maxDeadJobs)
		copy(kept, q.dead[drop:])
		q.dead = kept
	}
}

// ListJobs implements Browsable; it lists dead-letter jobs only.
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
	for i := len(q.dead) - 1 - skip; i >= 0 && len(out) < limit; i-- {
		job := q.dead[i].job
		job.Payload = slices.Clone(job.Payload)
		out = append(out, job)
	}
	return out, nil
}

// Stats implements Browsable.
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

func (q *MemoryQueue) pendingDepth(context.Context) (int, error) {
	q.pmu.Lock()
	defer q.pmu.Unlock()
	return len(q.pending.items), nil
}

// Replay implements Replayable.
func (q *MemoryQueue) Replay(ctx context.Context, jobID string) error {
	_, err := q.ReplayJob(ctx, jobID)
	return err
}

// ReplayJob implements ReplayReporter.
func (q *MemoryQueue) ReplayJob(ctx context.Context, jobID string) (bool, error) {
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
		return false, nil
	}
	job, gen := q.dead[idx].job, q.dead[idx].gen
	q.deadMu.Unlock()

	job.Attempts = 0
	job.ScheduledAt = q.now()
	if err := q.Enqueue(ctx, job); err != nil {
		return false, err
	}
	if q.replayEnqueued != nil {
		q.replayEnqueued()
	}

	q.deadMu.Lock()
	for i, d := range q.dead {
		if d.gen == gen {
			q.dead = slices.Delete(q.dead, i, i+1)
			break
		}
	}
	q.deadMu.Unlock()
	return true, nil
}

var (
	_ Queue      = (*MemoryQueue)(nil)
	_ Browsable  = (*MemoryQueue)(nil)
	_ Replayable = (*MemoryQueue)(nil)

	_ ReplayReporter = (*MemoryQueue)(nil)
)

// Close drains pending jobs and waits for every worker to finish.
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
