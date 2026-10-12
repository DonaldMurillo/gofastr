package framework

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// recordingJobs is a JobRunner that records what it was handed.
type recordingJobs struct{ got []string }

func (r *recordingJobs) Enqueue(_ context.Context, job entityui.BulkJob) error {
	r.got = append(r.got, job.ID)
	return nil
}

func (r *recordingJobs) Principal(ctx context.Context, _ entityui.BulkJob) (context.Context, error) {
	return ctx, nil
}

// App start hands the runner the job a crash left unenqueued and drops
// a finished job past the retention window.
func TestEntityUIStartResumesAndPrunes(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range []entityui.BulkJob{bulkStoreJob("orphan", "k1", 1), bulkStoreJob("old", "k2", 1)} {
		if _, err := s.Create(ctx, j, []string{"a"}); err != nil {
			t.Fatal(err)
		}
	}
	long := time.Now().Add(-entityui.BulkRetention - time.Hour).UTC()
	if ok, err := s.Claim(ctx, "old", "r1", long, long.Add(time.Minute)); err != nil || !ok {
		t.Fatalf("Claim = %v, %v", ok, err)
	}
	if err := s.Finish(ctx, "old", "r1", entityui.BulkDone, long); err != nil {
		t.Fatal(err)
	}
	jobs := query.QuoteIdent(bulkJobsTable)
	if _, err := db.Exec(`UPDATE `+jobs+` SET created_at = $1`, long); err != nil {
		t.Fatal(err)
	}

	app := entityUIAuditApp(t, db, "notes", false)
	runner := &recordingJobs{}
	app.EntityUI(entityui.Extensions{Jobs: runner})
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	if len(runner.got) != 1 || runner.got[0] != "orphan" {
		t.Fatalf("start enqueued %v, want [orphan]", runner.got)
	}
	if _, err := s.Job(ctx, "old"); err == nil {
		t.Fatal("start kept the finished job past retention")
	}
	if j, err := s.Job(ctx, "orphan"); err != nil || j.Status != entityui.BulkQueued {
		t.Fatalf("orphan after start = %+v, %v", j, err)
	}
}
