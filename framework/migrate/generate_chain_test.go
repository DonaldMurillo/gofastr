package migrate_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// genChain generates one migration per plan, each against the snapshot
// the previous one left, and applies each through the migration runner.
// It returns the applied migrations, for genDown.
func genChain(t *testing.T, db *sql.DB, d migrate.Dialect, plans ...migrate.Plan) []coremig.Migration {
	t.Helper()
	var migs []coremig.Migration
	prev := migrate.SchemaSnapshot{}
	for i, p := range plans {
		up, down, next, err := migrate.GeneratePlan(p, prev, d)
		if err != nil {
			t.Fatalf("generate step %d: %v", i+1, err)
		}
		prev = next
		if up == "" {
			continue
		}
		migs = append(migs, coremig.Migration{Version: uint64(i + 1), Name: fmt.Sprintf("m%d", i+1), Up: up, Down: down})
		m := coremig.New(db, coremig.WithDialect(d))
		for _, mg := range migs {
			if err := m.Register(mg); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.Up(context.Background()); err != nil {
			t.Fatalf("step %d Up:\n%s\n: %v", i+1, up, err)
		}
	}
	return migs
}

// genDown rolls back the last applied migration of migs.
func genDown(t *testing.T, db *sql.DB, d migrate.Dialect, migs []coremig.Migration) {
	t.Helper()
	m := coremig.New(db, coremig.WithDialect(d))
	for _, mg := range migs {
		if err := m.Register(mg); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Down(context.Background(), 1); err != nil {
		t.Fatalf("Down:\n%s\n: %v", migs[len(migs)-1].Down, err)
	}
}

// indexNames lists the index names on table, sorted.
func indexNames(t *testing.T, db *sql.DB, d migrate.Dialect, table string) []string {
	t.Helper()
	q := `SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name`
	if d == migrate.DialectPostgres {
		q = `SELECT indexname FROM pg_indexes WHERE tablename = $1 AND schemaname = current_schema() AND indexname NOT LIKE '%_pkey' ORDER BY indexname`
	}
	rows, err := db.Query(q, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
