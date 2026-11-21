package pgtest

import (
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPGHarnessRequired is the canary that keeps the dual-dialect claim honest
// in CI. When a real Postgres is required (PGTEST_REQUIRED set, or running
// under GITHUB_ACTIONS) it fails the build unless the harness can stand up a
// live, queryable Postgres — not merely that resolve() returned a string.
// Outside that (local dev, no Docker) it skips, preserving the cheap local
// experience.
//
// This is the named, always-run assertion that complements the fail-closed
// choke point in BaseDSN: even if every other pgtest caller were removed, this
// test alone makes "Postgres ran in CI" an enforced fact rather than prose.
func TestPGHarnessRequired(t *testing.T) {
	if !required() {
		t.Skip("pgtest not required (set PGTEST_REQUIRED=1 or run on CI to enforce)")
	}
	// DB(t) routes through BaseDSN(t), which already fatals under required()
	// when Postgres is unreachable. The round-trip below proves the returned
	// connection is genuinely usable.
	db := DB(t)
	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("pgtest canary: harness DB unusable (SELECT 1): %v", err)
	}
	if n != 1 {
		t.Fatalf("pgtest canary: SELECT 1 returned %d, want 1", n)
	}
}

func TestUnusedDSNDropsCompetingDBNameSelector(t *testing.T) {
	const child = "GOFASTR_PGTEST_UNUSED_DSN_CHILD"
	const marker = "checked both competing database selectors"
	if os.Getenv(child) == "" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUnusedDSNDropsCompetingDBNameSelector$", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1",
			"TEST_POSTGRES_DSN=postgres://test:test@localhost:5432/pgtest?dbname=origdb&database=otherdb&sslmode=disable")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), marker) {
			t.Fatalf("selector subprocess did not pass its assertions: %v\n%s", err, out)
		}
		return
	}
	dsn, _ := UnusedDSN(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	if got := u.Query().Get("dbname"); got != "" {
		t.Errorf("dbname selector %q survived the rewrite: %s", got, dsn)
	}
	if got := u.Query().Get("database"); got != "" {
		t.Errorf("database selector %q survived the rewrite: %s", got, dsn)
	}
	if !strings.HasPrefix(u.Path, "/created_") {
		t.Errorf("path still points at the original database: %s", dsn)
	}
	t.Log(marker)
}

// TestRewriteDBNameDropsCompetingSelectors is the pure form of the
// entry-point pin above: both selector spellings go, unrelated
// connection options stay, and the path names the fresh database.
func TestRewriteDBNameDropsCompetingSelectors(t *testing.T) {
	got, err := RewriteDBName("postgres://u:p@h:5432/orig?dbname=other&database=third&sslmode=disable", "fresh_db")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse %q: %v", got, err)
	}
	if u.Path != "/fresh_db" {
		t.Errorf("path %q, want /fresh_db", u.Path)
	}
	q := u.Query()
	for _, selector := range []string{"dbname", "database"} {
		if v := q.Get(selector); v != "" {
			t.Errorf("%s selector %q survived: %s", selector, v, got)
		}
	}
	if v := q.Get("sslmode"); v != "disable" {
		t.Errorf("sslmode %q, want disable", v)
	}
}

func TestDSNWithSearchPathPreservesStartupOptions(t *testing.T) {
	for _, tc := range []struct{ name, dsn, want string }{
		{"url", "postgres://test@localhost/db?options=-c+statement_timeout%3D1379&search_path=old", "postgres://test@localhost/db?options=-c+statement_timeout%3D1379&search_path=new_schema"},
		{"keyword", "host=localhost dbname=db options='-c statement_timeout=1379' search_path='old, public'", "search_path=new_schema host=localhost dbname=db options='-c statement_timeout=1379'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DSNWithSearchPath(tc.dsn, "new_schema")
			if err != nil || got != tc.want {
				t.Fatalf("DSNWithSearchPath = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestDSNWithSearchPathRedactsMalformedDSNs(t *testing.T) {
	const secret = "fake-secret-marker"
	for _, dsn := range []string{
		"postgres://test:" + secret + "@localhost/%",
		"postgres://test:" + secret + "@localhost/db?broken=%",
		"password='" + secret,
	} {
		got, err := DSNWithSearchPath(dsn, "new_schema")
		if got != "" || err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("malformed DSN should return no string and a credential-free error, got %q, %v", got, err)
		}
	}
}
