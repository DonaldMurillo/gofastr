package migrate_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func ixEntity(fields []schema.Field, idx ...entity.Index) *entity.Entity {
	return entity.Define("ixt", entity.EntityConfig{
		Table:      "ixt",
		Fields:     append([]schema.Field{{Name: "id", Type: schema.String, Required: true}}, fields...),
		PrimaryKey: []string{"id"},
		Indices:    idx,
	}.WithTimestamps(false))
}

// rejectsDuplicate reports whether inserting a second row with the same
// value in col fails.
func rejectsDuplicate(t *testing.T, db *sql.DB, col string) bool {
	t.Helper()
	fkExec(t, db, "INSERT INTO ixt (id, "+col+") VALUES ('r1', 'same')")
	_, err := db.Exec("INSERT INTO ixt (id, " + col + ") VALUES ('r2', 'same')")
	fkExec(t, db, "DELETE FROM ixt")
	return err != nil
}

// A unique index retargeted to a column added in the same change was
// emitted as CREATE (from the add) → DROP (the old definition) → CREATE,
// and the dedup kept the first CREATE: the migration ended with no
// index while the snapshot recorded one.
func TestRetargetedUniqueIndexSurvives(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		a := schema.Field{Name: "a", Type: schema.String}
		b := schema.Field{Name: "b", Type: schema.String}
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{a}, entity.Index{Name: "ux", Columns: []string{"a"}, Unique: true})}},
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{a, b}, entity.Index{Name: "ux", Columns: []string{"b"}, Unique: true})}},
		)
		if got := indexNames(t, db, d, "ixt"); !slices.Contains(got, "ux") {
			t.Fatalf("indexes after retarget = %v, want ux", got)
		}
		if !rejectsDuplicate(t, db, "b") {
			t.Fatal("ux does not hold b unique after the retarget")
		}
		genDown(t, db, d, migs)
		if !rejectsDuplicate(t, db, "a") {
			t.Fatal("rollback did not restore ux on a")
		}
	})
}

// ADD COLUMN for a Unique field dropped UNIQUE (SQLite refuses it on ADD
// COLUMN) and no index supplied it: the column a fresh CREATE TABLE
// would hold unique accepted duplicates.
func TestAddedUniqueColumnIsUnique(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity(nil)}},
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{{Name: "code", Type: schema.String, Unique: true}})}},
		)
		if !rejectsDuplicate(t, db, "code") {
			t.Fatal("an added Unique column accepted a duplicate")
		}
		genDown(t, db, d, migs)
	})
}

// Removing an indexed column together with its index dropped the column
// first, which SQLite refuses for an indexed column; the Postgres Down
// recreated the index before the column it covers.
func TestDropIndexedColumnAndIndex(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		a := schema.Field{Name: "a", Type: schema.String}
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{ixEntity([]schema.Field{a}, entity.Index{Name: "ix_a", Columns: []string{"a"}})}},
			migrate.Plan{Registry: fkReg{ixEntity(nil)}},
		)
		if got := indexNames(t, db, d, "ixt"); slices.Contains(got, "ix_a") {
			t.Fatalf("indexes after drop = %v, want no ix_a", got)
		}
		genDown(t, db, d, migs)
		if got := indexNames(t, db, d, "ixt"); !slices.Contains(got, "ix_a") {
			t.Fatalf("indexes after rollback = %v, want ix_a", got)
		}
	})
}
