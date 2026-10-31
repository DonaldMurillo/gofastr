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

func codeField(unique bool) []schema.Field {
	return []schema.Field{{Name: "code", Type: schema.String, Unique: unique}}
}

// Unique set on a column that already existed made no change: the only
// field-level UNIQUE outside CREATE TABLE was the ADD COLUMN path, so
// the column kept accepting duplicates, and clearing Unique later left
// the constraint behind.
func TestGenerateConvergesUniqueFlip(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity(codeField(false))}},
			migrate.Plan{Registry: fkReg{ixEntity(codeField(true))}},
		)
		if !rejectsDuplicate(t, db, "code") {
			t.Fatal("Unique set on an existing column accepted a duplicate")
		}
		genDown(t, db, d, migs)
		if rejectsDuplicate(t, db, "code") {
			t.Fatal("rolling back the Unique flip left the column unique")
		}
	})
}

// The index a flip added is removed by a later clear: on Postgres an
// index, not a constraint, and on SQLite a named index the rebuild's
// DROP takes.
func TestGenerateClearsAddedUnique(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity(codeField(false))}},
			migrate.Plan{Registry: fkReg{ixEntity(codeField(true))}},
			migrate.Plan{Registry: fkReg{ixEntity(codeField(false))}},
		)
		if rejectsDuplicate(t, db, "code") {
			t.Fatal("Unique cleared after a flip still rejects a duplicate")
		}
	})
}

func renamedCode(unique bool) *entity.Entity {
	ent := ixEntity(codeField(unique))
	ent.Config.Renames = map[string]string{"old": "code"}
	return ent
}

// A renamed column is known to the table and the snapshot by its old
// name: Unique set in the rename indexes the column, and Unique cleared
// drops the constraint the old name gave it.
func TestGenerateUniqueAcrossRename(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{{Name: "old", Type: schema.String}})}},
			migrate.Plan{Registry: fkReg{renamedCode(true)}},
		)
		if !rejectsDuplicate(t, db, "code") {
			t.Fatal("Unique set in a rename accepted a duplicate")
		}
	})
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{{Name: "old", Type: schema.String, Unique: true}})}},
			migrate.Plan{Registry: fkReg{renamedCode(false)}},
		)
		if rejectsDuplicate(t, db, "code") {
			t.Fatal("Unique cleared in a rename still rejects a duplicate")
		}
	})
}

// A column created unique (inline UNIQUE in CREATE TABLE) keeps its
// constraint when Unique is cleared unless the migration removes it:
// on Postgres a constraint, on SQLite an autoindex only a rebuild drops.
func TestGenerateDropsInlineUnique(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity(codeField(true))}},
			migrate.Plan{Registry: fkReg{ixEntity(codeField(false))}},
		)
		if rejectsDuplicate(t, db, "code") {
			t.Fatal("Unique cleared on a column created unique still rejects a duplicate")
		}
		if d == migrate.DialectPostgres {
			genDown(t, db, d, migs)
			if !rejectsDuplicate(t, db, "code") {
				t.Fatal("rollback did not restore the unique constraint")
			}
		}
	})
}

// A snapshot written before unique columns were recorded reads them from
// the CREATE TABLE it recorded: an unchanged entity makes no change (a
// Down dropping the table's own constraint would remove it), and a
// cleared Unique is still seen, though generate rewrites the snapshot
// only with a migration. One with neither compares nothing. The note
// column's default holds the word UNIQUE, which does not make it unique,
// and code is the last definition, closed by the statement's paren.
func TestLegacySnapshotReadsUniqueFromDDL(t *testing.T) {
	withNote := func(unique bool) migrate.Plan {
		note := schema.Field{Name: "note", Type: schema.String, Default: "a UNIQUE b"}
		return migrate.Plan{Registry: fkReg{ixEntity(append([]schema.Field{note}, codeField(unique)...))}}
	}
	uniquePlan, plainPlan := withNote(true), withNote(false)
	for _, d := range []migrate.Dialect{migrate.DialectSQLite, migrate.DialectPostgres} {
		_, _, snap, err := migrate.GeneratePlan(uniquePlan, migrate.SchemaSnapshot{}, d)
		if err != nil {
			t.Fatal(err)
		}
		if snap.UniqueColumns == nil {
			t.Fatalf("%s: snapshot records no unique columns", d)
		}
		snap.UniqueColumns = nil
		gen := func(p migrate.Plan, prev migrate.SchemaSnapshot) string {
			up, _, _, err := migrate.GeneratePlan(p, prev, d)
			if err != nil {
				t.Fatal(err)
			}
			return up
		}
		if up := gen(uniquePlan, snap); up != "" {
			t.Fatalf("%s: an unchanged entity over a legacy snapshot produced:\n%s", d, up)
		}
		if up := gen(plainPlan, snap); !strings.Contains(up, "ixt_code_key") && !strings.Contains(up, "__rebuild") {
			t.Fatalf("%s: a Unique cleared over a legacy snapshot produced:\n%s", d, up)
		}
		snap.TableDDL = nil
		if up := gen(plainPlan, snap); up != "" {
			t.Fatalf("%s: a snapshot with no unique record produced:\n%s", d, up)
		}
	}
}

// Boot converges a Unique flip on an existing column too: it is additive
// and a unique index only narrows what the table accepts.
func TestBootEnforcesUniqueOnExisting(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(false))}); err != nil {
			t.Fatal(err)
		}
		if err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(true))}); err != nil {
			t.Fatal(err)
		}
		if !rejectsDuplicate(t, db, "code") {
			t.Fatal("boot left a Unique column accepting duplicates")
		}
	})
}

// The column CREATE TABLE made unique already has its index (the
// Postgres constraint, the SQLite autoindex); a second boot must not
// add another one beside it.
func TestBootAddsNoSecondUniqueIndex(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		reg := fkReg{ixEntity(codeField(true))}
		if err := migrate.AutoMigrate(db, reg); err != nil {
			t.Fatal(err)
		}
		before := strings.Join(indexNames(t, db, d, "ixt"), ",")
		if err := migrate.AutoMigrate(db, reg); err != nil {
			t.Fatal(err)
		}
		if after := strings.Join(indexNames(t, db, d, "ixt"), ","); after != before {
			t.Fatalf("indexes after a second boot = %q, want %q", after, before)
		}
	})
}

// A unique index over code and another column holds the pair unique, not
// code: boot still gives code its own index.
func TestBootIgnoresCompositeUnique(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		pair := entity.Index{Name: "ux_pair", Columns: []string{"code", "other"}, Unique: true}
		fields := func(unique bool) []schema.Field {
			return append(codeField(unique), schema.Field{Name: "other", Type: schema.String})
		}
		if err := migrate.AutoMigrate(db, fkReg{ixEntity(fields(false), pair)}); err != nil {
			t.Fatal(err)
		}
		if err := migrate.AutoMigrate(db, fkReg{ixEntity(fields(true), pair)}); err != nil {
			t.Fatal(err)
		}
		if !rejectsDuplicate(t, db, "code") {
			t.Fatal("a composite unique index stood in for the column's own")
		}
	})
}

// Boot applies no rename, so a Unique field renamed through Renames has
// no column under its new name yet; boot must not index a column that
// is not there.
func TestBootSkipsUniqueRenameTarget(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{ixEntity([]schema.Field{{Name: "old", Type: schema.String}})}); err != nil {
			t.Fatal(err)
		}
		renamed := ixEntity(codeField(true))
		renamed.Config.Renames = map[string]string{"old": "code"}
		if err := migrate.AutoMigrate(db, fkReg{renamed}); err != nil {
			t.Fatalf("boot with a renamed Unique field: %v", err)
		}
	})
}

// Rows that already repeat a value cannot take the index; the boot error
// names the field, so the operator knows which column to clean up.
func TestBootUniqueFlipNamesTheField(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(false))}); err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, "INSERT INTO ixt (id, code) VALUES ('r1', 'same'), ('r2', 'same')")
		err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(true))})
		if err == nil || !strings.Contains(err.Error(), "ixt.code") {
			t.Fatalf("boot over duplicate values = %v, want an error naming ixt.code", err)
		}
	})
}
