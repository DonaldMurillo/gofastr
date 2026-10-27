package queue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An older build kept "Jobs" and its scheduler tables case-kept. This
// build names jobs and would start empty beside them, so the queue
// refuses to open and prints the rename.
func TestCaseKeptQueueTableRefusedPG(t *testing.T) {
	for _, older := range []string{"Jobs", "Jobs_scheduler_occurrences"} {
		t.Run(older, func(t *testing.T) {
			db := pgtest.DB(t)
			if _, err := db.Exec(`CREATE TABLE "` + older + `" (id TEXT PRIMARY KEY)`); err != nil {
				t.Fatalf("create the older table: %v", err)
			}
			_, err := NewDBQueue(db, WithTable("Jobs"), WithWorkers(0))
			want := `ALTER TABLE "` + older + `" RENAME TO "` + strings.ToLower(older) + `"`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("NewDBQueue over %q: err = %v, want %s", older, err, want)
			}
		})
	}
}

// A queue configured as "Jobs" folds the name the way unquoted DDL would:
// on Postgres it creates, claims and settles jobs. The host registers the
// same "Jobs" for export and erase, and both planes reach that table.
func TestMixedCaseQueueTablePG(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	q, err := NewDBQueue(db, WithTable("Jobs"), WithWorkers(0))
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}
	if err := q.Enqueue(ctx, Job{ID: "j1", Type: "t", UserID: "u1",
		Payload: json.RawMessage(`{"email":"u1@example.com"}`)}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := q.Enqueue(ctx, Job{ID: "j2", Type: "t", UserID: "u2"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := q.Dequeue(ctx, "t")
	if err != nil {
		t.Fatalf("dequeue: %v %v", job, err)
	}
	if err := q.Nack(ctx, job); err != nil {
		t.Fatalf("nack: %v", err)
	}

	datexport.Register(datexport.DataExporter{Name: "mixed_jobs", Source: "queue-test", Table: "Jobs",
		PrimaryKey: "id", Columns: []string{"id", "user_id", "payload"}})
	datexport.RegisterEraser(datexport.DataEraser{Name: "mixed_jobs", Source: "queue-test", Table: "Jobs",
		Column: "user_id", Mode: datexport.EraseDelete})
	t.Cleanup(func() {
		datexport.Unregister("mixed_jobs")
		datexport.UnregisterEraser("mixed_jobs")
	})

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	dump, err := os.ReadFile(filepath.Join(dir, "mixed_jobs.ndjson"))
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if got := strings.Count(string(dump), "\n"); got != 2 || !strings.Contains(string(dump), `"j1"`) {
		t.Errorf("export of Jobs = %q, want the queue's two jobs", dump)
	}

	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	var queued int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE user_id = 'u1'`).Scan(&queued); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if queued != 0 {
		t.Errorf("erasure left %d of u1's jobs in the queue's jobs", queued)
	}
	// The queue still runs on its own table after the erase.
	next, err := q.Dequeue(ctx, "t")
	if err != nil || next.ID != "j2" {
		t.Fatalf("dequeue after erase = %v, %v; want j2", next, err)
	}
}
