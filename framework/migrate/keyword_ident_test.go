package migrate_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The migrator wrote every name into DDL bare, while CRUD quotes them, so
// a field named order, group or limit failed CREATE TABLE on both engines,
// and one named user, end, desc, to, offset or do booted on SQLite and
// failed on Postgres. A name that is a keyword on either engine is now
// quoted (folded, as the unquoted DDL folds every other name); the rest
// stay bare (2026-11-01 bug bash, owner decision: quote keywords only).

var kwFields = []string{"order", "group", "limit", "user", "end", "desc", "to", "offset", "do"}

// kwParent is a table named user keyed on a column named group.
func kwParent() *entity.Entity {
	return entity.Define("kwuser", entity.EntityConfig{
		Table:      "user",
		Fields:     []schema.Field{{Name: "group", Type: schema.String, Required: true}, {Name: "end", Type: schema.String}},
		PrimaryKey: []string{"group"},
	}.WithTimestamps(false))
}

// kwEntity holds every keyword field. Its to column references user(group).
// Stage 2 adds a column, a Unique keyword column (fetch), an index (on end)
// and a unique index (on desc), and makes do Unique; stage 3 retypes
// offset from String to Int.
func kwEntity(stage int) *entity.Entity {
	var fields []schema.Field
	fields = append(fields, schema.Field{Name: "id", Type: schema.String, Required: true})
	for _, n := range kwFields {
		f := schema.Field{Name: n, Type: schema.String}
		if n == "limit" || (n == "do" && stage >= 2) {
			f.Unique = true
		}
		if n == "offset" && stage >= 3 {
			f.Type = schema.Int
		}
		fields = append(fields, f)
	}
	idx := []entity.Index{{Columns: []string{"order"}}, {Columns: []string{"group", "user"}, Unique: true}}
	if stage >= 2 {
		fields = append(fields, schema.Field{Name: "extra", Type: schema.String}, schema.Field{Name: "fetch", Type: schema.String, Unique: true})
		idx = append(idx, entity.Index{Columns: []string{"end"}}, entity.Index{Columns: []string{"desc"}, Unique: true})
	}
	return entity.Define("kw", entity.EntityConfig{
		Table:      "kw",
		Fields:     fields,
		PrimaryKey: []string{"id"},
		Indices:    idx,
		Relations:  []entity.Relation{entity.BelongsTo("owner", "kwuser", "to")},
	}.WithTimestamps(false))
}

func kwReg(stage int) fkReg { return fkReg{kwParent(), kwEntity(stage)} }

func kwInsert(t *testing.T, db *sql.DB, d migrate.Dialect, id, desc string) {
	t.Helper()
	cols := []string{`"id"`}
	vals := []string{"'" + id + "'"}
	for _, n := range kwFields {
		cols = append(cols, `"`+n+`"`)
		v := "'" + id + "_" + n + "'"
		switch n {
		case "to":
			v = "'g1'"
		case "offset":
			v = "'7'"
		case "desc":
			v = "'" + desc + "'"
		}
		vals = append(vals, v)
	}
	fkExec(t, db, "INSERT INTO kw ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(vals, ", ")+")")
}

func kwDiffEmpty(t *testing.T, db *sql.DB, reg fkReg) {
	t.Helper()
	changes, err := migrate.DiffSchema(context.Background(), db, reg)
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	if len(changes) != 0 {
		var s []string
		for _, c := range changes {
			s = append(s, c.Summary)
		}
		t.Fatalf("DiffSchema on a converged schema: %s", strings.Join(s, "; "))
	}
}

func TestKeywordNamesBootAndConverge(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		ctx := context.Background()
		if err := migrate.AutoMigrate(db, kwReg(1)); err != nil {
			t.Fatalf("first boot: %v", err)
		}
		fkExec(t, db, `INSERT INTO "user" ("group", "end") VALUES ('g1', 'e')`)
		kwInsert(t, db, d, "r1", "same")
		kwDiffEmpty(t, db, kwReg(1))

		// A column, an index and a unique index added at boot.
		for range 2 {
			if err := migrate.AutoMigrate(db, kwReg(2)); err != nil {
				t.Fatalf("boot adding a column and indexes: %v", err)
			}
		}
		kwDiffEmpty(t, db, kwReg(2))
		if _, err := db.Exec(`INSERT INTO kw ("id", "desc", "to") VALUES ('r2', 'same', 'g1')`); err == nil {
			t.Fatal("the unique index on desc takes duplicates")
		}
		if _, err := db.Exec(`INSERT INTO kw ("id", "limit") VALUES ('r3', 'r1_limit')`); err == nil {
			t.Fatal("the Unique limit column takes duplicates")
		}
		if _, err := db.Exec(`INSERT INTO kw ("id", "do") VALUES ('r3', 'r1_do')`); err == nil {
			t.Fatal("do made Unique at boot takes duplicates")
		}
		fkExec(t, db, `INSERT INTO kw ("id", "fetch") VALUES ('f1', 'f')`)
		if _, err := db.Exec(`INSERT INTO kw ("id", "fetch") VALUES ('f2', 'f')`); err == nil {
			t.Fatal("the added Unique fetch column takes duplicates")
		}
		fkExec(t, db, `DELETE FROM kw WHERE "id" = 'f1'`)
		if _, err := db.Exec(`INSERT INTO kw ("id", "to") VALUES ('r4', 'nope')`); err == nil {
			t.Fatal("the foreign key from to onto user(group) does not hold")
		}

		// A retype: an in-place ALTER on Postgres, a table rebuild on SQLite.
		changes, err := migrate.DiffSchema(ctx, db, kwReg(3))
		if err != nil {
			t.Fatalf("DiffSchema for the retype: %v", err)
		}
		if _, err := migrate.ApplySchemaDiffWithOptions(ctx, db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
			t.Fatalf("apply the retype: %v", err)
		}
		var off int
		var order string
		if err := db.QueryRow(`SELECT "offset", "order" FROM kw WHERE "id" = 'r1'`).Scan(&off, &order); err != nil || off != 7 || order != "r1_order" {
			t.Fatalf("row after the retype: offset=%d order=%q err=%v", off, order, err)
		}
		kwDiffEmpty(t, db, kwReg(3))
		if err := migrate.AutoMigrate(db, kwReg(3)); err != nil {
			t.Fatalf("an idle reboot after the retype: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO kw ("id", "desc", "to") VALUES ('r5', 'same', 'g1')`); err == nil {
			t.Fatal("the unique index on desc did not survive the retype")
		}
	})
}

func TestKeywordNamesGenerate(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		dir := t.TempDir()
		opts := migrate.MigrationFileOptions{MigrationsDir: dir, Dialect: d}
		for i, stage := range []int{1, 2, 3} {
			path, err := migrate.GenerateMigrationFile(migrate.Plan{Registry: kwReg(stage)}, "step", opts)
			if err != nil {
				t.Fatalf("generate stage %d: %v", stage, err)
			}
			if path == "" {
				t.Fatalf("stage %d generated nothing", stage)
			}
			m := coremig.New(db, coremig.WithDialect(d))
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".sql") {
					continue
				}
				f, err := os.Open(filepath.Join(dir, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				err = m.RegisterFromReader(f)
				f.Close()
				if err != nil {
					t.Fatalf("the runner refuses %s: %v", e.Name(), err)
				}
			}
			if err := m.Up(context.Background()); err != nil {
				body, _ := os.ReadFile(path)
				t.Fatalf("apply stage %d:\n%s\n: %v", stage, body, err)
			}
			if i == 0 {
				fkExec(t, db, `INSERT INTO "user" ("group") VALUES ('g1')`)
				kwInsert(t, db, d, "r1", "same")
			}
		}
		var off int
		if err := db.QueryRow(`SELECT "offset" FROM kw WHERE "id" = 'r1'`).Scan(&off); err != nil || off != 7 {
			t.Fatalf("row after the generated retype: %d %v", off, err)
		}
		kwDiffEmpty(t, db, kwReg(3))
		again, err := migrate.GenerateMigrationFile(migrate.Plan{Registry: kwReg(3)}, "again", opts)
		if err != nil || again != "" {
			t.Fatalf("a generation with nothing changed wrote %q (%v)", again, err)
		}
	})
}
