package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// resolveOAuthUser is the security core of the OAuth callback. The five
// cases below are the decision table from the contract:
//
//	(a) existing link              → LOGIN as the linked user
//	(b) verified email + password  → errOAuthEmailCollision (refuse)
//	(c) verified email + pwless    → AUTO-LINK + login
//	(d) UNVERIFIED email + passwd  → NO MATCH; the unique-email constraint
//	                                  then refuses the fresh-create too —
//	                                  the attacker's callback fails closed
//	                                  either way, and the victim's account
//	                                  is never linked.
//	(e) no match at all            → create + link
//
// (d) is the core takeover regression — an attacker who controls an
// unverified IdP email must NOT bind to a victim's existing account. Each
// test pins one arm of the table so a regression is unambiguous.

// newResolveManager wires an OAuth2Plugin against the given store and
// returns the plugin + manager so tests can call resolveOAuthUser directly
// or drive the callback HTTP path. The store must implement OAuthLinker;
// tests that exercise the linker-required Init path use this helper.
func newResolveManager(t *testing.T, store UserStore) (*OAuth2Plugin, *AuthManager) {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret", // prod-mode Init fails closed without one
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return plugin, mgr
}

// TestResolveOAuth_ExistingLink_LogsIn: case (a). A pre-linked user
// returns from FindByOAuth → resolveOAuthUser returns them with linked=false
// (this is a login, not a new binding). The auto-link-with-email-collision
// path must NOT fire.
func TestResolveOAuth_ExistingLink_LogsIn(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	existing, err := store.CreateUserNoPassword(ctx, "alice@example.com", []string{"user"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.LinkOAuth(ctx, existing.GetID(), "google", "g-1"); err != nil {
		t.Fatalf("LinkOAuth: %v", err)
	}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "g-1",
		Email:         "alice@example.com",
		Provider:      "google",
		EmailVerified: true,
	}
	got, linked, err := plugin.resolveOAuthUser(ctx, store, info)
	if err != nil {
		t.Fatalf("resolveOAuthUser: %v", err)
	}
	if linked {
		t.Fatalf("on an existing link, linked must be false (it is a login, not a bind)")
	}
	if got.GetID() != existing.GetID() {
		t.Fatalf("returned user %q; want linked %q", got.GetID(), existing.GetID())
	}
}

// TestResolveOAuth_VerifiedEmailAndPassword_Refuses: case (b). An existing
// PASSWORD account + a verified-email match MUST be refused with
// errOAuthEmailCollision. The user must log in with their password and
// link the provider from /auth/accounts — never a silent bind.
func TestResolveOAuth_VerifiedEmailAndPassword_Refuses(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, "victim@example.com", "realhash", []string{"user"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "attacker-id",
		Email:         "victim@example.com",
		Provider:      "google",
		EmailVerified: true,
	}
	_, _, err := plugin.resolveOAuthUser(ctx, store, info)
	if !errors.Is(err, errOAuthEmailCollision) {
		t.Fatalf("expected errOAuthEmailCollision for verified-email + password account; got %v", err)
	}
}

// TestResolveOAuth_VerifiedEmailAndPasswordless_AutoLinks: case (c). An
// existing PASSWORDLESS account (created by a prior OAuth login) + a
// verified-email match MUST auto-link and log in. This is the safe
// migration path. linked=true because a new binding was persisted.
func TestResolveOAuth_VerifiedEmailAndPasswordless_AutoLinks(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	existing, err := store.CreateUserNoPassword(ctx, "alice@example.com", []string{"user"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "g-1",
		Email:         "alice@example.com",
		Provider:      "google",
		EmailVerified: true,
	}
	got, linked, err := plugin.resolveOAuthUser(ctx, store, info)
	if err != nil {
		t.Fatalf("resolveOAuthUser: %v", err)
	}
	if !linked {
		t.Fatalf("auto-link must report linked=true (a new binding was persisted)")
	}
	if got.GetID() != existing.GetID() {
		t.Fatalf("returned user %q; want existing %q", got.GetID(), existing.GetID())
	}
	// The auto-link must actually persist — a subsequent callback for the
	// same (provider, id) returns the same user via the link table.
	got2, linked2, err := plugin.resolveOAuthUser(ctx, store, info)
	if err != nil {
		t.Fatalf("second callback: %v", err)
	}
	if linked2 {
		t.Fatalf("second callback must report linked=false (login, not bind)")
	}
	if got2.GetID() != existing.GetID() {
		t.Fatalf("second callback user drift: %q vs %q", got2.GetID(), existing.GetID())
	}
}

// TestResolveOAuth_UnverifiedEmail_DoesNotMatchPasswordAccount: case (d).
// The CORE takeover regression. An attacker who controls an UNVERIFIED IdP
// email for "victim@example.com" must NOT bind to (or log in as) the
// existing password account. The callback falls through to step 3 — but
// step 3 tries to create a NEW user with the same email, which the
// user-unique constraint correctly rejects. The attacker's callback fails
// closed either way; the load-bearing assertions are that the victim's
// account is NOT linked to the attacker's provider_id and the victim's
// session is not granted.
//
// Without this test, any regression that re-introduces email-trust on
// unverified emails silently re-opens the account-takeover hole.
func TestResolveOAuth_UnverifiedEmail_DoesNotMatchPasswordAccount(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	victim, err := store.CreateUser(ctx, "victim@example.com", "realhash", []string{"user"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "attacker-id",
		Email:         "victim@example.com",
		Provider:      "google",
		EmailVerified: false, // THE takeover signal — must not bind.
	}
	_, _, err = plugin.resolveOAuthUser(ctx, store, info)
	if err == nil {
		t.Fatal("unverified-email path against an existing password account must NOT succeed")
	}
	// The error can be ErrEmailTaken (the unique-email constraint fires
	// when step 3 tries to create with the victim's address — the correct,
	// takeover-blocking outcome) or errOAuthLookupFailed. What it MUST NOT
	// be is errOAuthEmailCollision (only VERIFIED emails may collide) or nil.
	if errors.Is(err, errOAuthEmailCollision) {
		t.Fatalf("regression: unverified email matched as a collision — only VERIFIED emails may do that")
	}
	// And the victim's account must NOT have a link to the attacker's
	// provider_id. If it did, a follow-up verified-email login would
	// silently log the attacker in as the victim via FindByOAuth.
	if linked, lerr := store.FindByOAuth(ctx, "google", "attacker-id"); lerr == nil {
		t.Fatalf("TAKEOVER REGRESSION: attacker-id was linked (to %q); FindByOAuth must return not-found",
			linked.GetID())
	} else if !errors.Is(lerr, ErrUserNotFound) {
		t.Fatalf("FindByOAuth error shape: %v", lerr)
	}
	// And the victim must not have any links at all.
	accts, _ := store.ListAccounts(ctx, victim.GetID())
	if len(accts) != 0 {
		t.Fatalf("victim account gained OAuth links from an unverified-email callback: %+v", accts)
	}
}

// TestResolveOAuth_UnverifiedEmail_NoExistingAccount_CreatesFresh: when the
// unverified email matches NO existing account, the callback creates a new
// user — that path is the legitimate "first login" case for an IdP that
// doesn't assert verification. The new account is distinct from any future
// account that might later register the same email with a password.
func TestResolveOAuth_UnverifiedEmail_NoExistingAccount_CreatesFresh(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "g-fresh",
		Email:         "fresh@example.com",
		Provider:      "google",
		EmailVerified: false,
	}
	got, linked, err := plugin.resolveOAuthUser(ctx, store, info)
	if err != nil {
		t.Fatalf("unverified email on a clean store must create a new user: %v", err)
	}
	if !linked {
		t.Fatal("fresh-create path must report linked=true")
	}
	if got.GetID() == "" {
		t.Fatal("created user must have a non-empty ID")
	}
}

// TestResolveOAuth_NoMatch_CreatesAndLinks: case (e). A clean callback for
// a never-seen email creates a new user + binds the (provider, id).
// linked=true. The same callback a second time resolves via the link and
// reports linked=false.
func TestResolveOAuth_NoMatch_CreatesAndLinks(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "g-1",
		Email:         "fresh@example.com",
		Provider:      "google",
		EmailVerified: true,
	}
	got, linked, err := plugin.resolveOAuthUser(ctx, store, info)
	if err != nil {
		t.Fatalf("resolveOAuthUser: %v", err)
	}
	if !linked {
		t.Fatalf("first callback must report linked=true (new binding)")
	}
	if got.GetID() == "" {
		t.Fatal("created user must have a non-empty ID")
	}
	if _, err := store.FindByOAuth(ctx, "google", "g-1"); err != nil {
		t.Fatalf("FindByOAuth must return the created user after a successful link: %v", err)
	}
}

// TestResolveOAuth_NoLinker_FailsClosed: a store that does NOT implement
// OAuthLinker must NEVER fall back to email-trust. resolveOAuthUser returns
// errOAuthNoLinker — production Init fails closed before this is reachable,
// but the function itself is the last line of defense. Init is bypassed
// here (this test exercises the runtime guarantee, not the boot-time one
// — TestOAuth2Plugin_Init_FailsClosedWithoutLinker covers the Init path).
func TestResolveOAuth_NoLinker_FailsClosed(t *testing.T) {
	store := &staticUserStore{
		byID: map[string]User{"u-1": &BasicUser{ID: "u-1", Email: "x@x.com", Roles: []string{"user"}}},
	}
	plugin := NewOAuth2Plugin(OAuth2Config{StateSecret: "test"})
	// No Init — we are testing the runtime contract directly.

	info := &OAuth2UserInfo{ID: "g-1", Email: "x@x.com", Provider: "google", EmailVerified: true}
	_, _, err := plugin.resolveOAuthUser(context.Background(), store, info)
	if !errors.Is(err, errOAuthNoLinker) {
		t.Fatalf("expected errOAuthNoLinker, got %v", err)
	}
}

// TestResolveOAuth_LookupError_FailsClosed: a transient error from
// FindByOAuth or FindByEmail must surface as errOAuthLookupFailed, never
// be conflated with "not found" and trigger an auto-create. This is the
// same invariant the magic-link DBError test pins — never silently
// auto-create on an error you can't classify.
func TestResolveOAuth_LookupError_FailsClosed(t *testing.T) {
	store := &flakyUserStore{err: errors.New("connection refused")}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{ID: "g-1", Email: "x@x.com", Provider: "google", EmailVerified: true}
	_, _, err := plugin.resolveOAuthUser(context.Background(), store, info)
	if !errors.Is(err, errOAuthLookupFailed) {
		t.Fatalf("expected errOAuthLookupFailed for a transient DB error; got %v", err)
	}
}

// TestResolveOAuth_HasPasswordError_FailsClosed: when EmailVerified is
// true and FindByEmail returns a user, a HasPassword error must fail
// closed — never default to "auto-link" when we can't tell password from
// passwordless.
func TestResolveOAuth_HasPasswordError_FailsClosed(t *testing.T) {
	store := &erroringPasswordCheckerStore{
		memoryUserStore: newMemoryUserStore(),
	}
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, "victim@example.com", "realhash", []string{"user"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "attacker-id",
		Email:         "victim@example.com",
		Provider:      "google",
		EmailVerified: true,
	}
	_, _, err := plugin.resolveOAuthUser(ctx, store, info)
	if !errors.Is(err, errOAuthLookupFailed) {
		t.Fatalf("HasPassword error must fail closed as errOAuthLookupFailed; got %v", err)
	}
}

// erroringPasswordCheckerStore wraps memoryUserStore with a PasswordChecker
// that always errors. Used to verify resolveOAuthUser fails closed on a
// HasPassword lookup error (rather than defaulting to "auto-link").
type erroringPasswordCheckerStore struct {
	*memoryUserStore
}

func (s *erroringPasswordCheckerStore) HasPassword(_ context.Context, _ string) (bool, error) {
	return false, errors.New("password store unreachable")
}

// TestResolveOAuth_HTTP_EmailCollisionMapsTo409: the callback's HTTP
// mapping of errOAuthEmailCollision must remain 409 (with the actionable
// "link from settings" message). A drift here breaks the user-facing
// recovery path.
func TestResolveOAuth_HTTP_EmailCollisionMapsTo409(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, "victim@example.com", "realhash", []string{"user"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	plugin, mgr := newResolveManager(t, store)
	r := router.New()
	mgr.RegisterRoutes(r)

	prov := &stubOAuthProvider{
		name: "stub",
		userInfo: &OAuth2UserInfo{
			ID:            "attacker-id",
			Email:         "victim@example.com",
			Provider:      "stub",
			EmailVerified: true,
		},
	}
	plugin.RegisterProvider("stub", prov)

	state, err := plugin.generateState("stub", "")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet,
		"/auth/oauth/stub/callback?state="+state+"&code=fakecode", nil)
	rec := httptest.NewRecorder()
	_ = ctx
	r.ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Fatalf("email collision must map to 409; got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestResolveOAuth_ConcurrentSameIdentityProducesOneLink: two concurrent
// callbacks for the SAME (provider, provider_id) on a clean store must
// produce exactly ONE link, with the goroutine whose LinkOAuth won the PK
// being the authoritative user. The brief calls this out as the
// race-safety invariant; a regression to plain INSERT would either error
// or create duplicates.
func TestResolveOAuth_ConcurrentSameIdentityProducesOneLink(t *testing.T) {
	store := newMemoryUserStore()
	ctx := context.Background()
	plugin, _ := newResolveManager(t, store)

	info := &OAuth2UserInfo{
		ID:            "g-race",
		Email:         "racer@example.com",
		Provider:      "google",
		EmailVerified: true,
	}

	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	type result struct {
		id  string
		err error
	}
	results := make([]result, N)
	start := make(chan struct{})
	for i := range N {
		go func(i int) {
			defer wg.Done()
			<-start
			u, _, err := plugin.resolveOAuthUser(ctx, store, info)
			results[i] = result{id: idOrEmpty(u), err: err}
		}(i)
	}
	close(start)
	wg.Wait()

	// Every goroutine either succeeded or hit a benign create error from
	// the email-unique constraint (in-memory store returns ErrEmailTaken
	// on a dup, which surfaces as a plain error — not as errOAuthEmailCollision).
	// The link table must end up with exactly one row.
	var firstSuccessID string
	for _, r := range results {
		if r.err == nil {
			if firstSuccessID == "" {
				firstSuccessID = r.id
			}
		}
	}
	if firstSuccessID == "" {
		t.Fatalf("at least one goroutine must succeed; all %d errored: %+v", N, results)
	}
	// FindByOAuth returns the single authoritative winner. The in-memory
	// store's LinkOAuth is "first writer wins" — exactly one (provider,
	// provider_id) binding exists.
	got, err := store.FindByOAuth(ctx, "google", "g-race")
	if err != nil {
		t.Fatalf("FindByOAuth after race: %v", err)
	}
	if got.GetID() == "" {
		t.Fatalf("post-race FindByOAuth returned empty user")
	}
	// Count distinct link rows for this (provider, provider_id). There
	// must be exactly one.
	accts, err := store.ListAccounts(ctx, got.GetID())
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	var count int
	for _, a := range accts {
		if a.Provider == "google" && a.ProviderID == "g-race" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("after %d concurrent resolves for the same identity, found %d link rows; want 1",
			N, count)
	}
}

func idOrEmpty(u User) string {
	if u == nil {
		return ""
	}
	return u.GetID()
}
