package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// covT_chdirBlueprint writes a gofastr.yml with the given fields YAML into a
// temp dir and chdirs to it.
func covT_chdirBlueprint(t *testing.T, fields string) {
	t.Helper()
	dir := t.TempDir()
	bp := "app:\n  name: testapp\nentities:\n  - name: posts\n    table: posts\n    fields:\n" + fields
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	covT_chdir(t, dir)
}

// CLI `migrate up --create-db` against Postgres: creates the database that does
// not yet exist, then applies migrations into it.
func TestCLI_PG_CreateDBThenUp(t *testing.T) {
	target, drop := pgtest.UnusedDSN(t) // db does not exist yet
	defer drop()
	covT_migrationsDir(t) // migrations/001_create_posts.sql + chdir

	out := covT_capStdout(t, func() {
		runMigrate([]string{"up", "--create-db", "--db-url=" + target, "--driver=postgres"})
	})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("up --create-db reported an error: %s", out)
	}
	db, err := sql.Open("postgres", target)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var reg sql.NullString
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass('public.posts')").Scan(&reg); err != nil {
		t.Fatalf("the database should have been created and migrated: %v", err)
	}
	if !reg.Valid {
		t.Fatal("--create-db created the DB but migrations did not apply")
	}
}

// CLI `migrate force` against Postgres marks a version's applied state without
// running its SQL.
func TestCLI_PG_Force(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	dbFlag := "--db-url=" + dsn
	drv := "--driver=postgres"
	covT_migrationsDir(t)

	covT_capStdout(t, func() { runMigrate([]string{"up", dbFlag, drv}) })
	// Force the applied migration back to not-applied (operator reconciliation).
	covT_capStdout(t, func() { runMigrate([]string{"force", "1", "--not-applied", dbFlag, drv}) })
	st := covT_capStdout(t, func() { runMigrate([]string{"status", dbFlag, drv}) })
	if !strings.Contains(st, "Applied: 0") || !strings.Contains(st, "create_posts") {
		t.Fatalf("after force --not-applied, status should be Applied: 0 with create_posts pending, got:\n%s", st)
	}
}

// CLI `migrate diff --apply` against a live Postgres applies the declarative
// delta and a re-diff is idempotent (up to date).
// CLI `migrate status` renders a dirty migration (⚠ DIRTY) on Postgres.
func TestCLI_PG_StatusShowsDirty(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	covT_migrationsDir(t) // status needs a migrations/ dir present (+ chdir)
	// Induce a dirty state with the runner: a failing NoTransaction migration.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	m := coremig.New(db, coremig.WithDialect(coremig.DialectPostgres))
	m.Register(coremig.Migration{Version: 1, Name: "wedged", Up: "SELECT definitely_not_a_function()", Down: "SELECT 1", NoTransaction: true})
	_ = m.Up(context.Background()) // expected to fail and mark dirty
	db.Close()

	st := covT_capStdout(t, func() {
		runMigrate([]string{"status", "--db-url=" + dsn, "--driver=postgres"})
	})
	if !strings.Contains(st, "DIRTY") {
		t.Fatalf("status should flag the dirty migration, got:\n%s", st)
	}
}
