package queue

import (
	"testing"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestDurableSchedulerHardeningDefaults(t *testing.T) {
	db, err := gosqlite.Open()
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
	if scheduler.occurrenceRetention != defaultOccurrenceRetention {
		t.Fatalf("occurrence retention = %s, want %s",
			scheduler.occurrenceRetention, defaultOccurrenceRetention)
	}
	if scheduler.maxCatchUpOccurrences != defaultMaxCatchUpOccurrences {
		t.Fatalf("max catch-up occurrences = %d, want %d",
			scheduler.maxCatchUpOccurrences, defaultMaxCatchUpOccurrences)
	}
}
