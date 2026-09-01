package testdb

// The per-test schema binding has to survive a connection replacement.
//
// SetMaxOpenConns(1) bounds concurrency, not connection identity:
// database/sql discards a connection whose backend died and silently opens
// another. A schema bound with `SET search_path` on the open session is
// gone at that point, and every statement after it runs in "$user", public
// — the test's tables are invisible and it fails as `relation "…" does not
// exist`, blaming the schema rather than the replaced connection.
//
// The failure needs a dead backend, so the test kills its own with
// pg_terminate_backend rather than waiting for one.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// backendPID is the connection's own server process, the one to kill.
func backendPID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var pid int
	if err := db.QueryRow("SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid: %v", err)
	}
	return pid
}

func TestSearchPathSurvivesConnectionReplacement(t *testing.T) {
	db := Open(t, migrate.DialectPostgres) // skips when no PG is reachable

	var before string
	if err := db.QueryRow("SHOW search_path").Scan(&before); err != nil {
		t.Fatalf("SHOW search_path: %v", err)
	}
	if before == "" || strings.Contains(before, "public") {
		t.Fatalf("Open did not scope the connection to a per-test schema: search_path = %q", before)
	}
	if _, err := db.Exec("CREATE TABLE probe (id int)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Kill this pool's backend from a separate, unscoped connection. The
	// next use of db then runs on a connection database/sql opened itself,
	// which is the case the DSN parameter exists for.
	base, err := ResolvePostgresOnce()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	killer, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatalf("open killer: %v", err)
	}
	defer killer.Close()
	if _, err := killer.Exec("SELECT pg_terminate_backend($1)", backendPID(t, db)); err != nil {
		t.Fatalf("terminate backend: %v", err)
	}

	// The first statement after the kill may itself be the one that
	// discovers the dead connection, so allow one retry for the discovery
	// and require the SECOND to be on a live, correctly scoped connection.
	var after string
	if err := db.QueryRow("SHOW search_path").Scan(&after); err != nil {
		if err := db.QueryRow("SHOW search_path").Scan(&after); err != nil {
			t.Fatalf("SHOW search_path after replacement: %v", err)
		}
	}
	if after != before {
		t.Errorf("search_path did not survive the connection replacement: was %q, now %q — the per-test schema must travel in the DSN, not a session-level SET", before, after)
	}
	if _, err := db.Exec("INSERT INTO probe VALUES (1)"); err != nil {
		t.Errorf("insert after the connection was replaced: %v (the replacement landed outside the test's schema)", err)
	}
}

func TestWithSearchPathRewritesBothDSNForms(t *testing.T) {
	cases := []struct {
		name, dsn, want string
	}{
		{
			name: "url",
			dsn:  "postgres://test:test@localhost:5432/framework_test?sslmode=disable",
			want: "search_path=t_x_1",
		},
		{
			name: "url with an existing search_path is replaced, not doubled",
			dsn:  "postgres://test@localhost/db?search_path=stale&sslmode=disable",
			want: "search_path=t_x_1",
		},
		{
			name: "keyword/value",
			dsn:  "host=localhost user=test dbname=framework_test sslmode=disable",
			want: "search_path=t_x_1",
		},
		{
			name: "keyword/value with an existing search_path is replaced",
			dsn:  "host=localhost search_path=stale dbname=db",
			want: "search_path=t_x_1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withSearchPath(tc.dsn, "t_x_1")
			if err != nil {
				t.Fatalf("withSearchPath: %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want it to contain %q", got, tc.want)
			}
			if strings.Contains(got, "stale") {
				t.Errorf("the stale search_path survived: %q", got)
			}
			if n := strings.Count(got, "search_path="); n != 1 {
				t.Errorf("got %d search_path parameters, want exactly 1: %q", n, got)
			}
		})
	}
}

// The schema name is interpolated into a DSN, so anything outside what
// NewSchemaName emits is refused rather than passed through.
func TestWithSearchPathRefusesUnsafeSchemaNames(t *testing.T) {
	for _, bad := range []string{"", "has space", "quote'", "UPPER", "semi;colon", "amp&sand"} {
		if got, err := withSearchPath("postgres://h/db", bad); err == nil {
			t.Errorf("withSearchPath(%q) returned %q, want an error", bad, got)
		}
	}
	if _, err := withSearchPath("postgres://h/db", "t_ok_123_4"); err != nil {
		t.Errorf("a NewSchemaName-shaped name was refused: %v", err)
	}
}
