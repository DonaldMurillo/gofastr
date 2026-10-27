package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A restore into a fresh database: build the app on src, export, build the
// same app on a second database, import. Runs on each dialect.
func forEachRestore(t *testing.T, build func(t *testing.T, db *sql.DB) *App, seed func(t *testing.T, db *sql.DB), check func(t *testing.T, dst *sql.DB, d migrate.Dialect)) {
	t.Helper()
	for _, d := range testdb.Dialects {
		t.Run(string(d), func(t *testing.T) {
			src := testdb.Open(t, d)
			srcApp := build(t, src)
			seed(t, src)
			dir := t.TempDir()
			if err := srcApp.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
				t.Fatalf("ExportData: %v", err)
			}
			dst := testdb.Open(t, d)
			dstApp := build(t, dst)
			if err := dstApp.ImportData(context.Background(), dir); err != nil {
				t.Fatalf("ImportData into a fresh database: %v", err)
			}
			check(t, dst, d)
		})
	}
}

func newBlogApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false)))
	// "comments" sorts before "posts": name order writes the child first.
	app.Registry.Register(entity.Define("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "parent_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("post", "posts", "post_id"),
			entity.BelongsTo("parent", "comments", "parent_id"),
		},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

func TestImportWritesParentsFirst(t *testing.T) {
	forEachRestore(t, newBlogApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db,
			`INSERT INTO posts (id, title) VALUES ('p1','hello')`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c1','p1',NULL)`,
		)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		wantIDs(t, dst, "posts", "p1")
		wantIDs(t, dst, "comments", "c1")
	})
}

// A reply whose key sorts before its parent's: keyset order writes the
// reply first, so rows of a self-referencing table go parents first too.
func TestImportOrdersSelfReferences(t *testing.T) {
	forEachRestore(t, newBlogApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db,
			`INSERT INTO posts (id, title) VALUES ('p1','hello')`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c9','p1',NULL)`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c5','p1','c9')`,
			`INSERT INTO comments (id, post_id, parent_id) VALUES ('c1','p1','c5')`,
		)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		wantIDs(t, dst, "comments", "c1", "c5", "c9")
	})
}

func TestExportRestoresPivotRows(t *testing.T) {
	forEachRestore(t, newPivotApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db,
			`INSERT INTO posts (id, owner_id, title) VALUES ('p1','u1','a')`,
			`INSERT INTO tags (id, name) VALUES ('t1','go'), ('t2','sql')`,
			`INSERT INTO post_tags (post_id, tag_id) VALUES ('p1','t1'), ('p1','t2')`,
		)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		if got := countRows(t, dst, "post_tags"); got != 2 {
			t.Errorf("post_tags rows after restore = %d, want 2", got)
		}
	})
}

func TestImportKeepsLargeIntegers(t *testing.T) {
	const big = int64(9007199254740993) // 2^53 + 1: a float64 cannot hold it
	build := func(t *testing.T, db *sql.DB) *App {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("ledgers", entity.EntityConfig{
			Table: "ledgers",
			Fields: []schema.Field{
				{Name: "cents", Type: schema.Int, RawType: "BIGINT"},
				{Name: "ratio", Type: schema.Float},
			},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		return app
	}
	forEachRestore(t, build, func(t *testing.T, db *sql.DB) {
		if _, err := db.Exec(`INSERT INTO ledgers (id, cents, ratio) VALUES ($1, $2, $3)`, "g1", big, 0.25); err != nil {
			t.Fatal(err)
		}
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var cents int64
		var ratio float64
		if err := dst.QueryRow(`SELECT cents, ratio FROM ledgers WHERE id = 'g1'`).Scan(&cents, &ratio); err != nil {
			t.Fatal(err)
		}
		if cents != big {
			t.Errorf("cents after restore = %d, want %d", cents, big)
		}
		if ratio != 0.25 {
			t.Errorf("ratio after restore = %v, want 0.25", ratio)
		}
	})
}

func newCountersApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("counters", entity.EntityConfig{
		Table: "counters",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
			{Name: "label", Type: schema.String},
		},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

// After a restore, the next generated id follows the restored ones.
func TestImportAdvancesSerialSequence(t *testing.T) {
	forEachRestore(t, newCountersApp, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db, `INSERT INTO counters (label) VALUES ('a')`, `INSERT INTO counters (label) VALUES ('b')`,
			`INSERT INTO counters (label) VALUES ('c')`)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var id int64
		if err := dst.QueryRow(`INSERT INTO counters (label) VALUES ('next') RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("insert after restore: %v", err)
		}
		if id != 4 {
			t.Errorf("next id after restore = %d, want 4", id)
		}
	})
}

type recordingExec struct{ stmts []string }

func (r *recordingExec) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	s := q
	for _, a := range args {
		s += " | " + a.(string)
	}
	r.stmts = append(r.stmts, s)
	return nil, nil
}

// The Postgres statement, pinned without a server: setval on the column's
// own sequence, to the restored maximum, with an empty table (or one whose
// ids sit below the sequence's minimum) restarting at that minimum.
func TestSerialResetStatement(t *testing.T) {
	var rec recordingExec
	if err := resetSerialSequences(context.Background(), &rec, migrate.DialectPostgres, "counters", []string{"id"}); err != nil {
		t.Fatal(err)
	}
	want := `SELECT setval(s.seqrelid, GREATEST(COALESCE(m.v, s.seqmin), s.seqmin), m.v IS NOT NULL AND m.v >= s.seqmin) ` +
		`FROM pg_sequence s CROSS JOIN (SELECT MAX("id") AS v FROM "counters") m ` +
		`WHERE s.seqrelid = pg_get_serial_sequence($1, $2)::regclass | "counters" | id`
	if len(rec.stmts) != 1 || rec.stmts[0] != want {
		t.Errorf("statements = %q\nwant [%q]", rec.stmts, want)
	}
	rec.stmts = nil
	if err := resetSerialSequences(context.Background(), &rec, migrate.DialectSQLite, "counters", []string{"id"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.stmts) != 0 {
		t.Errorf("sqlite issued %q, want nothing (AUTOINCREMENT follows explicit ids)", rec.stmts)
	}
}

// tableExists reads the current schema only, as ReadLiveColumns does: a
// same-named table in another schema is not this app's table.
func TestTableExistsReadsCurrentSchema(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	other := testdb.NewSchemaName(t) + "_x"
	cascadeExec(t, db, `CREATE SCHEMA `+other, `CREATE TABLE `+other+`.ghost (id TEXT PRIMARY KEY)`)
	t.Cleanup(func() { _, _ = db.Exec(`DROP SCHEMA ` + other + ` CASCADE`) })
	ok, err := tableExists(context.Background(), db, "ghost", migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("tableExists found a table that lives in another schema")
	}
	cascadeExec(t, db, `CREATE TABLE folded (id TEXT)`)
	for _, name := range []string{"folded", "Folded"} {
		if ok, err := tableExists(context.Background(), db, name, migrate.DialectPostgres); err != nil || !ok {
			t.Errorf("tableExists(%q) = %v, %v; want true (unquoted DDL folds to lowercase)", name, ok, err)
		}
	}
}
