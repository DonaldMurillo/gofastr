package queue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The queue registered its exporter and eraser once, in init, for the
// default table queue_jobs. A DBQueue built WithTable("billing_jobs")
// kept its jobs in a table neither reached: App.EraseUserData left the
// user's rows (payload included) in place and App.ExportData never listed
// them. The outbox and the durable scheduler already registered under
// their configured names; NewDBQueue now does too.
func TestCustomTableJobsAreErased(t *testing.T) {
	ctx := context.Background()
	for _, dl := range []string{"sqlite", "postgres"} {
		t.Run(dl, func(t *testing.T) {
			var q *DBQueue
			if dl == "sqlite" {
				_, q = openDBQueueOpts(t, WithWorkers(0), WithTable("billing_jobs"))
			} else {
				var err error
				if q, err = NewDBQueue(pgtest.DB(t), WithWorkers(0), WithTable("billing_jobs")); err != nil {
					t.Fatal(err)
				}
			}
			name := datexport.ScopedName("queue_jobs", "billing_jobs")
			var exported, erased bool
			for _, ex := range datexport.All() {
				exported = exported || (ex.Name == name && ex.Table == "billing_jobs")
			}
			for _, e := range datexport.AllErasers() {
				erased = erased || (e.Name == name && e.Table == "billing_jobs" && e.Column == "user_id")
			}
			if !exported || !erased {
				t.Fatalf("billing_jobs registered for export %v, for erasure %v; want both", exported, erased)
			}
			if err := q.Enqueue(ctx, Job{Type: "invoice", UserID: "u1",
				Payload: json.RawMessage(`{"card":"u1-secret"}`)}); err != nil {
				t.Fatal(err)
			}
			app := framework.NewApp(framework.WithDB(q.db))
			if _, err := app.EraseUserData(ctx, "u1"); err != nil {
				t.Fatalf("EraseUserData: %v", err)
			}
			if n := countJobs(t, q.db, q.qt()); n != 0 {
				t.Fatalf("%d of u1's jobs survive the erasure in billing_jobs", n)
			}
		})
	}
}
