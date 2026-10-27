package query_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A store now names the folded table. When Postgres still holds the
// case-kept one an older build quoted, boot stops and prints the rename;
// running that rename clears the refusal. A lowercase name or a fresh
// database is never refused.
func TestRefuseCaseKeptTables(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	// A qualified name's schema is not the current one, so the advice has
	// to move the renamed table there.
	schema := fmt.Sprintf("cko_%d", os.Getpid())
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	if err := query.RefuseCaseKeptTables(ctx, db, "Jobs", "jobs_lower"); err != nil {
		t.Fatalf("fresh database refused: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE "Jobs" (id TEXT)`,
		`CREATE TABLE jobs_lower (id TEXT)`,
		fmt.Sprintf(`CREATE TABLE %q (id TEXT)`, schema+".Events"),
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}
	if err := query.RefuseCaseKeptTables(ctx, db, "jobs_lower"); err != nil {
		t.Errorf("lowercase table refused: %v", err)
	}
	for _, table := range []string{"Jobs", schema + ".Events"} {
		err := query.RefuseCaseKeptTables(ctx, db, "jobs_lower", table)
		if err == nil {
			t.Fatalf("%s: older case-kept table was not refused", table)
		}
		_, advice, ok := strings.Cut(err.Error(), "restart: ")
		if !ok {
			t.Fatalf("%s: no rename advice in %q", table, err)
		}
		if _, err := db.Exec(advice); err != nil {
			t.Fatalf("%s: run advice %q: %v", table, advice, err)
		}
		if err := query.RefuseCaseKeptTables(ctx, db, table); err != nil {
			t.Errorf("%s: still refused after the advised rename: %v", table, err)
		}
	}
	var jobs, events bool
	if err := db.QueryRow(`SELECT to_regclass('jobs') IS NOT NULL, to_regclass($1) IS NOT NULL`,
		schema+".events").Scan(&jobs, &events); err != nil {
		t.Fatalf("look up renamed tables: %v", err)
	}
	if !jobs || !events {
		t.Errorf("after the advice: jobs=%v %s.events=%v, want both", jobs, schema, events)
	}
	// Both spellings present: the rows have to move by hand.
	for _, ddl := range []string{`CREATE TABLE "Jobs" (id TEXT)`} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("%s: %v", ddl, err)
		}
	}
	err := query.RefuseCaseKeptTables(ctx, db, "Jobs")
	if err == nil || !strings.Contains(err.Error(), "move the rows") {
		t.Errorf("both spellings present: err = %v, want advice to move the rows", err)
	}
}
