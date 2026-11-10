package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func dcParent(extra ...schema.Field) *entity.Entity {
	return entity.Define("dcp", entity.EntityConfig{
		Table:      "dcp",
		Fields:     append([]schema.Field{{Name: "id", Type: schema.String, Required: true}}, extra...),
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

func dcChild(extra []schema.Field, rels []entity.Relation, idx ...entity.Index) *entity.Entity {
	return entity.Define("dcc", entity.EntityConfig{
		Table:      "dcc",
		Fields:     append([]schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "title", Type: schema.String}}, extra...),
		PrimaryKey: []string{"id"},
		Relations:  rels,
		Indices:    idx,
	}.WithTimestamps(false))
}

// dcShapes are the columns SQLite will not ALTER TABLE … DROP COLUMN, plus
// a plain one as the control. Each case is the child entity holding the
// column (steps, in order: "addedunique" gets its column from a second
// step, so the database holds a unique index rather than an inline
// UNIQUE), and the column removed afterwards.
var dcShapes = []struct {
	name  string
	col   string
	steps []*entity.Entity
}{
	{"plain", "views", []*entity.Entity{dcChild([]schema.Field{{Name: "views", Type: schema.Int}}, nil)}},
	{"unique", "slug", []*entity.Entity{dcChild([]schema.Field{{Name: "slug", Type: schema.String, Unique: true}}, nil)}},
	{"addedunique", "slug", []*entity.Entity{dcChild(nil, nil), dcChild([]schema.Field{{Name: "slug", Type: schema.String, Unique: true}}, nil)}},
	{"relation", "p_id", []*entity.Entity{dcChild([]schema.Field{{Name: "p_id", Type: schema.String}}, []entity.Relation{entity.BelongsTo("dcp", "dcp", "p_id")})}},
	{"indexed", "views", []*entity.Entity{dcChild([]schema.Field{{Name: "views", Type: schema.Int}}, nil, entity.Index{Columns: []string{"views"}})}},
	{"uniqueindex", "views", []*entity.Entity{dcChild([]schema.Field{{Name: "views", Type: schema.Int}}, nil, entity.Index{Columns: []string{"views"}, Unique: true})}},
}

// dcAssertDropped checks the column is gone and the row written before
// the drop is still there.
func dcAssertDropped(t *testing.T, db *sql.DB, col string) {
	t.Helper()
	live, err := migrate.ReadLiveColumnsSQLite(context.Background(), db, "dcc")
	if err != nil {
		t.Fatal(err)
	}
	if _, still := live[col]; still {
		t.Fatalf("column %s survived the drop: %v", col, live)
	}
	var title string
	if err := db.QueryRow("SELECT title FROM dcc WHERE id = 'c1'").Scan(&title); err != nil || title != "kept" {
		t.Fatalf("the row did not survive the drop: title=%q err=%v", title, err)
	}
}

// A removed field generates a migration that applies on SQLite whatever
// the column carried. SQLite refuses DROP COLUMN on a column that is
// UNIQUE, indexed or named by a foreign key, so those leave with a table
// rebuild. The bug: a bare DROP COLUMN was written for all of them, and
// removing a unique field or a relation produced a file `migrate up`
// failed on ("cannot drop UNIQUE column", "unknown column … in foreign
// key definition").
func TestGeneratedSQLiteColumnDropApplies(t *testing.T) {
	for _, shape := range dcShapes {
		t.Run(shape.name, func(t *testing.T) {
			db := testdb.Open(t, migrate.DialectSQLite)
			var plans []migrate.Plan
			for _, child := range shape.steps {
				plans = append(plans, migrate.Plan{Registry: fkReg{dcParent(), child}})
			}
			genChain(t, db, migrate.DialectSQLite, plans...)
			fkExec(t, db, "INSERT INTO dcp (id) VALUES ('p1')", "INSERT INTO dcc (id, title) VALUES ('c1', 'kept')")

			// Replay the chain's snapshots to reach the one the drop diffs
			// against, then generate and run the drop on the live rows.
			prev := migrate.SchemaSnapshot{}
			for _, p := range plans {
				var err error
				if _, _, prev, err = migrate.GeneratePlan(p, prev, migrate.DialectSQLite); err != nil {
					t.Fatal(err)
				}
			}
			up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{dcParent(), dcChild(nil, nil)}}, prev, migrate.DialectSQLite)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(up); err != nil {
				t.Fatalf("the generated drop does not apply: %v\n%s", err, up)
			}
			dcAssertDropped(t, db, shape.col)
			// The file names the column it drops even when no DROP COLUMN
			// statement runs: `migrate generate` warns from this line.
			if !strings.Contains(up, "DROP COLUMN dcc."+shape.col) && !strings.Contains(up, "ALTER TABLE dcc DROP COLUMN "+shape.col) {
				t.Fatalf("the Up does not name the dropped column %s:\n%s", shape.col, up)
			}
		})
	}
}

// The live diff gets the same answer: DiffSchema reads the table's
// indexes and foreign keys, and a held column leaves with the rebuild
// ApplySchemaDiffWithOptions runs. Boot never drops a column.
func TestLiveSQLiteColumnDropApplies(t *testing.T) {
	for _, shape := range dcShapes {
		t.Run(shape.name, func(t *testing.T) {
			db := testdb.Open(t, migrate.DialectSQLite)
			for _, child := range shape.steps {
				if err := migrate.AutoMigrate(db, fkReg{dcParent(), child}); err != nil {
					t.Fatal(err)
				}
			}
			fkExec(t, db, "INSERT INTO dcp (id) VALUES ('p1')", "INSERT INTO dcc (id, title) VALUES ('c1', 'kept')")
			changes, err := migrate.DiffSchema(context.Background(), db, fkReg{dcParent(), dcChild(nil, nil)})
			if err != nil {
				t.Fatal(err)
			}
			named := false
			for _, c := range changes {
				named = named || strings.Contains(c.Summary, "drop column "+shape.col)
				if !c.Destructive {
					t.Fatalf("%q is not marked destructive", c.Summary)
				}
			}
			if !named {
				t.Fatalf("no change names the dropped column %s: %+v", shape.col, changes)
			}
			fkApply(t, db, changes)
			dcAssertDropped(t, db, shape.col)
			if again, err := migrate.DiffSchema(context.Background(), db, fkReg{dcParent(), dcChild(nil, nil)}); err != nil || len(again) != 0 {
				t.Fatalf("the drop did not converge: %+v, %v", again, err)
			}
		})
	}
}

// A foreign-key column holds the drop back on its own: the automatic
// belongs_to index is not what the live diff leans on. The table here is
// made by hand with the key and no index.
func TestLiveSQLiteUnindexedForeignKeyColumnDropApplies(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	if err := migrate.AutoMigrate(db, fkReg{dcParent()}); err != nil {
		t.Fatal(err)
	}
	fkExec(t, db,
		"CREATE TABLE dcc (id TEXT PRIMARY KEY NOT NULL, title TEXT, p_id TEXT, FOREIGN KEY (p_id) REFERENCES dcp(id))",
		"INSERT INTO dcp (id) VALUES ('p1')",
		"INSERT INTO dcc (id, title, p_id) VALUES ('c1', 'kept', 'p1')")
	changes, err := migrate.DiffSchema(context.Background(), db, fkReg{dcParent(), dcChild(nil, nil)})
	if err != nil {
		t.Fatal(err)
	}
	fkApply(t, db, changes)
	dcAssertDropped(t, db, "p_id")
}

// A rebuild drops its table, which a generated file cannot do to a table
// other tables reference (see TestGeneratedRebuildOfReferencedTableRefused),
// so removing a unique field from such a table is refused with the way
// out, rather than written as a DROP COLUMN SQLite refuses.
func TestGeneratedSQLiteHeldColumnDropOnReferencedTableRefused(t *testing.T) {
	child := dcChild([]schema.Field{{Name: "p_id", Type: schema.String}}, []entity.Relation{entity.BelongsTo("dcp", "dcp", "p_id")})
	_, _, snap, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{dcParent(schema.Field{Name: "code", Type: schema.String, Unique: true}), child}}, migrate.SchemaSnapshot{}, migrate.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = migrate.GeneratePlan(migrate.Plan{Registry: fkReg{dcParent(), child}}, snap, migrate.DialectSQLite)
	if err == nil || !strings.Contains(err.Error(), "dcc") || !strings.Contains(err.Error(), "foreign_keys=OFF") {
		t.Fatalf("want a refusal naming the referencing table and the way out, got %v", err)
	}
}
