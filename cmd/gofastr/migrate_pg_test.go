package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Real-Postgres end-to-end of the `gofastr migrate` CLI. The rest of the CLI
// migrate tests run against SQLite or exercise the postgres branch only as a
// negative (unreachable server); this drives the production path — up, status,
// down — against a live Postgres database.
func TestRunMigratePostgresRoundTrip(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t) // skips if no Postgres
	dbFlag := "--db-url=" + dsn
	drv := "--driver=postgres"

	covT_migrationsDir(t) // writes migrations/001_create_posts.sql + chdirs

	// up
	out := covT_capStdout(t, func() { runMigrate([]string{"up", dbFlag, drv}) })
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("migrate up reported an error: %s", out)
	}

	// The table really exists on Postgres now.
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var reg sql.NullString
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass('public.posts')").Scan(&reg); err != nil {
		t.Fatal(err)
	}
	if !reg.Valid {
		t.Fatal("CLI migrate up did not create the posts table on Postgres")
	}

	// status reflects the applied migration (clean applied rows show in the count)
	st := covT_capStdout(t, func() { runMigrate([]string{"status", dbFlag, drv}) })
	if !strings.Contains(st, "Applied: 1") || !strings.Contains(st, "Pending: 0") {
		t.Fatalf("status after up should be Applied: 1 / Pending: 0, got:\n%s", st)
	}

	// down rolls it back
	covT_capStdout(t, func() { runMigrate([]string{"down", "1", dbFlag, drv}) })
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass('public.posts')").Scan(&reg); err != nil {
		t.Fatal(err)
	}
	if reg.Valid {
		t.Fatal("CLI migrate down did not drop the posts table on Postgres")
	}

	// status now shows the migration back in pending (by name)
	st2 := covT_capStdout(t, func() { runMigrate([]string{"status", dbFlag, drv}) })
	if !strings.Contains(st2, "Applied: 0") || !strings.Contains(st2, "create_posts") {
		t.Fatalf("status after down should be Applied: 0 with create_posts pending, got:\n%s", st2)
	}
}
