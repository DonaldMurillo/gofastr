package framework

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

func TestBulkResumeOnlyExpiredLeases(t *testing.T) {
	database := openTestDB(t, DialectSQLite)
	ctx := context.Background()
	store, err := newSQLBulkStore(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cutoff := now.Add(-time.Minute)
	for _, sample := range []struct {
		id      string
		until   any
		created time.Time
		status  string
	}{
		{"abandoned", now.Add(-2 * time.Minute), now.Add(-10 * time.Minute), entityui.BulkQueued},
		{"live", now.Add(time.Minute), now.Add(-10 * time.Minute), entityui.BulkQueued},
		{"recent-expiry", now.Add(-30 * time.Second), now.Add(-10 * time.Minute), entityui.BulkQueued},
		{"never-started", nil, now.Add(-10 * time.Minute), entityui.BulkQueued},
		{"young", now.Add(-2 * time.Minute), now, entityui.BulkQueued},
		{"finished", now.Add(-2 * time.Minute), now.Add(-10 * time.Minute), entityui.BulkDone},
	} {
		if _, err := store.Create(ctx, bulkStoreJob(sample.id, sample.id, 1), []string{"row"}); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE gofastr_bulk_jobs SET enqueued = 1, lease_until = ?, created_at = ?, status = ? WHERE id = ?`, sample.until, sample.created, sample.status, sample.id); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := store.Unenqueued(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	if !slices.Equal(ids, []string{"abandoned"}) {
		t.Fatalf("resume candidates=%v, want only abandoned", ids)
	}
}
