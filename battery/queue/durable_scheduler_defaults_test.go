package queue

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestDurableSchedulerHardeningDefaults(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewDurableScheduler(q, DurableSchedulerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// The documented defaults (queue.md: 30 days retention, 1000 catch-up
	// occurrences), not the constants: a default change must fail this.
	if scheduler.occurrenceRetention != 30*24*time.Hour {
		t.Fatalf("occurrence retention = %s, want the documented 30*24h", scheduler.occurrenceRetention)
	}
	if scheduler.maxCatchUpOccurrences != 1000 {
		t.Fatalf("max catch-up occurrences = %d, want the documented 1000", scheduler.maxCatchUpOccurrences)
	}
}
