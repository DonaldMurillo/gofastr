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
	"fmt"
	"net/url"
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

// A libpq value may be single-quoted and contain spaces, so a whitespace
// split mangles it: `search_path='stale, public'` splits into
// `search_path='stale,` and `public'`, and dropping the first leaves the
// second behind as a stray token. A quoted value that merely CONTAINS
// "search_path=" loses its tail to the same filter. lib/pq then reports a
// malformed DSN rather than the schema, so the symptom points anywhere but
// here.
func TestWithSearchPathPreservesQuotedValues(t *testing.T) {
	cases := []struct {
		name, dsn, keep string
	}{
		{
			name: "quoted search_path with a space is removed whole",
			dsn:  "host=localhost search_path='stale, public' dbname=db",
			keep: "dbname=db",
		},
		{
			name: "an unrelated quoted value with a space survives",
			dsn:  "host=localhost password='a b c' dbname=db",
			keep: "password='a b c'",
		},
		{
			// libpq's `options` really can carry `-c search_path=…`, so this
			// is the realistic shape, not a contrived one: a filter that
			// matches on the substring would eat half of it.
			name: "a quoted value containing search_path= is not filtered",
			dsn:  "host=localhost options='-c search_path=y' dbname=db",
			keep: "options='-c search_path=y'",
		},
		{
			name: "an escaped quote inside a value does not end it",
			dsn:  `host=localhost password='a\'b c' dbname=db`,
			keep: `password='a\'b c'`,
		},
		{
			name: "whitespace around the equals sign",
			dsn:  "host = localhost dbname=db",
			keep: "host = localhost",
		},
		{
			// lib/pq's unquoted-value scanner consumes a backslash-escaped
			// character, so `password=a\ b` is ONE value to the driver.
			// Splitting at the space rejected a DSN lib/pq accepts.
			name: "an escaped space in an unquoted value is one token",
			dsn:  `host=localhost password=a\ b dbname=db`,
			keep: `password=a\ b`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := withSearchPath(tc.dsn, "t_new_1")
			if err != nil {
				t.Fatalf("withSearchPath: %v", err)
			}
			if !strings.Contains(got, tc.keep) {
				t.Errorf("got %q, want it to keep %q intact", got, tc.keep)
			}
			if strings.Contains(got, "stale") {
				t.Errorf("the stale search_path survived: %q", got)
			}
			if n := strings.Count(got, "search_path=t_new_1"); n != 1 {
				t.Errorf("got %d copies of the new search_path, want 1: %q", n, got)
			}
			// A leftover fragment is the actual bug: the old value's tail
			// surviving as a token of its own.
			for _, frag := range []string{" public'", " y'"} {
				if strings.Contains(got, frag) {
					t.Errorf("a quoted value was split, leaving %q behind: %q", frag, got)
				}
			}
		})
	}
}

// Everything above asserts the SHAPE of the rewritten string. That is not
// the property that matters: the property is that lib/pq still accepts it
// and lands in the right schema. A rewrite can look right and not connect —
// which is exactly what the whitespace split did.
//
// Builds a keyword/value DSN carrying a quoted value with a space (the case
// that broke), rewrites it, and opens a real connection with the result.
func TestRewrittenKeywordValueDSNStillConnects(t *testing.T) {
	base, err := ResolvePostgresOnce()
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Skipf("TEST_POSTGRES_DSN is not a URL (%v); this test builds from one", err)
	}
	pass, _ := u.User.Password()
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	// The password is interpolated into a keyword/value DSN, so it has to
	// be single-quoted or it cannot stay one pair: u.User.Password() is ""
	// for a trust/peer-auth DSN, and `password= dbname=…` makes `dbname=…`
	// the password (no dbname pair survives) — a password containing a
	// space breaks identically. Either way the failure lands on
	// "lib/pq rejected the rewritten DSN" and blames the rewrite instead
	// of the fixture. Quotes and backslashes are escaped per libpq.
	pass = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(pass) + "'"
	// The fixture has to contain a QUOTED search_path with a space in it.
	// Splitting on whitespace and re-joining on whitespace is lossless on
	// its own — the damage happens only when a token is dropped, and here
	// `search_path='stale,` is dropped while `public'` is left behind as a
	// stray token that lib/pq cannot parse. A fixture without a quoted
	// search_path round-trips unharmed through the broken code, so this test
	// would pass against the bug it exists to catch. (It did, first draft.)
	//
	// options carries the second half of the property: a quoted value with a
	// space that must survive untouched.
	kv := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable "+
			"options='-c statement_timeout=30s' search_path='stale, public'",
		host, port, u.User.Username(), pass, strings.TrimPrefix(u.Path, "/"))

	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	const sn = "t_kv_rewrite_probe"
	admin.Exec("DROP SCHEMA IF EXISTS " + sn + " CASCADE")
	if _, err := admin.Exec("CREATE SCHEMA " + sn); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer admin.Exec("DROP SCHEMA " + sn + " CASCADE")

	rewritten, err := withSearchPath(kv, sn)
	if err != nil {
		t.Fatalf("withSearchPath: %v", err)
	}
	db, err := sql.Open("postgres", rewritten)
	if err != nil {
		t.Fatalf("open rewritten: %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow("SHOW search_path").Scan(&got); err != nil {
		t.Fatalf("lib/pq rejected the rewritten DSN: %v", err)
	}
	if got != sn {
		t.Errorf("connected, but search_path = %q, want %q", got, sn)
	}
	// The quoted value has to have survived intact, not just been carried.
	var timeout string
	if err := db.QueryRow("SHOW statement_timeout").Scan(&timeout); err != nil {
		t.Fatalf("SHOW statement_timeout: %v", err)
	}
	if timeout != "30s" {
		t.Errorf("the quoted options value did not survive the rewrite: statement_timeout = %q, want 30s", timeout)
	}
}

// A DSN the tokeniser cannot parse is an error, not a guess: silently
// reshaping a connection string is how the whitespace-split bug read.
func TestWithSearchPathRefusesMalformedDSNs(t *testing.T) {
	for _, bad := range []string{
		"postgres://%",              // url.Parse fails
		"host=localhost dbname",     // key with no value
		"host=localhost password='", // unterminated quote
		"=novalue dbname=db",        // empty key
	} {
		if got, err := withSearchPath(bad, "t_x_1"); err == nil {
			t.Errorf("withSearchPath(%q) returned %q, want an error", bad, got)
		}
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
