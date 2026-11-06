package queue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestJobExportKeepsState(t *testing.T) {
	_, queue := openDBQueueOpts(t, WithWorkers(0))
	if err := queue.Enqueue(context.Background(), Job{ID: "mail", Type: "mail", OccurrenceID: "tick", UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.db.Exec(`UPDATE queue_jobs SET last_error = 'retry', claim_token = 'lease', updated_at = created_at WHERE id = 'mail'`); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := framework.NewApp(framework.WithDB(queue.db)).ExportData(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "queue_jobs.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"occurrence_id", "updated_at", "last_error", "claim_token"} {
		if row[column] == nil || row[column] == "" {
			t.Errorf("export lost %s: %s", column, data)
		}
	}
}
