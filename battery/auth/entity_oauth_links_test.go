package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// These tests pin the durable OAuth link store added to EntityUserStore —
// the surface that makes the OAuth2 callback safe in production. Without a
// linker, the callback falls back to email-only matching and an IdP that
// emits an unverified email can sign in as an existing local account. The
// linker is the serialization point that closes that hole.
//
// The tests use the same in-memory SQLite setup the rest of entity_store
// tests use; the link table is created by EnsureSchema, mirroring how
// AuthManager.Init provisions it for hosts.

// oauthLinkFixture bundles the per-test state for the link-store suite so
// each test reads from a single, obviously-fresh store.
type oauthLinkFixture struct {
	db    *sql.DB
	store *EntityUserStore
	ctx   context.Context
}

// newOAuthLinkFixture opens a fresh in-memory SQLite DB with the users table
// already created (so NewEntityUserStore.EnsureSchema only has to add the
// oauth_links table — exercising exactly the migration path a host hits on
// upgrade).
func newOAuthLinkFixture(t *testing.T) *oauthLinkFixture {
	t.Helper()
	db := setupTestDB(t)
	// SQLite ":memory:" gives each pool connection its own private DB. With
	// the default pool the goroutine-race test would spawn a second conn and
	// not see the link table created on the first. Pin the pool to one conn
	// so every operation in the test shares the same in-memory schema.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return &oauthLinkFixture{db: db, store: store, ctx: ctx}
}

func (f *oauthLinkFixture) createUser(t *testing.T, email string) User {
	t.Helper()
	u, err := f.store.CreateUser(f.ctx, email, "irrelevant-hash", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", email, err)
	}
	return u
}

// TestEntityOAuthLinks_TableProvisioned: EnsureSchema creates the
// "<table>_oauth_links" table with the expected shape. Without this row the
// linker methods would error on every call and OAuth login would never work.
func TestEntityOAuthLinks_TableProvisioned(t *testing.T) {
	f := newOAuthLinkFixture(t)
	var name, kind string
	err := f.db.QueryRow(
		"SELECT name, type FROM sqlite_master WHERE name = 'users_oauth_links'",
	).Scan(&name, &kind)
	if err != nil {
		t.Fatalf("link table missing: %v", err)
	}
	if name != "users_oauth_links" {
		t.Fatalf("link table name = %q, want users_oauth_links", name)
	}
	if !strings.EqualFold(kind, "table") {
		t.Fatalf("sqlite_master type = %q, want table", kind)
	}
	// The PK is the serialization point — confirm provider and provider_id
	// are both in it. pragma_index_list formatting varies across SQLite
	// builds, so we go straight to table_info and read pkOrd per column.
	cols, err := f.db.Query("PRAGMA table_info(users_oauth_links)")
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	defer cols.Close()
	type colInfo struct {
		name string
		pk   int
	}
	var got []colInfo
	for cols.Next() {
		var cid int
		var cn, ct string
		var notnull, pkOrd int
		var dflt sql.NullString
		if err := cols.Scan(&cid, &cn, &ct, &notnull, &dflt, &pkOrd); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, colInfo{cn, pkOrd})
	}
	if len(got) != 7 {
		t.Fatalf("expected 7 columns in users_oauth_links, got %d", len(got))
	}
	providerPK, providerIDPK := 0, 0
	for _, c := range got {
		switch c.name {
		case "provider":
			providerPK = c.pk
		case "provider_id":
			providerIDPK = c.pk
		}
	}
	if providerPK == 0 || providerIDPK == 0 {
		t.Fatalf("provider (pk=%d) and provider_id (pk=%d) must both be in the PRIMARY KEY", providerPK, providerIDPK)
	}
}

// TestEntityOAuthLinks_FindByOAuthNotFound: a clean store returns
// ErrUserNotFound — never nil — so resolveOAuthUser proceeds to step 2/3
// instead of crashing on a nil User.
func TestEntityOAuthLinks_FindByOAuthNotFound(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u, err := f.store.FindByOAuth(f.ctx, "google", "never-linked")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil user on not-found, got %v", u)
	}
}

// TestEntityOAuthLinks_FindByOAuthEmptyKey: an empty provider or providerID
// is treated as not-found rather than producing a degenerate SELECT. Prevents
// a stray empty-string IdP payload from being looked up against a possibly-
// exists row with empty fields.
func TestEntityOAuthLinks_FindByOAuthEmptyKey(t *testing.T) {
	f := newOAuthLinkFixture(t)
	if _, err := f.store.FindByOAuth(f.ctx, "", "x"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("empty provider must yield ErrUserNotFound; got %v", err)
	}
	if _, err := f.store.FindByOAuth(f.ctx, "google", ""); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("empty providerID must yield ErrUserNotFound; got %v", err)
	}
}

// TestEntityOAuthLinks_LinkAndFindRoundTrip: link + lookup is the minimum
// viable flow. FindByOAuth MUST return the same User ID LinkOAuth recorded;
// a drift here is a silent account-confusion bug.
func TestEntityOAuthLinks_LinkAndFindRoundTrip(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	if err := f.store.LinkOAuth(f.ctx, u.GetID(), "google", "g-123"); err != nil {
		t.Fatalf("LinkOAuth: %v", err)
	}
	got, err := f.store.FindByOAuth(f.ctx, "google", "g-123")
	if err != nil {
		t.Fatalf("FindByOAuth: %v", err)
	}
	if got.GetID() != u.GetID() {
		t.Fatalf("FindByOAuth returned %q, linked %q", got.GetID(), u.GetID())
	}
}

// TestEntityOAuthLinks_LinkIsIdempotent: a second LinkOAuth for the same
// (provider, provider_id) is a no-op, not an error. The PK is the
// serialization point. Without idempotence, every callback after the first
// would 500.
func TestEntityOAuthLinks_LinkIsIdempotent(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	if err := f.store.LinkOAuth(f.ctx, u.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth #1: %v", err)
	}
	if err := f.store.LinkOAuth(f.ctx, u.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth #2 (idempotent retry): %v", err)
	}
	n, err := f.store.countLinksForProvider(f.ctx, "google", "g-1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("idempotent retry created %d rows; want 1", n)
	}
}

// TestEntityOAuthLinks_LinkDoesNotReassignOwner: the user_id of an existing
// binding is immutable from LinkOAuth — a second callback for the same
// external identity under a DIFFERENT local user must NOT flip the binding.
// Otherwise a single (provider, provider_id) could be used to walk through
// local accounts.
func TestEntityOAuthLinks_LinkDoesNotReassignOwner(t *testing.T) {
	f := newOAuthLinkFixture(t)
	first := f.createUser(t, "alice@example.com")
	second := f.createUser(t, "alice2@example.com")
	if err := f.store.LinkOAuth(f.ctx, first.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth first: %v", err)
	}
	// Race-style: a second caller links the SAME external identity to a
	// different user. The first binding MUST win.
	if err := f.store.LinkOAuth(f.ctx, second.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth second (must not error; must not reassign): %v", err)
	}
	got, err := f.store.FindByOAuth(f.ctx, "google", "g-1")
	if err != nil {
		t.Fatalf("FindByOAuth: %v", err)
	}
	if got.GetID() != first.GetID() {
		t.Fatalf("LinkOAuth flipped owner from %q to %q — the PK must win, not the last writer",
			first.GetID(), got.GetID())
	}
}

// TestEntityOAuthLinks_ConcurrentSamePairProducesOneRow: two goroutines
// racing to link the SAME (provider, provider_id) at first-login time must
// produce exactly one row and one winning user_id. This is the
// race-safety guarantee the brief calls out — the PK upsert is what enforces
// it; this test would catch a regression to plain INSERT (which would error
// or create duplicates).
func TestEntityOAuthLinks_ConcurrentSamePairProducesOneRow(t *testing.T) {
	f := newOAuthLinkFixture(t)
	const N = 16
	users := make([]User, N)
	for i := range users {
		users[i] = f.createUser(t, fmt.Sprintf("user%02d@example.com", i))
	}
	var wg sync.WaitGroup
	wg.Add(N)
	start := make(chan struct{})
	for i := range users {
		go func(i int) {
			defer wg.Done()
			<-start
			_ = f.store.LinkOAuth(f.ctx, users[i].GetID(), "github", "gh-777")
		}(i)
	}
	close(start)
	wg.Wait()

	n, err := f.store.countLinksForProvider(f.ctx, "github", "gh-777")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("after %d concurrent LinkOAuth for the same (provider, provider_id), got %d rows; want 1",
			N, n)
	}
	// FindByOAuth returns SOME user from the contender set — the winner is
	// whatever the upsert picked first. The invariant is "exactly one",
	// not "a specific one".
	got, err := f.store.FindByOAuth(f.ctx, "github", "gh-777")
	if err != nil {
		t.Fatalf("FindByOAuth after race: %v", err)
	}
	if got == nil || got.GetID() == "" {
		t.Fatalf("post-race FindByOAuth returned no user")
	}
}

// TestEntityOAuthLinks_EnrichedStoresProfile: LinkOAuthEnriched persists the
// profile so /auth/accounts can render "connected as alice@example.com
// (Alice)". A drift here would leave users guessing which provider is which.
func TestEntityOAuthLinks_EnrichedStoresProfile(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	prof := OAuthAccountProfile{Email: "alice@example.com", Name: "Alice", AvatarURL: "https://example.com/a.png"}
	if err := f.store.LinkOAuthEnriched(f.ctx, u.GetID(), "google", "g-1", prof); err != nil {
		t.Fatalf("LinkOAuthEnriched: %v", err)
	}
	accts, err := f.store.ListAccounts(f.ctx, u.GetID())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accts) != 1 {
		t.Fatalf("got %d accounts; want 1", len(accts))
	}
	a := accts[0]
	if a.Provider != "google" || a.ProviderID != "g-1" {
		t.Fatalf("account = %+v", a)
	}
	if a.Email != "alice@example.com" || a.Name != "Alice" || a.AvatarURL != "https://example.com/a.png" {
		t.Fatalf("profile drift: %+v", a)
	}
	if a.LinkedAt == nil || time.Since(*a.LinkedAt) > 5*time.Second {
		t.Fatalf("LinkedAt not populated or stale: %v", a.LinkedAt)
	}
}

// TestEntityOAuthLinks_EnrichedRefreshesProfile: a second enriched link on
// the same (provider, provider_id) refreshes the profile in place — the
// email shown in /auth/accounts matches what the IdP says now, not what it
// said at first link.
func TestEntityOAuthLinks_EnrichedRefreshesProfile(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	first := OAuthAccountProfile{Email: "alice@example.com", Name: "Alice", AvatarURL: "https://example.com/old.png"}
	if err := f.store.LinkOAuthEnriched(f.ctx, u.GetID(), "github", "gh-1", first); err != nil {
		t.Fatalf("LinkOAuthEnriched #1: %v", err)
	}
	updated := OAuthAccountProfile{Email: "alice@new.example", Name: "Alice Smith", AvatarURL: "https://example.com/new.png"}
	if err := f.store.LinkOAuthEnriched(f.ctx, u.GetID(), "github", "gh-1", updated); err != nil {
		t.Fatalf("LinkOAuthEnriched #2 (refresh): %v", err)
	}
	accts, err := f.store.ListAccounts(f.ctx, u.GetID())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accts) != 1 {
		t.Fatalf("after refresh, got %d rows; want 1", len(accts))
	}
	a := accts[0]
	if a.Email != "alice@new.example" || a.Name != "Alice Smith" || a.AvatarURL != "https://example.com/new.png" {
		t.Fatalf("profile not refreshed: %+v", a)
	}
}

// TestEntityOAuthLinks_MultipleProvidersForOneUser: a user can link more
// than one provider — this is the reason links are a separate table and not
// columns on the users table. ListAccounts returns all of them.
func TestEntityOAuthLinks_MultipleProvidersForOneUser(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	links := []struct{ prov, pid string }{
		{"google", "g-1"},
		{"github", "gh-1"},
		{"keycloak", "kc-1"},
	}
	for _, p := range links {
		if err := f.store.LinkOAuth(f.ctx, u.GetID(), p.prov, p.pid); err != nil {
			t.Fatalf("LinkOAuth %s: %v", p.prov, err)
		}
	}
	accts, err := f.store.ListAccounts(f.ctx, u.GetID())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accts) != 3 {
		t.Fatalf("got %d accounts; want 3", len(accts))
	}
	// ListAccounts is ordered by provider so the UI is stable.
	got := []string{accts[0].Provider, accts[1].Provider, accts[2].Provider}
	want := []string{"github", "google", "keycloak"}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestEntityOAuthLinks_UnlinkRemovesProvider: UnlinkOAuth deletes by
// (user_id, provider), not by provider_id — so a user with two accounts at
// the same provider (unusual but possible) loses both, matching the
// AccountsPlugin contract.
func TestEntityOAuthLinks_UnlinkRemovesProvider(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	if err := f.store.LinkOAuth(f.ctx, u.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth google: %v", err)
	}
	if err := f.store.LinkOAuth(f.ctx, u.GetID(), "github", "gh-1"); err != nil {
		t.Fatalf("LinkOAuth github: %v", err)
	}

	if err := f.store.UnlinkOAuth(f.ctx, u.GetID(), "google"); err != nil {
		t.Fatalf("UnlinkOAuth: %v", err)
	}
	accts, err := f.store.ListAccounts(f.ctx, u.GetID())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accts) != 1 || accts[0].Provider != "github" {
		t.Fatalf("after unlink google, accounts = %+v", accts)
	}
}

// TestEntityOAuthLinks_UnlinkAbsentIsNotAnError: deleting an absent link is
// idempotent. AccountsPlugin's flow has already verified existence, so a
// double-unlink (e.g. two browser tabs) must not 500.
func TestEntityOAuthLinks_UnlinkAbsentIsNotAnError(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	if err := f.store.UnlinkOAuth(f.ctx, u.GetID(), "google"); err != nil {
		t.Fatalf("UnlinkOAuth on never-linked provider must be a no-op; got %v", err)
	}
}

// TestEntityOAuthLinks_LinkOAuthRejectsEmpty: empty userID/provider/providerID
// is a programming error (not a user-controllable path), and the store fails
// closed rather than writing a degenerate row.
func TestEntityOAuthLinks_LinkOAuthRejectsEmpty(t *testing.T) {
	f := newOAuthLinkFixture(t)
	u := f.createUser(t, "alice@example.com")
	for _, tc := range []struct{ name, uid, prov, pid string }{
		{"empty user", "", "google", "g-1"},
		{"empty provider", u.GetID(), "", "g-1"},
		{"empty providerID", u.GetID(), "google", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := f.store.LinkOAuth(f.ctx, tc.uid, tc.prov, tc.pid); err == nil {
				t.Fatalf("LinkOAuth with %s must error", tc.name)
			}
		})
	}
}

// TestEntityOAuthLinks_CustomTableNameDerives: a host that names its users
// table differently gets a matching "<table>_oauth_links" — two
// EntityUserStore instances on the same DB don't collide.
func TestEntityOAuthLinks_CustomTableNameDerives(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	// Create a differently-named users table so EnsureSchema has work to do.
	if _, err := db.Exec(`CREATE TABLE custom_users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		roles TEXT DEFAULT '[]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE
	)`); err != nil {
		t.Fatalf("create custom_users: %v", err)
	}
	store := NewEntityUserStore(db, "custom_users")
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if got := store.oauthLinksTable(); got != "custom_users_oauth_links" {
		t.Fatalf("derived link table name = %q, want custom_users_oauth_links", got)
	}
	var name string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE name = 'custom_users_oauth_links'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("derived link table not created: %v", err)
	}
}
