package queue

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// TestQueueExporterRegistered verifies the queue_jobs table is registered for
// data export when this package is imported.
func TestQueueExporterRegistered(t *testing.T) {
	var e datexport.DataExporter
	ok := false
	for _, ex := range datexport.All() {
		if ex.Name == "queue_jobs" {
			e, ok = ex, true
		}
	}
	if !ok {
		t.Fatal("queue_jobs exporter not registered")
	}
	if e.Source != "queue" || e.Table != "queue_jobs" {
		t.Errorf("queue_jobs exporter meta = %+v", e)
	}
	if e.PrimaryKey != "id" {
		t.Errorf("queue_jobs primary key = %q, want id", e.PrimaryKey)
	}
	if len(e.Columns) < 11 {
		t.Errorf("queue_jobs columns = %d, want >= 11", len(e.Columns))
	}
}
