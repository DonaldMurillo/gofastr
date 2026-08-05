package pgtest

import "testing"

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
