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

// ownerCode is ixt with an owner column and a code column, unique on its
// own (globalUnique) or only within its owner.
func ownerCode(globalUnique, indexAlone bool) *entity.Entity {
	fields := []schema.Field{
		{Name: "owner", Type: schema.String},
		{Name: "code", Type: schema.String, Unique: globalUnique},
	}
	if globalUnique {
		return ixEntity(fields)
	}
	if indexAlone {
		return ixEntity(fields, entity.Index{Columns: []string{"code"}, Unique: true})
	}
	return ixEntity(fields, entity.Index{Columns: []string{"owner", "code"}, Unique: true})
}

// insertsSecond reports whether a code 'same' for owner 'a' admits a
// second row with the same code for owner second.
func insertsSecond(t *testing.T, db *sql.DB, second string) bool {
	t.Helper()
	fkExec(t, db, "INSERT INTO ixt (id, owner, code) VALUES ('r1', 'a', 'same')")
	_, err := db.Exec("INSERT INTO ixt (id, owner, code) VALUES ('r2', '" + second + "', 'same')")
	fkExec(t, db, "DELETE FROM ixt")
	return err == nil
}

// A table built when code was unique on its own kept that UNIQUE after
// the entity moved code's uniqueness into an (owner, code) index: boot
// added the composite index beside it, so one owner's value refused
// every other owner's create, and the 409 told them it existed. Boot
// now refuses the table, and the live diff drops the stale UNIQUE.
func TestBootRefusesStaleUnique(t *testing.T) {
	for _, tc := range []struct {
		name string
		old  *entity.Entity
	}{
		{"field", ownerCode(true, false)},
		{"index", ownerCode(false, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
				ctx := context.Background()
				if err := migrate.AutoMigrate(db, fkReg{tc.old}); err != nil {
					t.Fatalf("first boot: %v", err)
				}
				scoped := fkReg{ownerCode(false, false)}
				err := migrate.AutoMigrate(db, scoped)
				if err == nil || !strings.Contains(err.Error(), "ixt.code") {
					t.Fatalf("boot over a stale UNIQUE = %v, want a refusal naming ixt.code", err)
				}
				changes, err := migrate.DiffSchema(ctx, db, scoped)
				if err != nil {
					t.Fatalf("diff: %v", err)
				}
				fkApply(t, db, changes)
				if err := migrate.AutoMigrate(db, scoped); err != nil {
					t.Fatalf("boot after the diff: %v", err)
				}
				if !insertsSecond(t, db, "b") {
					t.Fatal("two owners still cannot hold one code")
				}
				if insertsSecond(t, db, "a") {
					t.Fatal("one owner may repeat a code")
				}
			})
		})
	}
}

// A column unique on its own AND in a composite index is not stale.
func TestBootKeepsDeclaredUnique(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		both := ownerCode(true, false)
		both.Config.Indices = []entity.Index{{Columns: []string{"owner", "code"}, Unique: true}}
		for range 2 {
			if err := migrate.AutoMigrate(db, fkReg{both}); err != nil {
				t.Fatalf("boot: %v", err)
			}
		}
		changes, err := migrate.DiffSchema(context.Background(), db, fkReg{both})
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 0 {
			t.Fatalf("diff of a converged table = %v", changes)
		}
	})
}
