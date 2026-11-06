package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

func TestExporterAddsMissingKeyColumns(t *testing.T) {
	app, database := newExportTestApp(t)
	t.Cleanup(func() { _ = database.Close() })
	for _, key := range []string{"pk_id", "pk_id,kind"} {
		t.Run(key, func(t *testing.T) {
			if _, err := database.Exec(`CREATE TABLE restore_keys (pk_id TEXT, kind TEXT, body TEXT, PRIMARY KEY (pk_id, kind))`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = database.Exec(`DROP TABLE restore_keys`) })
			if _, err := database.Exec(`INSERT INTO restore_keys VALUES ('key-1', 'note', 'saved')`); err != nil {
				t.Fatal(err)
			}
			datexport.Register(datexport.DataExporter{Name: "restore_keys", Table: "restore_keys", PrimaryKey: key, Columns: []string{"body", "kind"}})
			t.Cleanup(func() { datexport.Unregister("restore_keys") })
			directory := t.TempDir()
			if err := app.ExportData(context.Background(), directory); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`DELETE FROM restore_keys`); err != nil {
				t.Fatal(err)
			}
			if err := app.ImportData(context.Background(), directory); err != nil {
				t.Fatalf("restore exported archive: %v", err)
			}
			var body string
			if err := database.QueryRow(`SELECT body FROM restore_keys WHERE pk_id = 'key-1' AND kind = 'note'`).Scan(&body); err != nil || body != "saved" {
				t.Fatalf("restored body=%q, error=%v", body, err)
			}
		})
	}
}
