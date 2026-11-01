package migrate_test

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Two unnamed composite unique indexes whose default names share their
// first 63 bytes were one name to Postgres, which truncates identifiers
// there: CREATE UNIQUE INDEX IF NOT EXISTS skipped the second with a
// NOTICE, and the table took duplicate rows (2026-10-27 M7).
func TestLongIndexNamesStayDistinct(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		bins := entity.Define("bins", entity.EntityConfig{
			Table: "inventory_bins",
			Fields: []schema.Field{
				{Name: "warehouse_identifier_code", Type: schema.String},
				{Name: "bin_location_identifier_a", Type: schema.String},
				{Name: "bin_location_identifier_b", Type: schema.String},
			},
			Indices: []entity.Index{
				{Columns: []string{"warehouse_identifier_code", "bin_location_identifier_a"}, Unique: true},
				{Columns: []string{"warehouse_identifier_code", "bin_location_identifier_b"}, Unique: true},
			},
		}.WithTimestamps(false))
		bootPlan(t, db, "1", bins)
		fkExec(t, db, `INSERT INTO inventory_bins (id, warehouse_identifier_code, bin_location_identifier_a, bin_location_identifier_b)
			VALUES ('1', 'w', 'a1', 'b')`)
		if _, err := db.Exec(`INSERT INTO inventory_bins (id, warehouse_identifier_code, bin_location_identifier_a, bin_location_identifier_b)
			VALUES ('2', 'w', 'a2', 'b')`); err == nil {
			t.Fatal("a repeated (warehouse, bin b) went in: the second unique index is missing")
		}
		bootPlan(t, db, "2", bins)
	})
}
