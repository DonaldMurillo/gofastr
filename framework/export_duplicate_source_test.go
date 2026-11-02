package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// TestImportRefusesASourceListedTwice: a manifest naming one source twice
// (two archives merged by hand) restored the same file twice. A keyed
// table refused the second copy on its key; a keyless one took every row
// twice and committed.
func TestImportRefusesASourceListedTwice(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		identityExec(t, db, `CREATE TABLE dup_events (kind TEXT, n INTEGER)`)
		for _, q := range []string{
			`INSERT INTO dup_events (kind, n) VALUES ('a', 1)`,
			`INSERT INTO dup_events (kind, n) VALUES ('b', 2)`,
		} {
			identityExec(t, db, q)
		}
		datexport.Register(datexport.DataExporter{
			Name: "dup_events", Source: "test", Table: "dup_events",
			Columns: []string{"kind", "n"},
		})
		t.Cleanup(func() { datexport.Unregister("dup_events") })
		app := NewApp(WithDB(db))
		ctx := context.Background()
		dir := t.TempDir()
		if err := app.ExportData(ctx, dir); err != nil {
			t.Fatal(err)
		}
		mp := filepath.Join(dir, "manifest.json")
		raw, err := os.ReadFile(mp)
		if err != nil {
			t.Fatal(err)
		}
		var m exportManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m.Entities {
			if e.Name == "dup_events" {
				m.Entities = append(m.Entities, e)
				break
			}
		}
		raw, _ = json.Marshal(m)
		if err := os.WriteFile(mp, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		identityExec(t, db, `DELETE FROM dup_events`)
		err = app.ImportData(ctx, dir)
		if n := identityCount(t, db, `SELECT COUNT(*) FROM dup_events`); n != 0 {
			t.Fatalf("restored %d rows from an archive listing dup_events twice (err %v), want the import refused", n, err)
		}
		if err == nil || !strings.Contains(err.Error(), "twice") {
			t.Fatalf("ImportData = %v, want a refusal naming the repeated source", err)
		}
	})
}
