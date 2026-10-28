package a

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"lib"
)

type Job struct{ ID, Type string }

type Handler func(ctx context.Context, job Job) error

// ---- B29 reduced: DBQueue.workerLoop through two hops of facts ----

type Queue struct {
	handlers map[string]Handler
	db       *sql.DB
}

func invoke(ctx context.Context, h Handler, job Job) error { // want invoke:`runsCallback\[0\]`
	ctx = context.WithValue(ctx, ctxKey{}, job.ID) // a tracer span in the original
	return h(ctx, job)
}

type ctxKey struct{}

func (q *Queue) runHandler(ctx context.Context, h Handler, job Job) error { // want runHandler:`runsCallback\[0\]`
	hctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- invoke(hctx, h, job) }()
	return <-done
}

func (q *Queue) Ack(ctx context.Context, job Job) error {
	_, err := q.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = $1", job.ID)
	return err
}

func (q *Queue) fail(ctx context.Context, job Job, reason error) error {
	_, err := q.db.ExecContext(ctx, "UPDATE jobs SET status='pending' WHERE id = $1", job.ID)
	return err
}

func (q *Queue) workerLoop(ctx context.Context, job Job) { // want workerLoop:`runsCallback\[0\]`
	h := q.handlers[job.Type]
	if err := q.runHandler(ctx, h, job); err != nil {
		_ = q.fail(ctx, job, err) // want `fail records an outcome on the context the callback on line 50 ran under`
		return
	}
	_ = q.Ack(ctx, job) // want `Ack records an outcome on the context the callback on line 50 ran under`
}

// The fix: the outbox settleContext spelling, inline.
func (q *Queue) workerLoopFixed(ctx context.Context, job Job) { // want workerLoopFixed:`runsCallback\[0\]`
	h := q.handlers[job.Type]
	err := q.runHandler(ctx, h, job)
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err != nil {
		_ = q.fail(settleCtx, job, err)
		return
	}
	_ = q.Ack(settleCtx, job)
}

// The fix through a same-package helper (detaches fact, bounded).
func (q *Queue) settleContext(ctx context.Context) (context.Context, context.CancelFunc) { // want settleContext:`detaches\(bounded=true\)`
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

func (q *Queue) workerLoopHelper(ctx context.Context, job Job) { // want workerLoopHelper:`runsCallback\[0\]`
	_ = q.runHandler(ctx, q.handlers[job.Type], job)
	sctx, cancel := q.settleContext(ctx)
	defer cancel()
	_ = q.Ack(sctx, job)
}

// The fix through an imported helper.
func (q *Queue) workerLoopLib(ctx context.Context, job Job) { // want workerLoopLib:`runsCallback\[0\]`
	_ = q.runHandler(ctx, q.handlers[job.Type], job)
	sctx, cancel := lib.SettleContext(ctx)
	defer cancel()
	_ = q.Ack(sctx, job)
}

// The fix reassigning the same variable.
func (q *Queue) workerLoopReassign(ctx context.Context, job Job) { // want workerLoopReassign:`runsCallback\[0\]`
	_ = q.runHandler(ctx, q.handlers[job.Type], job)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = q.Ack(ctx, job)
}

// Bounded but valueless: not the shutdown bug.
func (q *Queue) workerLoopBackgroundBounded(ctx context.Context, job Job) { // want workerLoopBackgroundBounded:`runsCallback\[0\]`
	_ = q.runHandler(ctx, q.handlers[job.Type], job)
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = q.Ack(sctx, job)
}

// B27's failure: detached with no bound outlives Stop.
func (q *Queue) workerLoopUnbounded(ctx context.Context, job Job) { // want workerLoopUnbounded:`runsCallback\[0\]`
	_ = q.runHandler(ctx, q.handlers[job.Type], job)
	_ = q.Ack(context.Background(), job)       // want `Ack records an outcome under context.Background\(\); a blocked write outlives shutdown`
	_ = q.Ack(context.WithoutCancel(ctx), job) // want `Ack records an outcome under context.WithoutCancel with no timeout`
	_ = q.Ack(lib.Detach(ctx), job)            // want `Ack records an outcome under context.WithoutCancel with no timeout`
}

// ---- B35 reduced: the outcome write is an Update whose Status was set
// after the business handler returned ----

type Envelope struct {
	ID     string
	Status string
}

type InboundStore interface {
	MarkEnvelopeProcessing(ctx context.Context, id string) error
	UpdateEnvelope(ctx context.Context, e Envelope) error
}

func ProcessInbound(store InboundStore, fn func(ctx context.Context, e Envelope) error) Handler {
	return func(ctx context.Context, job Job) error {
		env := Envelope{ID: job.ID}
		// The processing claim comes before the work: not an outcome.
		if err := store.MarkEnvelopeProcessing(ctx, env.ID); err != nil {
			return err
		}
		claimed := env
		claimed.Status = "processing"
		if err := store.UpdateEnvelope(ctx, claimed); err != nil {
			return err
		}
		if err := fn(ctx, env); err != nil {
			failed := env
			failed.Status = "failed"
			_ = store.UpdateEnvelope(ctx, failed) // want `UpdateEnvelope records an outcome`
			return err
		}
		done := env
		done.Status = "processed"
		return store.UpdateEnvelope(ctx, done) // want `UpdateEnvelope records an outcome`
	}
}

// A Status set before the work, written after it, records no outcome.
func statusBeforeWork(ctx context.Context, store InboundStore, fn func(context.Context) error) error { // want statusBeforeWork:`runsCallback\[0\]`
	env := Envelope{Status: "processing"}
	pre := env
	pre.Status = "processing"
	if err := fn(ctx); err != nil {
		return err
	}
	return store.UpdateEnvelope(ctx, pre)
}

// A composite literal with a Status key is an outcome record.
func statusLiteral(ctx context.Context, store InboundStore, fn func(context.Context) error) error { // want statusLiteral:`runsCallback\[0\]`
	if err := fn(ctx); err != nil {
		return err
	}
	return store.UpdateEnvelope(ctx, Envelope{ID: "x", Status: "done"}) // want `UpdateEnvelope records an outcome`
}

// ---- U51 reduced: the work runs under a derived context, the checkpoint
// under its parent ----

type Action struct {
	Run func(ctx context.Context, ids []string) error
}

type BulkStore interface {
	Settle(ctx context.Context, id string, outcomes map[string]string) error
}

func principal(ctx context.Context, creator string) (context.Context, error) {
	return context.WithValue(ctx, ctxKey{}, creator), nil
}

func runAction(ctx context.Context, a Action, ids []string) bool { // want runAction:`runsCallback\[0\]`
	return a.Run(ctx, ids) == nil
}

func runBulk(ctx context.Context, a Action, ids []string) map[string]string { // want runBulk:`runsCallback\[0\]`
	out := map[string]string{}
	ok := runAction(ctx, a, ids)
	for _, id := range ids {
		out[id] = map[bool]string{true: "done", false: "failed"}[ok]
	}
	return out
}

func RunBulkJob(ctx context.Context, store BulkStore, a Action, id string, ids []string) error { // want RunBulkJob:`runsCallback\[0\]`
	cctx, err := principal(ctx, "creator")
	if err != nil {
		return err
	}
	outcomes := runBulk(cctx, a, ids)
	return store.Settle(ctx, id, outcomes) // want `Settle records an outcome on the context the callback on line 204 ran under`
}

// ---- M19 reduced: a func field runs the seed ----

type EntityConfig struct {
	Seed func(ctx context.Context, db *sql.DB) error
}

func withSeedData(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, "seed")
}

func recordSeeded(ctx context.Context, db *sql.DB, name string) error {
	_, err := db.ExecContext(ctx, "INSERT INTO seeds VALUES ($1)", name)
	return err
}

func runSeeds(ctx context.Context, db *sql.DB, cfgs map[string]EntityConfig) error { // want runSeeds:`runsCallback\[0\]`
	for name, cfg := range cfgs {
		if err := cfg.Seed(withSeedData(ctx), db); err != nil {
			return err
		}
		if err := recordSeeded(ctx, db, name); err != nil { // want `recordSeeded records an outcome`
			return err
		}
	}
	return nil
}

// ---- other roots and hops ----

type Ledger interface {
	Complete(ctx context.Context, id string) error
	MarkDone(ctx context.Context, id string) error
	Finish(ctx context.Context, id string) error
	Load(ctx context.Context, id string) error
	RecordCount(ctx context.Context, id string) (int, error)
	RecordTx(ctx context.Context, tx *sql.Tx, id string) error
}

// Through an imported runsCallback fact.
func viaLib(ctx context.Context, l Ledger, fn func(context.Context) error) error { // want viaLib:`runsCallback\[0\]`
	if err := lib.Run(ctx, fn); err != nil {
		return err
	}
	return l.Complete(ctx, "id") // want `Complete records an outcome`
}

// An imported function that takes a callback but never runs it under ctx.
func viaLookup(ctx context.Context, l Ledger, fn func() error) error {
	if err := lib.Lookup(ctx, fn); err != nil {
		return err
	}
	return l.Complete(ctx, "id")
}

// The request's context is a root of its own.
type web struct {
	l  Ledger
	fn func(ctx context.Context, id string) error
}

func (h *web) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.fn(r.Context(), "id"); err != nil {
		return
	}
	_ = h.l.MarkDone(r.Context(), "id") // want `MarkDone records an outcome`
}

// A context held in a struct field.
type worker struct {
	ctx context.Context
	h   Handler
	q   *Queue
}

func (w *worker) step(job Job) {
	if err := w.h(w.ctx, job); err == nil {
		_ = w.q.Ack(w.ctx, job) // want `Ack records an outcome`
	}
}

// A deferred settle runs after the work wherever it is written.
func deferred(ctx context.Context, l Ledger, fn func(context.Context) error) error { // want deferred:`runsCallback\[0\]`
	defer l.Finish(ctx, "id") // want `Finish records an outcome`
	return fn(ctx)
}

// ---- near misses: quiet ----

// No callback ran: a lookup then an ack is the request itself.
func noCallback(ctx context.Context, l Ledger, q *Queue, job Job) error {
	if err := l.Load(ctx, job.ID); err != nil {
		return err
	}
	return q.Ack(ctx, job)
}

// A local func literal is this function's own code, not a callback.
func localClosure(ctx context.Context, q *Queue, job Job) error {
	step := func(ctx context.Context) error { return nil }
	if err := step(ctx); err != nil {
		return err
	}
	return q.Ack(ctx, job)
}

// Not outcome-named, no status write.
func plainWrite(ctx context.Context, l Ledger, fn func(context.Context) error) error { // want plainWrite:`runsCallback\[0\]`
	if err := fn(ctx); err != nil {
		return err
	}
	return l.Load(ctx, "id")
}

// Outcome-named but returns more than an error.
func notErrorOnly(ctx context.Context, l Ledger, fn func(context.Context) error) error { // want notErrorOnly:`runsCallback\[0\]`
	if err := fn(ctx); err != nil {
		return err
	}
	_, err := l.RecordCount(ctx, "id")
	return err
}

// The settle shares the work's transaction: they commit together.
func sharedTx(ctx context.Context, l Ledger, tx *sql.Tx, fn func(context.Context, *sql.Tx) error) error { // want sharedTx:`runsCallback\[0\]`
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return l.RecordTx(ctx, tx, "id")
}

// A spawned callback has not returned when the ack runs.
func spawned(ctx context.Context, q *Queue, job Job, h Handler) error { // want spawned:`runsCallback\[0\]`
	go h(ctx, job)
	return q.Ack(ctx, job)
}

// The callback ran under a different root.
func otherRoot(ctx, jobCtx context.Context, q *Queue, job Job, h Handler) error { // want otherRoot:`runsCallback\[1\]`
	if err := h(jobCtx, job); err != nil {
		return err
	}
	return q.Ack(ctx, job)
}

// The settle comes before the work.
func settleFirst(ctx context.Context, l Ledger, fn func(context.Context) error) error { // want settleFirst:`runsCallback\[0\]`
	if err := l.MarkDone(ctx, "id"); err != nil {
		return err
	}
	return fn(ctx)
}

// The runner runs the callback under its second context only.
func runUnder(outer, inner context.Context, fn func(context.Context) error) error { // want runUnder:`runsCallback\[1\]`
	_ = outer
	return fn(inner)
}

func indexRespected(ctx, jobCtx context.Context, q *Queue, job Job, fn func(context.Context) error) error { // want indexRespected:`runsCallback\[1\]`
	if err := runUnder(ctx, jobCtx, fn); err != nil {
		return err
	}
	return q.Ack(ctx, job)
}
