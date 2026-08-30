package queue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// ============================================================================
// Property: every raw table a battery registers with the EXPORT plane that
// can carry per-user data must also be reachable by the ERASE plane, so
// App.EraseUserData covers what App.ExportData discloses. The datexport
// package documents this as the registering module's duty ("a battery
// registers DataErasers ... so an erasure reaches the same tables an export
// does"). Surfaces: battery/queue/export.go's init() registration (registry
// parity) and the terminal rows DBQueue retains (end-to-end erasure reach).
//
// RED (2026-08-30 pass): battery/queue registers queue_jobs as a DataExporter
// (payload column included) but no DataEraser anywhere in the package, and
// queue_jobs lives outside the entity registry, so EraseUserData — which
// walks owner-scoped entities plus datexport.AllErasers() only — can never
// reach it. A host that enqueues PII-bearing jobs and later erases the user
// keeps the rows forever, and re-exports them verbatim in every
// App.ExportData dump.
// ============================================================================

// TestQueueJobsEraseParity asserts the registry-plane half of the property:
// every exporter this battery registers (Source "queue") has a matching
// DataEraser, so the erase plane can reach the tables the export plane dumps.
func TestQueueJobsEraseParity(t *testing.T) {
	erasers := datexport.AllErasers()
	byName := make(map[string]datexport.DataEraser, len(erasers))
	for _, e := range erasers {
		byName[e.Name] = e
	}
	for _, ex := range datexport.All() {
		if ex.Source != "queue" {
			continue
		}
		e, ok := byName[ex.Name]
		if !ok {
			t.Errorf("exporter %q (table %q) has no matching DataEraser: EraseUserData cannot reach a table ExportData dumps, including its payload column",
				ex.Name, ex.Table)
			continue
		}
		if e.Table != ex.Table {
			t.Errorf("eraser %q targets table %q but the exporter dumps %q",
				ex.Name, e.Table, ex.Table)
		}
	}
}

// TestDeadJobsReachableByErasure asserts the row-plane half: a job that
// dead-lettered while carrying the erased user's data in its payload must not
// survive App.EraseUserData. DBQueue's only job-row DELETE is Ack-of-claimed;
// 'failed' rows are retained by design (visible to Stats/ListJobs/Replay),
// so the erasure plane is the only path that can expunge them.
func TestDeadJobsReachableByErasure(t *testing.T) {
	db, q := openDBQueue(t, 0)
	ctx := context.Background()

	if err := q.Enqueue(ctx, Job{
		Type:        "email.send",
		Payload:     json.RawMessage(`{"email":"u1@example.com"}`),
		MaxAttempts: 1,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	// The claim set attempts=1, so attempts >= max_attempts: Nack dead-letters
	// the row to 'failed' (db.go Nack CASE branch), where DBQueue retains it.
	if err := q.Nack(ctx, claimed); err != nil {
		t.Fatalf("nack: %v", err)
	}

	// The right-to-be-forgotten primitive of an app whose DB holds the
	// dead-lettered row. Importing battery/queue registered the exporter, so
	// the export plane sees this table; the erase plane must be able to too.
	app := framework.NewApp(framework.WithDB(db))
	rep, err := app.EraseUserData(ctx, "u1")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	var n int
	var status, payload string
	if err := db.QueryRow(
		`SELECT COUNT(*), COALESCE(MAX(status),''), COALESCE(MAX(payload),'') FROM queue_jobs`,
	).Scan(&n, &status, &payload); err != nil {
		t.Fatalf("inspect queue_jobs: %v", err)
	}
	if n != 0 {
		t.Fatalf("erasure left %d queue_jobs row(s) behind (status=%q payload=%s) while EraseUserData reported success erasing %d row(s): the dead-lettered job carrying the erased user's email survived the right-to-be-forgotten primitive",
			n, status, payload, rep.TotalErased())
	}
}
