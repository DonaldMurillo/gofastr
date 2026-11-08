package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A Required field with no default added to an existing table is applied
// nullable ("NOT NULL deferred": NOT NULL would violate every existing
// row). The snapshot used to record the declared NOT NULL anyway, so the
// tightening was never generated, and a later SQLite rebuild copied NULL
// rows into a NOT NULL column without the change ever being named (#656).

func deferredMustPlan(extra ...schema.Field) migrate.Plan {
	return migrate.Plan{Registry: fkReg{ixEntity(append([]schema.Field{{Name: "must", Type: schema.String, Required: true}}, extra...))}}
}

// genStep generates one migration from prev, applies it through the
// migration runner, and returns its SQL plus the snapshot it leaves.
func genStep(t *testing.T, db *sql.DB, d migrate.Dialect, version uint64, plan migrate.Plan, prev migrate.SchemaSnapshot) (string, string, migrate.SchemaSnapshot, error) {
	t.Helper()
	up, down, next, err := migrate.GeneratePlan(plan, prev, d)
	if err != nil {
		t.Fatalf("generate v%d: %v", version, err)
	}
	if up == "" {
		return "", down, next, nil
	}
	m := coremig.New(db, coremig.WithDialect(d))
	if err := m.Register(coremig.Migration{Version: version, Name: "gen", Up: up, Down: down}); err != nil {
		t.Fatal(err)
	}
	applyErr := m.Up(context.Background())
	if applyErr != nil {
		return up, down, next, applyErr
	}
	return up, down, next, nil
}

// ddlLine returns the CREATE TABLE line that defines col.
func ddlLine(t *testing.T, ddl, col string) string {
	t.Helper()
	for _, line := range strings.Split(ddl, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, col+" ") || strings.HasPrefix(trimmed, `"`+col+`"`) {
			return line
		}
	}
	t.Fatalf("column %s not found in DDL:\n%s", col, ddl)
	return ""
}

// deferredMustState runs the two steps that leave a deferred-nullable
// `must` column behind, and generates (without applying) the tightening.
func deferredMustState(t *testing.T, db *sql.DB, d migrate.Dialect) (tighten string, snapshotAfterAdd migrate.SchemaSnapshot) {
	t.Helper()
	_, _, s1, err := genStep(t, db, d, 1, migrate.Plan{Registry: fkReg{ixEntity(nil)}}, migrate.SchemaSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	up2, _, s2, err := genStep(t, db, d, 2, deferredMustPlan(), s1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(up2, "ADD COLUMN") {
		t.Fatalf("step 2 did not add the column:\n%s", up2)
	}
	if line := ddlLine(t, s2.TableDDL["ixt"], "must"); strings.Contains(strings.ToUpper(line), "NOT NULL") {
		t.Fatalf("snapshot records the deferred column as NOT NULL:\n%s", line)
	}
	tighten, _, _, err = migrate.GeneratePlan(deferredMustPlan(), s2, d)
	if err != nil {
		t.Fatal(err)
	}
	return tighten, s2
}

func TestGenerateDeferredNotNullIsTightenedLater(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		tighten, s2 := deferredMustState(t, db, d)
		if tighten == "" {
			t.Fatal("the deferred NOT NULL tightening was never generated")
		}
		if d == migrate.DialectPostgres && !strings.Contains(tighten, "SET NOT NULL") {
			t.Fatalf("Postgres tightening is not SET NOT NULL:\n%s", tighten)
		}
		// A NULL row blocks the tightening until it is backfilled.
		fkExec(t, db, "INSERT INTO ixt (id) VALUES ('n1')")
		if _, _, _, err := genStep(t, db, d, 3, deferredMustPlan(), s2); err == nil {
			t.Fatalf("the tightening applied over a NULL row:\n%s", tighten)
		}
		fkExec(t, db, "UPDATE ixt SET must = 'x' WHERE must IS NULL")
		up3, _, s3, err := genStep(t, db, d, 4, deferredMustPlan(), s2)
		if err != nil {
			t.Fatalf("apply the tightening after backfill:\n%s\n: %v", tighten, err)
		}
		if up3 == "" {
			t.Fatal("the tightening after backfill was not generated")
		}
		if line := ddlLine(t, s3.TableDDL["ixt"], "must"); !strings.Contains(strings.ToUpper(line), "NOT NULL") {
			t.Fatalf("snapshot after the tightening still records nullable:\n%s", line)
		}
		if _, err := db.Exec("INSERT INTO ixt (id, must) VALUES ('n2', NULL)"); err == nil {
			t.Fatal("the column still accepts NULL after the tightening")
		}
		fkExec(t, db, "DELETE FROM ixt")
	})
}

// A SQLite table rebuild triggered by an unrelated change carries the
// pending tightening as a named part of the same change set: a NULL row
// blocks the whole rebuild instead of the NOT NULL arriving unannounced
// mid-copy, and after a backfill the rebuild applies and tightens.
func TestGenerateDeferredNotNullSurvivesUnrelatedRebuild(t *testing.T) {
	// The rebuild over a NULL row fails: the tightening is in the change
	// set, refusing until the rows are backfilled.
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectSQLite {
			t.Skip("SQLite rebuilds the table for a type change")
		}
		up, down, _ := unrelatedRebuildChange(t, db, d)
		fkExec(t, db, "INSERT INTO ixt (id) VALUES ('n1')")
		m := coremig.New(db, coremig.WithDialect(d))
		if err := m.Register(coremig.Migration{Version: 3, Name: "m3", Up: up, Down: down}); err != nil {
			t.Fatal(err)
		}
		if err := m.Up(context.Background()); err == nil {
			t.Fatalf("the rebuild tightened NOT NULL over a NULL row:\n%s", up)
		}
	})
	// After a backfill the same change set applies and tightens.
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectSQLite {
			t.Skip("SQLite rebuilds the table for a type change")
		}
		up, down, next := unrelatedRebuildChange(t, db, d)
		fkExec(t, db, "INSERT INTO ixt (id) VALUES ('n1')")
		fkExec(t, db, "UPDATE ixt SET must = 'x' WHERE must IS NULL")
		m := coremig.New(db, coremig.WithDialect(d))
		if err := m.Register(coremig.Migration{Version: 3, Name: "m3", Up: up, Down: down}); err != nil {
			t.Fatal(err)
		}
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("apply the rebuild after backfill:\n%s\n: %v", up, err)
		}
		if line := ddlLine(t, next.TableDDL["ixt"], "must"); !strings.Contains(strings.ToUpper(line), "NOT NULL") {
			t.Fatalf("snapshot after the tightening still records nullable:\n%s", line)
		}
		if _, err := db.Exec("INSERT INTO ixt (id, must) VALUES ('n2', NULL)"); err == nil {
			t.Fatal("the column still accepts NULL after the tightening")
		}
		fkExec(t, db, "DELETE FROM ixt")
	})
}

// unrelatedRebuildChange leaves the deferred-nullable `must` column
// behind and returns the change set an unrelated type change generates
// from there, plus the snapshot it leaves.
func unrelatedRebuildChange(t *testing.T, db *sql.DB, d migrate.Dialect) (string, string, migrate.SchemaSnapshot) {
	t.Helper()
	_, s2 := deferredMustState(t, db, d)
	retyped := deferredMustPlan(schema.Field{Name: "extra", Type: schema.Int})
	up, down, next, err := migrate.GeneratePlan(retyped, s2, d)
	if err != nil {
		t.Fatal(err)
	}
	if up == "" {
		t.Fatal("the unrelated type change generated nothing")
	}
	if !strings.Contains(up, "ixt__rebuild") {
		t.Fatalf("the type change did not rebuild the table:\n%s", up)
	}
	return up, down, next
}
