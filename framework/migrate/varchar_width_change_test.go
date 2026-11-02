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

// A String's Max became a VARCHAR width that neither the live diff nor
// the snapshot compared: the type was canonicalized to "varchar" on both
// sides, so a Max raised from 50 to 200 changed nothing on Postgres, which
// enforces the width. Validation then passed a 150-character value and
// the insert failed (2026-11-01 bug bash).

func widthEntity(max float64) *entity.Entity {
	return entity.Define("vw", entity.EntityConfig{
		Table:      "vw",
		Fields:     []schema.Field{{Name: "id", Type: schema.String, Required: true}, {Name: "s", Type: schema.String, Max: &max}},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

// takesLength reports whether vw.s stores a value of n characters, and
// keeps it.
func takesLength(t *testing.T, db *sql.DB, id string, n int) bool {
	t.Helper()
	_, err := db.Exec("INSERT INTO vw (id, s) VALUES ('" + id + "', '" + strings.Repeat("x", n) + "')")
	return err == nil
}

func TestVarcharWidthChangeGenerates(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		migs := genChain(t, db, d,
			migrate.Plan{Registry: fkReg{widthEntity(50)}},
			migrate.Plan{Registry: fkReg{widthEntity(200)}},
		)
		if !takesLength(t, db, "a", 150) {
			t.Fatal("after Max 50 → 200 the column still refuses 150 characters")
		}
		if d == migrate.DialectPostgres {
			if len(migs) != 2 {
				t.Fatalf("got %d migrations, want the width change as the second", len(migs))
			}
			fkExec(t, db, "DELETE FROM vw")
			genDown(t, db, d, migs)
			if takesLength(t, db, "b", 150) {
				t.Fatal("rolling back left the wider column")
			}
		}
	})
}

func TestVarcharWidthChangeDiffs(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{widthEntity(50)}); err != nil {
			t.Fatal(err)
		}
		if !takesLength(t, db, "keep", 40) {
			t.Fatal("seed row refused")
		}
		changes, err := migrate.DiffSchema(context.Background(), db, fkReg{widthEntity(200)})
		if err != nil {
			t.Fatal(err)
		}
		if d == migrate.DialectSQLite {
			// SQLite ignores the width: nothing to rebuild.
			if len(changes) != 0 {
				t.Fatalf("SQLite width change proposed %v", changes)
			}
			return
		}
		fkApply(t, db, changes)
		if !takesLength(t, db, "a", 150) {
			t.Fatalf("after applying %v the column still refuses 150 characters", changes)
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM vw WHERE id = 'keep' AND length(s) = 40").Scan(&n); err != nil || n != 1 {
			t.Fatalf("the existing row did not survive the widening: n=%d err=%v", n, err)
		}
		again, err := migrate.DiffSchema(context.Background(), db, fkReg{widthEntity(200)})
		if err != nil || len(again) != 0 {
			t.Fatalf("diff after the widening = %v, %v; want none", again, err)
		}
	})
}
