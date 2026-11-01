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

// An index an older build named past 63 bytes, which Postgres cut to 63,
// differs from today's fitted name only by the name. migrate generate
// wrote DROP INDEX for the old one, which fails when a hand-added foreign
// key depends on that unique index. The name-only change creates the
// fitted index and leaves the old one (round 3 review of M7).
func TestFittedIndexNameDropsNothing(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		table := "w" + strings.Repeat("z", 40)
		cols := []string{"code_number_one", "code_number_two"}
		ent := entity.Define("parts", entity.EntityConfig{
			Table: table,
			Fields: []schema.Field{
				{Name: cols[0], Type: schema.String, Required: true},
				{Name: cols[1], Type: schema.String, Required: true},
			},
			Indices: []entity.Index{{Columns: cols, Unique: true}},
		}.WithTimestamps(false))
		reg := fkReg{ent}
		raw := "idx_" + table + "_" + strings.Join(cols, "_")
		fitted := entity.DefaultIndexName(table, cols)
		if fitted == raw {
			t.Fatalf("index name %q is not past 63 bytes", raw)
		}
		bootPlan(t, db, "1", ent)
		// The state an older build left: the index under the raw name
		// (Postgres keeps its first 63 bytes), and a foreign key that
		// depends on it.
		fkExec(t, db,
			"DROP INDEX "+fitted,
			"CREATE UNIQUE INDEX "+raw+" ON "+table+" ("+strings.Join(cols, ", ")+")",
			"CREATE TABLE kids (a TEXT, b TEXT, FOREIGN KEY (a, b) REFERENCES "+table+" ("+strings.Join(cols, ", ")+"))",
		)
		prev := migrate.SnapshotFromRegistry(reg, d)
		for tbl, ddls := range prev.Indices {
			for i, ddl := range ddls {
				prev.Indices[tbl][i] = strings.ReplaceAll(ddl, fitted, raw)
			}
		}
		up, _, _, err := migrate.GenerateMigration(reg, prev, d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToUpper(up), "DROP INDEX") {
			t.Errorf("a name-only change drops the old index:\n%s", up)
		}
		for _, stmt := range strings.Split(up, ";\n") {
			if strings.TrimSpace(stmt) == "" || strings.HasPrefix(strings.TrimSpace(stmt), "--") {
				continue
			}
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("apply %q: %v", stmt, err)
			}
		}
		var n int
		q := "SELECT count(*) FROM pg_indexes WHERE indexname = $1"
		if d == migrate.DialectSQLite {
			q = "SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?"
		}
		if err := db.QueryRow(q, fitted).Scan(&n); err != nil || n != 1 {
			t.Errorf("fitted index %s after the migration: count %d, err %v", fitted, n, err)
		}
	})
}

// After the name-only refit both indexes exist. The snapshot recorded only
// the fitted one, so removing the declared index later dropped that one,
// and the old unique index went on refusing duplicates (round 3 review).
func TestRetainedIndexDroppedOnRemoval(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		table := "w" + strings.Repeat("z", 40)
		cols := []string{"code_number_one", "code_number_two"}
		define := func(idx []entity.Index) *entity.Entity {
			return entity.Define("parts", entity.EntityConfig{
				Table: table,
				Fields: []schema.Field{
					{Name: cols[0], Type: schema.String, Required: true},
					{Name: cols[1], Type: schema.String, Required: true},
				},
				Indices: idx,
			}.WithTimestamps(false))
		}
		ent := define([]entity.Index{{Columns: cols, Unique: true}})
		raw := "idx_" + table + "_" + strings.Join(cols, "_")
		fitted := entity.DefaultIndexName(table, cols)
		bootPlan(t, db, "1", ent)
		fkExec(t, db,
			"DROP INDEX "+fitted,
			"CREATE UNIQUE INDEX "+raw+" ON "+table+" ("+strings.Join(cols, ", ")+")",
		)
		prev := migrate.SnapshotFromRegistry(fkReg{ent}, d)
		for tbl, ddls := range prev.Indices {
			for i, ddl := range ddls {
				prev.Indices[tbl][i] = strings.ReplaceAll(ddl, fitted, raw)
			}
		}
		apply := func(up string) {
			t.Helper()
			for _, stmt := range strings.Split(up, ";\n") {
				if s := strings.TrimSpace(stmt); s == "" || strings.HasPrefix(s, "--") {
					continue
				}
				if _, err := db.Exec(stmt); err != nil {
					t.Fatalf("apply %q: %v", stmt, err)
				}
			}
		}
		up, _, next, err := migrate.GenerateMigration(fkReg{ent}, prev, d)
		if err != nil {
			t.Fatal(err)
		}
		apply(up)
		// A generation with nothing changed keeps recording the old index.
		if up, _, next, err = migrate.GenerateMigration(fkReg{ent}, next, d); err != nil || up != "" {
			t.Fatalf("unchanged entity generated %q, %v", up, err)
		}

		up, _, _, err = migrate.GenerateMigration(fkReg{define(nil)}, next, d)
		if err != nil {
			t.Fatal(err)
		}
		apply(up)
		ins := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES ('a', 'b')"
		fkExec(t, db, ins)
		if _, err := db.Exec(ins); err != nil {
			t.Errorf("the index was removed, and a duplicate is still refused: %v\nmigration:\n%s", err, up)
		}
	})
}
