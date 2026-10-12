package entityui

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// remindJob is a queued run of an app action over 150 drafts that counts
// how often each id reaches Run and records the Run each call carried.
type remindJob struct {
	x     *testUI
	mb    *memBulk
	mu    sync.Mutex
	calls map[string]int
	runs  []string
}

func newRemindJob(t *testing.T) *remindJob {
	t.Helper()
	r := &remindJob{calls: map[string]int{}}
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "remind", Bulk: true,
		Run: func(_ context.Context, ac ActionContext) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, id := range ac.IDs {
				r.calls[id]++
			}
			r.runs = append(r.runs, ac.Run)
			return nil
		},
	}}}}}
	r.x, r.mb, _ = guardedInvoices(t, entity.AccessControl{}, 150, ext, nil)
	r.mb.principal = func(BulkJob) (context.Context, error) { return bulkCtx("u1", nil), nil }
	return r
}

func (r *remindJob) post(t *testing.T) (int, map[string]any) {
	t.Helper()
	return postBulk(t, r.x, bulkCtx("u1", nil), map[string]any{"action": "run:remind", "scope": "every", "match": matchDigest(invoiceIDs(t, r.x))})
}

func (r *remindJob) queue(t *testing.T) string {
	t.Helper()
	if code, out := r.post(t); code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	return r.mb.queued[len(r.mb.queued)-1].ID
}

// A second worker handed the job while the first holds its lease runs
// nothing and answers ErrBulkJobBusy, so no record is acted on twice.
func TestBulkJobSecondWorkerRunsNothing(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	var second error
	r.mb.onSettle = func(string) {
		if second == nil {
			second = r.x.ui.RunBulkJob(context.Background(), id)
		}
	}
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(second, ErrBulkJobBusy) {
		t.Fatalf("SECURITY: the second worker answered %v, want ErrBulkJobBusy", second)
	}
	for rid, n := range r.calls {
		if n != 1 {
			t.Fatalf("SECURITY: record %s reached Run %d times", rid, n)
		}
	}
	if len(r.calls) != 151 {
		t.Fatalf("Run reached %d records, want 151", len(r.calls))
	}
}

// A runner whose lease ran out and was taken over cannot settle or finish
// the job: the holder does.
func TestBulkJobLostLeaseCannotSettle(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	r.mb.onSettle = func(string) {
		r.mb.onSettle = nil
		later := time.Now().Add(2 * bulkLease)
		if ok, err := r.mb.Claim(context.Background(), id, "other", later, later.Add(bulkLease)); err != nil || !ok {
			t.Errorf("the other runner could not take an expired lease: %v %v", ok, err)
		}
	}
	if err := r.x.ui.RunBulkJob(context.Background(), id); !errors.Is(err, ErrBulkLeaseLost) {
		t.Fatalf("SECURITY: RunBulkJob after losing the lease = %v, want ErrBulkLeaseLost", err)
	}
	if n, _ := r.mb.Tally(context.Background(), id); len(n) != 0 {
		t.Fatalf("SECURITY: the runner without the lease settled %v", n)
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Status != BulkQueued {
		t.Fatalf("job status %q, want queued for its holder", j.Status)
	}
}

// A runner that dies holding the lease blocks the job only until the
// lease runs out; then a retry takes it over and finishes.
func TestBulkJobDeadRunnerLeaseExpires(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	now := time.Now()
	if ok, _ := r.mb.Claim(context.Background(), id, "dead", now, now.Add(bulkLease)); !ok {
		t.Fatal("could not plant the dead runner's lease")
	}
	if err := r.x.ui.RunBulkJob(context.Background(), id); !errors.Is(err, ErrBulkJobBusy) {
		t.Fatalf("RunBulkJob under a live lease = %v, want ErrBulkJobBusy", err)
	}
	r.x.ui.now = func() time.Time { return now.Add(bulkLease + time.Second) }
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatalf("RunBulkJob after the lease ran out: %v", err)
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Status != BulkDone {
		t.Fatalf("job status %q, want done", j.Status)
	}
}

// Every call an app action gets under a queued run names the job, the
// same on a retry, so the action can key its effects on it.
func TestBulkJobActionSeesRunID(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	r.mb.failPending = func(call int) bool { return call == 2 }
	if err := r.x.ui.RunBulkJob(context.Background(), id); err == nil {
		t.Fatal("the planted failure did not stop the first call")
	}
	r.mb.failPending = nil
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(r.runs) < 2 {
		t.Fatalf("Run was called %d times, want a call per chunk", len(r.runs))
	}
	for _, run := range r.runs {
		if run != id {
			t.Fatalf("ActionContext.Run = %q, want the job id %q", run, id)
		}
	}
}

// Confirming the same run again while it is queued answers the queued
// job: one job, one Enqueue.
func TestBulkResubmitAnswersQueuedJob(t *testing.T) {
	r := newRemindJob(t)
	first := r.queue(t)
	code, out := r.post(t)
	if code != http.StatusAccepted || out["job"] != first {
		t.Fatalf("resubmit = %d %v, want 202 naming job %s", code, out, first)
	}
	if len(r.mb.queued) != 1 {
		t.Fatalf("SECURITY: the resubmit queued %d jobs, want 1", len(r.mb.queued))
	}
	if err := r.x.ui.RunBulkJob(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	// Once the first run has finished, the same confirm is a new run.
	if code, out := r.post(t); code != http.StatusAccepted || out["job"] == first {
		t.Fatalf("a confirm after the run finished = %d %v, want a new job", code, out)
	}
}

// A queued run writes one audit row, at the end, under the creator's
// rebuilt context and naming the creator; queuing writes none.
func TestBulkQueuedRunAuditsOnce(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	if len(r.mb.audits) != 0 {
		t.Fatalf("queuing wrote %d audit rows, want none until the run ends", len(r.mb.audits))
	}
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(r.mb.audits) != 1 {
		t.Fatalf("the run wrote %d audit rows, want 1", len(r.mb.audits))
	}
	row := r.mb.audits[0]
	if row["actor"] != "u1" || row["detail"].(map[string]any)["creator"] != "u1" {
		t.Fatalf("audit row actor %v detail %v, want u1 both", row["actor"], row["detail"])
	}
}

// An audit row that fails to write leaves the job queued, so the retry
// writes it rather than the run ending with none.
func TestBulkAuditFailureKeepsJobQueued(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	r.mb.failAudit = errors.New("audit db down")
	if err := r.x.ui.RunBulkJob(context.Background(), id); err == nil {
		t.Fatal("SECURITY: the run reported success with no audit row")
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Status != BulkQueued {
		t.Fatalf("job status %q after a failed audit, want queued", j.Status)
	}
	r.mb.failAudit = nil
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Status != BulkDone || len(r.mb.audits) != 1 {
		t.Fatalf("after the retry: status %q, %d audit rows; want done and 1", j.Status, len(r.mb.audits))
	}
	for rid, n := range r.calls {
		if n != 1 {
			t.Fatalf("record %s reached Run %d times across the retry", rid, n)
		}
	}
}

// A job written but never enqueued (the process died in between) is
// handed over by ResumeBulkJobs, once.
func TestResumeBulkJobsEnqueuesOrphans(t *testing.T) {
	r := newRemindJob(t)
	orphan := BulkJob{ID: "orphan", Entity: "invoices", Action: "run:remind", Count: 2, Creator: "u1", Key: "k", Status: BulkQueued}
	if _, err := r.mb.Create(context.Background(), orphan, []string{"q000", "q001"}); err != nil {
		t.Fatal(err)
	}
	id := r.queue(t)
	n, err := r.x.ui.ResumeBulkJobs(context.Background(), 0)
	if err != nil || n != 1 {
		t.Fatalf("ResumeBulkJobs = %d, %v; want the one orphan", n, err)
	}
	if got := r.mb.queued[len(r.mb.queued)-1].ID; got != "orphan" {
		t.Fatalf("resumed %q, want orphan (job %s was enqueued already)", got, id)
	}
	if n, _ := r.x.ui.ResumeBulkJobs(context.Background(), 0); n != 0 {
		t.Fatalf("a second resume handed over %d jobs again", n)
	}
	if _, err := r.x.ui.ResumeBulkJobs(context.Background(), -time.Second); err == nil {
		t.Fatal("a negative grace was accepted")
	}
}

// A job the runner refused is stopped, so a resume does not run it later
// behind the confirmer's back.
func TestBulkRefusedEnqueueIsNotResumed(t *testing.T) {
	r := newRemindJob(t)
	r.mb.failEnqueue = errors.New("queue down")
	if code, _ := r.post(t); code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", code)
	}
	r.mb.failEnqueue = nil
	if n, err := r.x.ui.ResumeBulkJobs(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("ResumeBulkJobs = %d, %v; the refused job must stay stopped", n, err)
	}
}

// A runner whose Enqueue panics takes the refused path: the confirm
// answers 500 and a resume never runs the job behind the confirmer.
func TestBulkPanickingEnqueueIsNotResumed(t *testing.T) {
	r := newRemindJob(t)
	r.mb.panicEnqueue = true
	if code, _ := r.post(t); code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", code)
	}
	r.mb.panicEnqueue = false
	if n, err := r.x.ui.ResumeBulkJobs(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("SECURITY: ResumeBulkJobs = %d, %v; a job whose Enqueue panicked must stay stopped", n, err)
	}
}

// A Principal that panics stops the run like one that errors: nothing
// is written and the job closes as stopped, not held under its lease.
func TestBulkPanickingPrincipalStopsTheRun(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	r.mb.principal = func(BulkJob) (context.Context, error) { panic("principal") }
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatalf("RunBulkJob with a panicking Principal = %v, want the run stopped", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("SECURITY: the run reached %d records without a creator context", len(r.calls))
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Status != BulkStopped {
		t.Fatalf("job status %q, want stopped", j.Status)
	}
}

// Finishing a job drops its ids; PruneBulkJobs drops finished jobs past
// the retention and keeps queued ones.
func TestBulkFinishedJobsArePruned(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	if err := r.x.ui.RunBulkJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, kept := r.mb.ids[id]; kept {
		t.Fatal("a finished job kept its ids")
	}
	if j, _ := r.mb.Job(context.Background(), id); j.Done != 151 {
		t.Fatalf("finished job tally Done = %d, want 151", j.Done)
	}
	queued := BulkJob{ID: "waiting", Entity: "invoices", Action: "run:remind", Count: 1, Creator: "u1", Key: "w", Status: BulkQueued}
	if _, err := r.mb.Create(context.Background(), queued, []string{"q000"}); err != nil {
		t.Fatal(err)
	}
	if n, err := r.x.ui.PruneBulkJobs(context.Background(), BulkRetention); err != nil || n != 0 {
		t.Fatalf("PruneBulkJobs inside the retention = %d, %v; want 0", n, err)
	}
	r.x.ui.now = func() time.Time { return time.Now().Add(BulkRetention + time.Hour) }
	if n, err := r.x.ui.PruneBulkJobs(context.Background(), BulkRetention); err != nil || n != 1 {
		t.Fatalf("PruneBulkJobs past the retention = %d, %v; want 1", n, err)
	}
	if _, err := r.mb.Job(context.Background(), "waiting"); err != nil {
		t.Fatal("PruneBulkJobs dropped a queued job")
	}
}
