package migrate_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// dfltEntity carries scalar defaults on JSON columns and a float default
// below %f's six decimals.
func dfltEntity() *entity.Entity {
	return entity.Define("dflt", entity.EntityConfig{
		Table: "dflt",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "flag", Type: schema.JSON, Default: true},
			{Name: "num", Type: schema.JSON, Default: 3},
			{Name: "ratio", Type: schema.JSON, Default: 0.5},
			{Name: "tiny", Type: schema.Float, Default: 1e-7},
		},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

// A JSON column's scalar default rendered as a SQL boolean or number:
// Postgres refuses DEFAULT TRUE on JSONB, and SQLite stored the integer
// 1, which reads back as the JSON number 1, not true. A float default
// rendered with %f, so 1e-7 became 0.000000.
func TestScalarDefaultsKeepTheirValue(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{dfltEntity()}}, migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		fkRunUp(t, db, d, up)
		fkExec(t, db, `INSERT INTO dflt (id) VALUES ('r')`)
		textOf := "CAST(%s AS TEXT)"
		if d == migrate.DialectPostgres {
			textOf = "%s::text"
		}
		var flag, num, ratio string
		var tiny float64
		q := "SELECT " + fmt.Sprintf(textOf, "flag") + ", " + fmt.Sprintf(textOf, "num") + ", " + fmt.Sprintf(textOf, "ratio") + ", tiny FROM dflt"
		if err := db.QueryRowContext(context.Background(), q).Scan(&flag, &num, &ratio, &tiny); err != nil {
			t.Fatal(err)
		}
		if flag != "true" || num != "3" || ratio != "0.5" {
			t.Errorf("JSON defaults read back as flag=%q num=%q ratio=%q, want true, 3, 0.5", flag, num, ratio)
		}
		if tiny != 1e-7 {
			t.Errorf("float default read back as %v, want 1e-07", tiny)
		}
	})
}

// A snapshot written before the float fix records DEFAULT 0.000000 in
// its TableDDL, the default the table it generated really holds.
// Defaults are diffed now (2026-10-27 bug bash M15), so regenerating
// against it converges the column on the declared 1e-07; this test used
// to pin no migration, when defaults were not compared at all.
func TestOldFloatDefaultSnapshotConverges(t *testing.T) {
	plan := migrate.Plan{Registry: fkReg{dfltEntity()}}
	_, _, snap, err := migrate.GeneratePlan(plan, migrate.SchemaSnapshot{}, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	ddl := snap.TableDDL["dflt"]
	if !strings.Contains(ddl, "DEFAULT 1e-07") {
		t.Fatalf("snapshot DDL lacks the round-trip float default:\n%s", ddl)
	}
	snap.TableDDL["dflt"] = strings.Replace(ddl, "DEFAULT 1e-07", "DEFAULT 0.000000", 1)
	up, _, _, err := migrate.GeneratePlan(plan, snap, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(up, "CREATE TABLE dflt__rebuild") || !strings.Contains(up, "tiny REAL DEFAULT 1e-07") {
		t.Fatalf("an old-format float default did not converge:\n%s", up)
	}
}
