package queue

import (
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite names the schema on the index, never on the indexed table:
// CREATE INDEX "x" ON "main"."jobs" is a syntax error there.
func TestSQLiteQualifiedQueueBuilds(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	q, err := NewDBQueue(db, WithTable("main.jobs"))
	if err != nil {
		t.Fatalf("NewDBQueue with a schema-qualified table: %v", err)
	}
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("NewDurableScheduler on a schema-qualified queue: %v", err)
	}
	for _, name := range []string{
		"jobs_dequeue_idx", "jobs_lane_idx", "jobs_unique_idx",
		"jobs_scheduler_occurrences_schedule_job_idx",
		"jobs_scheduler_occurrences_created_at_idx",
	} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("index %s count = %d, want 1", name, n)
		}
	}
}
