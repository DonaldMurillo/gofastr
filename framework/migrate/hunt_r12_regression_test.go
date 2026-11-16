package migrate_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A column a view reads changes type while the view's body stays the same.
// Postgres refuses the ALTER unless the view is dropped around it (r12 migrate-01).
func TestRetypeUnderUnchangedView(t *testing.T) {
	w50, w200 := 50.0, 200.0
	plan := func(max *float64) migrate.Plan {
		return migrate.Plan{
			Registry: fkReg{ixEntity([]schema.Field{{Name: "note", Type: schema.String, Max: max}})},
			Views: []migrate.View{
				{Name: "note_view", Select: "SELECT id, note FROM ixt", DependsOn: []string{"ixt"}},
				{Name: "note_ids", Select: "SELECT id FROM note_view", DependsOn: []string{"note_view"}},
			},
		}
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d, plan(&w50), plan(&w200))
		fkExec(t, db, "SELECT id FROM note_ids")
		if d != migrate.DialectPostgres {
			return
		}
		fkExec(t, db, "INSERT INTO ixt (id, note) VALUES ('r1', '"+strings.Repeat("x", 150)+"')", "DELETE FROM ixt")
		genDown(t, db, d, migs)
		fkExec(t, db, "SELECT id FROM note_ids")
		if _, err := db.Exec("INSERT INTO ixt (id, note) VALUES ('r2', '" + strings.Repeat("x", 150) + "')"); err == nil {
			t.Fatal("the Down left the column at its new width")
		}
	})
}

// A column named primary or foreign is not the table constraint that
// starts with the same word (r12 migrate-02).
func TestConstraintLineIsNoColumn(t *testing.T) {
	parent := entity.Define("hp_parents", entity.EntityConfig{
		Table:  "hp_parents",
		Fields: []schema.Field{{Name: "id", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	link := func(required bool) *entity.Entity {
		return entity.Define("hp_links", entity.EntityConfig{
			Table: "hp_links",
			Fields: []schema.Field{
				{Name: "a", Type: schema.String, Required: true},
				{Name: "b", Type: schema.String, Required: true},
				{Name: "primary", Type: schema.Bool, Required: required, Default: false},
				{Name: "foreign", Type: schema.String, Required: required, Unique: true},
				{Name: "parent_id", Type: schema.Relation},
			},
			PrimaryKey: []string{"a", "b"},
			Relations:  []entity.Relation{{Type: entity.RelManyToOne, Name: "parent", Entity: "hp_parents", ForeignKey: []string{"parent_id"}}},
		}.WithTimestamps(false))
	}
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		plan := migrate.Plan{Registry: fkReg{parent, link(true)}}
		_, _, snap, err := migrate.GeneratePlan(plan, migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		if up, _, _, err := migrate.GeneratePlan(plan, snap, d); err != nil || up != "" {
			t.Errorf("[%s] unchanged plan generated %q, %v", d, up, err)
		}
		// The fallback for a snapshot without UniqueColumns reads the same lines.
		snap.UniqueColumns = nil
		if up, _, _, err := migrate.GeneratePlan(plan, snap, d); err != nil || up != "" {
			t.Errorf("[%s] unchanged plan, unique columns read from the DDL, generated %q, %v", d, up, err)
		}
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent, link(false)}}, snap, d)
		if err != nil || !strings.Contains(up, `"primary"`) || !strings.Contains(up, `"foreign"`) {
			t.Errorf("[%s] Required cleared on primary and foreign generated %q, %v", d, up, err)
		}
	}
}

// A trigger and the function it calls, removed together: the trigger goes
// first, and the Down recreates the function first (r12 migrate-03).
func TestRemovedRoutinesDropInReverseOrder(t *testing.T) {
	reg := fkReg{ixEntity(noteField(false, nil))}
	routines := []migrate.Routine{
		{Name: "stamp_audit", Dialect: migrate.DialectPostgres,
			Up:   "CREATE OR REPLACE FUNCTION stamp_audit() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql",
			Down: "DROP FUNCTION IF EXISTS stamp_audit()"},
		{Name: "stamp_audit_trg", Dialect: migrate.DialectPostgres,
			Up:   "CREATE OR REPLACE TRIGGER stamp_audit_trg BEFORE UPDATE ON ixt FOR EACH ROW EXECUTE FUNCTION stamp_audit()",
			Down: "DROP TRIGGER IF EXISTS stamp_audit_trg ON ixt"},
	}
	_, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: reg, Routines: routines}, migrate.SchemaSnapshot{}, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	up, down, _, err := migrate.GeneratePlan(migrate.Plan{Registry: reg}, snap, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(up, "DROP TRIGGER") > strings.Index(up, "DROP FUNCTION") {
		t.Errorf("the function is dropped before its trigger:\n%s", up)
	}
	if strings.Index(down, "CREATE OR REPLACE FUNCTION") > strings.Index(down, "CREATE OR REPLACE TRIGGER") {
		t.Errorf("the Down recreates the trigger before its function:\n%s", down)
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("a Postgres function and trigger")
		}
		migs := genChain(t, db, d, migrate.Plan{Registry: reg, Routines: routines}, migrate.Plan{Registry: reg})
		genDown(t, db, d, migs)
		fkExec(t, db, "DROP TRIGGER stamp_audit_trg ON ixt")
	})
}

// A view may be named with a keyword, as a table may (r12 migrate-04).
func TestKeywordViewName(t *testing.T) {
	view := func(sel string) migrate.Plan {
		return migrate.Plan{Registry: fkReg{ixEntity(noteField(false, nil))}, Views: []migrate.View{{Name: "user", Select: sel}}}
	}
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		for range 2 {
			if err := migrate.AutoMigratePlanContext(t.Context(), db, view("SELECT id FROM ixt")); err != nil {
				t.Fatalf("boot: %v", err)
			}
		}
		// A dropped column makes Postgres drop and recreate the view.
		if err := migrate.AutoMigratePlanContext(t.Context(), db, view("SELECT note FROM ixt")); err != nil {
			t.Fatalf("boot with a changed view: %v", err)
		}
		fkExec(t, db, `SELECT note FROM "user"`, `DROP VIEW "user"`)
		migs := genChain(t, db, d, view("SELECT id FROM ixt"), view("SELECT note FROM ixt"))
		fkExec(t, db, `SELECT note FROM "user"`)
		genDown(t, db, d, migs)
		fkExec(t, db, `SELECT id FROM "user"`)
	})
}
