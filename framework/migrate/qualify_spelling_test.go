package migrate_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// defaultSchemaOf is the schema an unqualified table lands in on d.
func defaultSchemaOf(d migrate.Dialect) string {
	if d == migrate.DialectPostgres {
		return "public"
	}
	return "main"
}

func spelledEntity(table string) *entity.Entity { return spelledWith(table, nil) }

// spelledWith is spelledEntity with edit applied to its config first.
func spelledWith(table string, edit func(*entity.EntityConfig)) *entity.Entity {
	cfg := entity.EntityConfig{
		Table: table,
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "code", Type: schema.String, Unique: true},
		},
		PrimaryKey: []string{"id"},
		Indices:    []entity.Index{{Columns: []string{"code", "id"}}},
	}
	if edit != nil {
		edit(&cfg)
	}
	return entity.Define("ixt", cfg.WithTimestamps(false))
}

// Writing a table's default schema into Table names the same table, but
// the snapshot keyed it by the old spelling: generate created the
// qualified table (a no-op) and dropped the unqualified one, rows and
// all. Either direction of the respelling is no change at all.
func TestQualifyingTableIsNoChange(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		qualified := defaultSchemaOf(d) + ".ixt"
		for _, steps := range [][2]string{{"ixt", qualified}, {qualified, "ixt"}} {
			up, _, next, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{spelledEntity(steps[1])}}, snapAfter(t, d, steps[0]), d)
			if err != nil {
				t.Fatal(err)
			}
			if up != "" {
				t.Fatalf("%s: %s → %s generated:\n%s", d, steps[0], steps[1], up)
			}
			if _, ok := next.Tables[steps[1]]; !ok {
				t.Fatalf("%s: %s → %s: next snapshot tables = %v", d, steps[0], steps[1], next.Tables)
			}
		}
	}
}

// The respelled table's columns, indexes and unique columns are still
// compared: a change made in the same step reaches the migration, and
// nothing else does.
func TestQualifyingTableKeepsDiffing(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		qualified := defaultSchemaOf(d) + ".ixt"
		ent := spelledWith(qualified, func(c *entity.EntityConfig) {
			c.Fields = append(c.Fields, schema.Field{Name: "note", Type: schema.String})
		})
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{ent}}, snapAfter(t, d, "ixt"), d)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(up, "ADD COLUMN note") {
			t.Fatalf("%s: added column missing:\n%s", d, up)
		}
		if strings.Contains(up, "INDEX") || strings.Contains(up, "DROP") {
			t.Fatalf("%s: respelling touched indexes or dropped something:\n%s", d, up)
		}

		// The recorded key and unique columns move with the table: a key
		// change, or Unique cleared, still reaches the migration, from a
		// snapshot that records unique columns and from a legacy one that
		// has only the CREATE TABLE text.
		rekeyed := spelledWith(qualified, func(c *entity.EntityConfig) { c.PrimaryKey = []string{"id", "code"} })
		cleared := spelledWith(qualified, func(c *entity.EntityConfig) { c.Fields[1].Unique = false })
		legacy := snapAfter(t, d, "ixt")
		legacy.UniqueColumns = nil
		noDDL := snapAfter(t, d, "ixt")
		noDDL.TableDDL = nil
		for _, step := range []struct {
			name string
			ent  *entity.Entity
			prev migrate.SchemaSnapshot
		}{
			{"key change", rekeyed, snapAfter(t, d, "ixt")},
			{"unique cleared", cleared, snapAfter(t, d, "ixt")},
			{"unique cleared, legacy snapshot", cleared, legacy},
			{"unique cleared, no CREATE TABLE text", cleared, noDDL},
		} {
			up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{step.ent}}, step.prev, d)
			if err != nil {
				t.Fatal(err)
			}
			if up == "" {
				t.Fatalf("%s: %s with the respelling generated nothing", d, step.name)
			}
		}
	}
}

// snapAfter is the snapshot a first generation of table leaves.
func snapAfter(t *testing.T, d migrate.Dialect, table string) migrate.SchemaSnapshot {
	t.Helper()
	_, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{spelledEntity(table)}}, migrate.SchemaSnapshot{}, d)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// A table moved to another schema is a different table: the respelling
// rule covers the default schema only, so generate still creates the new
// one and drops the old.
func TestOtherSchemaIsAnotherTable(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{spelledEntity("archive.ixt")}}, snapAfter(t, d, "ixt"), d)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(up, "DROP TABLE IF EXISTS ixt") || !strings.Contains(up, "CREATE TABLE IF NOT EXISTS archive.ixt") {
			t.Fatalf("%s: moving to another schema generated:\n%s", d, up)
		}
	}
}

// A many-to-many Through respelled the same way names the same pivot,
// which stays: no create, no drop.
func TestQualifyingThroughIsNoChange(t *testing.T) {
	plan := func(through string) migrate.Plan {
		tags := entity.Define("tags", entity.EntityConfig{
			Table:  "tags",
			Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		}.WithTimestamps(false))
		posts := entity.Define("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
			Relations: []entity.Relation{{
				Name: "tags", Type: entity.RelManyToMany, Entity: "tags",
				Through: through, LocalKey: "post_id", ForeignKeyTarget: "tag_id",
			}},
		}.WithTimestamps(false))
		return migrate.Plan{Registry: fkReg{posts, tags}}
	}
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		_, _, prev, err := migrate.GeneratePlan(plan("posts_tags"), migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := prev.Tables["posts_tags"]; !ok {
			t.Fatalf("%s: the pivot is not recorded: %v", d, prev.Tables)
		}
		up, _, _, err := migrate.GeneratePlan(plan(defaultSchemaOf(d)+".posts_tags"), prev, d)
		if err != nil {
			t.Fatal(err)
		}
		if up != "" {
			t.Fatalf("%s: respelling the Through generated:\n%s", d, up)
		}
	}
}

// A recorded spelling another declaration still names stays that
// declaration's: an unmanaged entity reading the table by its qualified
// name must not take the managed entity's records, which would read the
// managed table as new.
func TestNamedSpellingKeepsItsRecords(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		reader := entity.Define("ixtreader", entity.EntityConfig{
			Table:      defaultSchemaOf(d) + ".ixt",
			Unmanaged:  true,
			Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}},
			PrimaryKey: []string{"id"},
		}.WithTimestamps(false))
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{spelledEntity("ixt"), reader}}, snapAfter(t, d, "ixt"), d)
		if err != nil {
			t.Fatal(err)
		}
		if up != "" {
			t.Fatalf("%s: an unmanaged reader of the table generated:\n%s", d, up)
		}
	}
}

// A record kept under the declared spelling is the one compared, even
// when another spelling of the table is recorded beside it: the other is
// never moved onto it.
func TestExactSpellingRecordWins(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		qualified := defaultSchemaOf(d) + ".ixt"
		for _, pair := range [][2]string{{qualified, "ixt"}, {"ixt", qualified}} {
			exact, other := pair[0], pair[1]
			prev := snapAfter(t, d, exact)
			prev.Tables[other] = snapAfter(t, d, other).Tables[other]
			prev.Tables[other]["old"] = "TEXT"
			// Map order picks which record a collision keeps, so one pass
			// could miss it.
			for range 20 {
				up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{spelledEntity(exact)}}, prev, d)
				if err != nil {
					t.Fatal(err)
				}
				if up != "" {
					t.Fatalf("%s: declaring %s beside a recorded %s generated:\n%s", d, exact, other, up)
				}
			}
		}
	}
}
