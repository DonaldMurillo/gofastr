package queue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestSchedulesExportPayload(t *testing.T) {
	for _, table := range []string{"queue_jobs", "export_jobs"} {
		t.Run(table, func(t *testing.T) {
			_, queue := openDBQueueOpts(t, WithWorkers(0), WithTable(table))
			scheduler, err := NewDurableScheduler(queue, DurableSchedulerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if err := scheduler.Every("mail", time.Hour).Job("mail", map[string]any{"email": "person@example.com"}).UserID("alice").Register(); err != nil {
				t.Fatal(err)
			}
			app := framework.NewApp(framework.WithDB(queue.db))
			directory := t.TempDir()
			if err := app.ExportData(context.Background(), directory); err != nil {
				t.Fatal(err)
			}
			name := datexport.ScopedName("queue_scheduler_schedules", queue.schedulerName("_scheduler_schedules"))
			data, err := os.ReadFile(filepath.Join(directory, name+".ndjson"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "person@example.com") || !strings.Contains(string(data), "alice") {
				t.Fatalf("schedule export lost personal payload: %s", data)
			}
		})
	}
}
