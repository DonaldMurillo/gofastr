package access

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// teamsBoot boots a replica over db whose registry knows teams:read and
// teams:write.
func teamsBoot(t *testing.T, db *sql.DB) (*GrantStore, *RolePolicy) {
	t.Helper()
	ctx := context.Background()
	p := NewRolePolicy()
	p.Register("teams:read", "teams:write")
	s := NewGrantStore(db, p)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadInto(ctx, p); err != nil {
		t.Fatal(err)
	}
	return s, p
}

// Revoke("teams:*") tombstones the literal and its expansion; a re-grant of
// "teams:*" cleared only the expansion, so the surviving literal expanded
// again on every reload and peers and restarts denied what the origin
// allowed (2026-10-27 D22).
func TestRegrantWildcardLiftsItsTombstone(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	sa, pa := teamsBoot(t, db)
	sb, pb := teamsBoot(t, db)
	if err := sa.Grant(ctx, "lead", "teams:*"); err != nil {
		t.Fatal(err)
	}
	if err := sa.Revoke(ctx, "lead", "teams:*"); err != nil {
		t.Fatal(err)
	}
	if err := sa.Grant(ctx, "lead", "teams:*"); err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	if err := sb.reloadRole(ctx, "lead"); err != nil {
		t.Fatal(err)
	}
	if err := sa.reloadAll(ctx); err != nil {
		t.Fatal(err)
	}
	_, pc := teamsBoot(t, db)
	for name, p := range map[string]*RolePolicy{"origin": pa, "peer": pb, "fresh boot": pc} {
		for _, perm := range []Permission{"teams:read", "teams:write"} {
			if !permIn(p.PermissionsOf("lead"), perm) {
				t.Errorf("%s lacks re-granted %s: %v", name, perm, p.PermissionsOf("lead"))
			}
		}
	}
}

// A grant narrower than a surviving wildcard revocation used to succeed on
// the origin while every reload subtracted it again. It is refused, and
// nothing is written; granting the wildcard and revoking the rest is the
// way to keep part of it.
func TestNarrowGrantUnderWildcardRevoke(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	sa, pa := teamsBoot(t, db)
	sb, pb := teamsBoot(t, db)
	if err := sa.Grant(ctx, "lead", "teams:*"); err != nil {
		t.Fatal(err)
	}
	if err := sa.Revoke(ctx, "lead", "teams:*"); err != nil {
		t.Fatal(err)
	}
	if err := sa.CheckGrant(ctx, "lead", "teams:read"); !errors.Is(err, ErrRevokedByWildcard) {
		t.Fatalf("CheckGrant under teams:* revoke = %v, want ErrRevokedByWildcard", err)
	}
	if err := sa.CheckGrant(ctx, "lead", "teams:*"); err != nil {
		t.Fatalf("CheckGrant of the revoked wildcard itself = %v", err)
	}
	err := sa.Grant(ctx, "lead", "teams:read")
	if !errors.Is(err, ErrRevokedByWildcard) {
		t.Fatalf("narrow grant under teams:* revoke = %v, want ErrRevokedByWildcard", err)
	}
	if permIn(pa.PermissionsOf("lead"), "teams:read") {
		t.Fatal("the refused grant reached the live policy")
	}
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM access_grants WHERE role = 'lead'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused grant wrote %d grant rows", rows)
	}
	var stones int
	if err := db.QueryRow(`SELECT count(*) FROM access_grants_revoked WHERE role = 'lead' AND permission = 'teams:read'`).Scan(&stones); err != nil {
		t.Fatal(err)
	}
	if stones != 1 {
		t.Fatal("the refused grant lifted the teams:read tombstone")
	}

	if err := sa.Grant(ctx, "lead", "teams:*"); err != nil {
		t.Fatal(err)
	}
	if err := sa.Revoke(ctx, "lead", "teams:write"); err != nil {
		t.Fatal(err)
	}
	if err := sb.reloadRole(ctx, "lead"); err != nil {
		t.Fatal(err)
	}
	_, pc := teamsBoot(t, db)
	for name, p := range map[string]*RolePolicy{"origin": pa, "peer": pb, "fresh boot": pc} {
		got := p.PermissionsOf("lead")
		if !permIn(got, "teams:read") || permIn(got, "teams:write") {
			t.Errorf("%s holds %v, want teams:read alone", name, got)
		}
	}
}

// A wildcard tombstone recorded while the registry did not know the
// resource holds only the literal, and today's reload still expands it, so
// the narrow grant it covers is refused too. The global Wildcard tombstone
// is never expanded by a reload, so it refuses nothing.
func TestNarrowGrantUnderLiteralRevoke(t *testing.T) {
	ctx := context.Background()
	db := openAccessDB(t)
	p0 := NewRolePolicy()
	p0.Register("posts:read")
	s0 := NewGrantStore(db, p0)
	if err := s0.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s0.LoadInto(ctx, p0); err != nil {
		t.Fatal(err)
	}
	if err := s0.Revoke(ctx, "lead", "teams:*", Wildcard); err != nil {
		t.Fatal(err)
	}
	sa, pa := teamsBoot(t, db)
	if err := sa.Grant(ctx, "lead", "teams:write"); !errors.Is(err, ErrRevokedByWildcard) {
		t.Fatalf("grant under a literal teams:* revoke = %v, want ErrRevokedByWildcard", err)
	}
	if err := sa.Grant(ctx, "lead", "posts:read"); err != nil {
		t.Fatalf("grant under the global wildcard revoke: %v", err)
	}
	_, pc := teamsBoot(t, db)
	for name, p := range map[string]*RolePolicy{"origin": pa, "fresh boot": pc} {
		if got := p.PermissionsOf("lead"); !permIn(got, "posts:read") || permIn(got, "teams:write") {
			t.Errorf("%s holds %v, want posts:read alone", name, got)
		}
	}
}
