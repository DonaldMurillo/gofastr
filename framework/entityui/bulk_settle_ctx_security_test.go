package entityui

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestBulkAuditSurvivesHangUp: an in-request bulk run writes its summary
// audit row after the records changed. On the request's own context, a
// client that hung up mid-run cancelled that write and the run left no
// summary row (the battery/auth 79cc5197 shape). The row is written on the
// request's values minus its cancellation.
func TestBulkAuditSurvivesHangUp(t *testing.T) {
	var hangUp context.CancelFunc
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "remind", Bulk: true,
		Run: func(context.Context, ActionContext) error {
			hangUp() // the client goes away while the run is under way
			return nil
		},
	}}}}}
	x, mb, _ := guardedInvoices(t, entity.AccessControl{}, 0, ext, nil)
	ctx, cancel := context.WithCancel(bulkCtx("u1", nil))
	defer cancel()
	hangUp = cancel
	if code, out := postBulk(t, x, ctx, map[string]any{"action": "run:remind", "scope": "selected", "ids": "inv-1"}); code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if d := lastAudit(t, mb); d["done"] != 1 {
		t.Fatalf("summary = %v, want the run's one record", d)
	}
}

// TestBulkLeaseHandBackBounded: a queued run that fails while holding the
// lease hands it back on a context its caller's cancellation cannot end.
// With no bound either, a store that blocks held RunBulkJob, and the
// JobRunner worker behind it, for as long as the store did (d799883c).
func TestBulkLeaseHandBackBounded(t *testing.T) {
	r := newRemindJob(t)
	id := r.queue(t)
	r.mb.failPending = func(call int) bool { return call == 1 }
	if err := r.x.ui.RunBulkJob(context.Background(), id); err == nil {
		t.Fatal("the planted failure did not stop the run")
	}
	r.mb.mu.Lock()
	defer r.mb.mu.Unlock()
	n := len(r.mb.claimDeadlines)
	if n < 2 {
		t.Fatalf("%d Claim calls, want the claim and its hand-back", n)
	}
	if !r.mb.claimDeadlines[n-1] {
		t.Fatal("the lease hand-back ran with no deadline")
	}
}
