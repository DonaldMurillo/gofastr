package migrate_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A view that stops reading a column the same migration drops. Postgres
// refuses DROP COLUMN while the old view still reads it, and SQLite
// refuses it the same way, so a changed view is dropped before the table
// changes and created again after them (2026-10-27 bug bash M11). A
// declared view reading the changed one goes with it. The new body reads
// a column the migration adds, so the rollback must drop the new view
// before it drops that column.
func TestChangedViewDropsBeforeColumn(t *testing.T) {
	plan := func(view string, fields ...schema.Field) migrate.Plan {
		ent := entity.Define("qty", entity.EntityConfig{
			Table:      "qty",
			Fields:     append([]schema.Field{{Name: "id", Type: schema.String, Required: true}}, fields...),
			PrimaryKey: []string{"id"},
		}.WithTimestamps(false))
		return migrate.Plan{Registry: fkReg{ent}, Views: []migrate.View{
			{Name: "qv", Select: view, DependsOn: []string{"qty"}},
			{Name: "qvv", Select: "SELECT id FROM qv", DependsOn: []string{"qv"}},
		}}
	}
	n := schema.Field{Name: "n", Type: schema.Int}
	m := schema.Field{Name: "m", Type: schema.Int}
	k := schema.Field{Name: "k", Type: schema.Int}
	for _, d := range []migrate.Dialect{migrate.DialectPostgres, migrate.DialectSQLite} {
		t.Run(string(d), func(t *testing.T) {
			db := testdb.Open(t, d)
			migs := genChain(t, db, d,
				plan("SELECT id, n, m FROM qty", n, m),
				plan("SELECT id, n, k FROM qty", n, k),
			)
			readsView(t, db, "qv")
			readsView(t, db, "qvv")
			genDown(t, db, d, migs)
			readsView(t, db, "qv")
			readsView(t, db, "qvv")
		})
	}
}
