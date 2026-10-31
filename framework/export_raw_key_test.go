package framework

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// A raw exporter with no PrimaryKey was paged by a guessed "id". On SQLite
// a table without that column reads "id" as the string literal 'id', so
// paging stopped after the first page and the archive held pageSize rows
// under a valid checksum. An exporter that names a composite key
// ("provider,provider_id", the manifest's own spelling) pages by both.
func TestRawExporterKeyShapes(t *testing.T) {
	for _, tc := range []struct {
		name, ddl, pk string
		insert        func(i int) string
	}{
		{
			name: "no key", ddl: `CREATE TABLE rk_events (kind TEXT, n INTEGER)`,
			insert: func(i int) string { return fmt.Sprintf(`INSERT INTO rk_events (kind, n) VALUES ('k', %d)`, i) },
		},
		{
			name: "composite key", pk: "kind, n",
			ddl:    `CREATE TABLE rk_events (kind TEXT, n INTEGER, PRIMARY KEY (kind, n))`,
			insert: func(i int) string { return fmt.Sprintf(`INSERT INTO rk_events (kind, n) VALUES ('k%d', %d)`, i%2, i) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
				identityExec(t, db, tc.ddl)
				for i := range 5 {
					identityExec(t, db, tc.insert(i))
				}
				datexport.Register(datexport.DataExporter{
					Name: "rk_events", Source: "test", Table: "rk_events",
					PrimaryKey: tc.pk, Columns: []string{"kind", "n"},
				})
				t.Cleanup(func() { datexport.Unregister("rk_events") })
				app := NewApp(WithDB(db))
				ctx := context.Background()
				dir := t.TempDir()
				if err := app.ExportData(ctx, dir, WithExportPageSize(2)); err != nil {
					t.Fatal(err)
				}
				if rows, _ := manifestRows(t, dir, "rk_events"); rows != 5 {
					t.Fatalf("exported %d rows, want all 5", rows)
				}
				identityExec(t, db, `DELETE FROM rk_events`)
				if err := app.ImportData(ctx, dir); err != nil {
					t.Fatal(err)
				}
				if n := identityCount(t, db, `SELECT COUNT(*) FROM rk_events`); n != 5 {
					t.Errorf("restored %d rows, want 5", n)
				}
			})
		})
	}
}
