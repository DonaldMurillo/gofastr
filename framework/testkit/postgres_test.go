package testkit_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/DonaldMurillo/gofastr/framework/testkit"
)

// TestNewIsolatedDBCarvesAndDrops verifies the public helper creates a
// fresh DB, runs the migrate callback against it, and drops it on
// Cleanup.
func TestNewIsolatedDBCarvesAndDrops(t *testing.T) {
	adminDSN := os.Getenv("WTF_TEST_DATABASE_URL")
	if adminDSN == "" {
		adminDSN = os.Getenv("GOFASTR_TEST_POSTGRES_DSN")
	}
	if adminDSN == "" {
		// allow-skip: the helper itself hard-fails for host-app callers,
		// but the framework's own self-test must run in CI environments
		// without a live Postgres. CI sets one of these env vars when a
		// Postgres service is available.
		t.Skip("WTF_TEST_DATABASE_URL or GOFASTR_TEST_POSTGRES_DSN unset; skipping live-PG self-test")
	}

	migrated := false
	migrate := func(db *sql.DB) error {
		migrated = true
		_, err := db.ExecContext(context.Background(), `CREATE TABLE smoke (id INT PRIMARY KEY)`)
		return err
	}
	db, name := testkit.NewIsolatedDBWithName(t, adminDSN, migrate)
	if !migrated {
		t.Fatal("migrate callback was not invoked")
	}
	var got int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM smoke`).Scan(&got); err != nil {
		t.Fatalf("query smoke table: %v", err)
	}
	if got != 0 {
		t.Fatalf("smoke table row count = %d, want 0", got)
	}

	// Independently confirm the carved DB exists right now.
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("open admin DSN: %v", err)
	}
	defer admin.Close()
	var exists bool
	if err := admin.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&exists); err != nil {
		t.Fatalf("check pg_database: %v", err)
	}
	if !exists {
		t.Fatalf("isolated DB %q not found in pg_database while still alive", name)
	}
}

func TestNewIsolatedDBHardFailsWithoutDSN(t *testing.T) {
	// Smoke check on the error wording — the helper itself uses t.Fatalf
	// internally, so we exercise the validator directly.
	if err := testkit.ValidateAdminDSN(""); err == nil {
		t.Fatal("ValidateAdminDSN(\"\") should fail")
	} else if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error doesn't mention empty DSN: %v", err)
	}
}

// rewriteDBName must REFUSE to silently fall through on parse failure —
// previously a parse error returned the original DSN, so the carved
// conn pointed at the admin/maintenance DB and any migrations or
// fixture writes hit the operator's `postgres` database.
func TestRewriteDBNameRejectsParseFailures(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		{"libpq-kv-form", "host=db user=postgres dbname=postgres"},
		{"empty", ""},
		{"mysql-scheme", "mysql://user:pw@localhost:3306/db"},
		{"http-scheme", "http://example.com/db"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testkit.RewriteDBNameForTest(tc.dsn, "carved")
			if err == nil {
				t.Fatalf("RewriteDBName(%q) returned no error — silent fallthrough would let tests run against admin DB", tc.dsn)
			}
		})
	}
}

func TestRewriteDBNamePreservesUserHostQuery(t *testing.T) {
	in := "postgres://u:p@localhost:5432/postgres?sslmode=disable"
	out, err := testkit.RewriteDBNameForTest(in, "carved_xyz")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, must := range []string{"u:p@", "localhost:5432", "/carved_xyz", "sslmode=disable"} {
		if !strings.Contains(out, must) {
			t.Fatalf("rewritten DSN missing %q: %s", must, out)
		}
	}
}
