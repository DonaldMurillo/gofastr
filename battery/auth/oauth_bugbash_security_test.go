package auth

// SECURITY TESTS — the OAuth/OIDC findings of the 2026-11-01 battery/auth
// bug bash. Each test below was written against the pre-fix code, watched
// fail, and then watched fail again with its fix reverted.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ── 1. OIDC email_verified describes only the standard email claim ─────────

// email_verified (OIDC Core §5.1) says the `email` claim was verified. A
// host mapping EmailClaim to preferred_username read that flag as a
// verdict on preferred_username: an attacker set preferred_username to the
// victim's address, verified their own `email`, and the callback
// auto-linked the victim's account.
func TestOIDCRemappedEmailClaimDoesNotInheritVerified(t *testing.T) {
	cases := []struct {
		name      string
		preferred string
		want      bool
	}{
		{"mapped claim names another address", "victim@example.com", false},
		{"mapped claim is the verified address", "attacker@example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.claims = baseClaims(f)
			f.claims["email"] = "attacker@example.com"
			f.claims["email_verified"] = true
			f.claims["preferred_username"] = tc.preferred
			p, err := NewOIDCProvider(OIDCConfig{
				Issuer:       f.issuer,
				ClientID:     f.clientID,
				ClientSecret: f.clientSecret,
				RedirectURL:  "https://app.example.com/cb",
				HTTPClient:   newOIDCTestHTTPClient(t),
				Claims:       OIDCClaimsMapping{EmailClaim: "preferred_username"},
			})
			if err != nil {
				t.Fatal(err)
			}
			tok, err := p.ExchangeCode(ctxBg(), "any-code")
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
			if err != nil {
				t.Fatalf("FetchUserInfo: %v", err)
			}
			if info.Email != tc.preferred {
				t.Fatalf("Email = %q, want the mapped claim %q", info.Email, tc.preferred)
			}
			if info.EmailVerified != tc.want {
				t.Errorf("SECURITY: EmailVerified = %v for mapped address %q (email claim attacker@example.com verified), want %v",
					info.EmailVerified, tc.preferred, tc.want)
			}
		})
	}
}

// The same rule on the userinfo-only path (no cached id_token claims).
func TestOIDCUserinfoRemappedEmailClaimDoesNotInheritVerified(t *testing.T) {
	f := newFakeIdP(t)
	f.userinfo = map[string]any{
		"email":              "attacker@example.com",
		"email_verified":     true,
		"preferred_username": "victim@example.com",
	}
	f.userinfoSub = "user-123"
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:       f.issuer,
		ClientID:     f.clientID,
		ClientSecret: f.clientSecret,
		RedirectURL:  "https://app.example.com/cb",
		HTTPClient:   newOIDCTestHTTPClient(t),
		Claims:       OIDCClaimsMapping{EmailClaim: "preferred_username"},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := p.FetchUserInfo(ctxBg(), "uncached-access-token")
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.Email != "victim@example.com" || info.EmailVerified {
		t.Errorf("SECURITY: userinfo path gave Email=%q EmailVerified=%v, want victim@example.com unverified",
			info.Email, info.EmailVerified)
	}
}

// ── 2. An email-less identity never matches or creates by "" ──────────────

func TestOAuthEmptyEmailNeverMergesAccounts(t *testing.T) {
	c := newClaimRig(t)
	c.corp.userResp = &OAuth2UserInfo{ID: "sub-a", Email: "", EmailVerified: true}
	uidA, codeA := c.oauth("corp")
	c.corp.userResp = &OAuth2UserInfo{ID: "sub-b", Email: "", EmailVerified: true}
	uidB, codeB := c.oauth("corp")
	if uidA != "" && uidA == uidB {
		t.Fatalf("SECURITY: two email-less identities signed into the same account %s", uidA)
	}
	if codeA != http.StatusBadRequest || codeB != http.StatusBadRequest {
		t.Errorf("email-less sign-up answered %d / %d, want 400 for both", codeA, codeB)
	}
	if _, _, err := c.store.FindByEmail(context.Background(), ""); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("an account with an empty email was created (err=%v)", err)
	}
}

// An identity that is already linked still signs in with no email.
func TestOAuthEmptyEmailLinkedIdentitySignsIn(t *testing.T) {
	c := newClaimRig(t)
	ctx := context.Background()
	u, err := c.store.CreateUserNoPassword(ctx, "linked@example.com", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.LinkOAuth(ctx, u.GetID(), "corp", "sub-linked"); err != nil {
		t.Fatal(err)
	}
	c.corp.userResp = &OAuth2UserInfo{ID: "sub-linked", Email: ""}
	got, code := c.oauth("corp")
	if got != u.GetID() {
		t.Fatalf("linked email-less identity: signed into %q (%d), want %s", got, code, u.GetID())
	}
}

func TestOAuthOverlongEmailRefused(t *testing.T) {
	c := newClaimRig(t)
	c.corp.userResp = &OAuth2UserInfo{ID: "sub-long", Email: strings.Repeat("a", maxEmailLen) + "@example.com"}
	if uid, code := c.oauth("corp"); uid != "" || code != http.StatusBadRequest {
		t.Fatalf("overlong provider email: uid=%q code=%d, want refused 400", uid, code)
	}
}

// ── 3. An account claim drops the squatter's stored provider tokens ────────

func withClaimTokenStore(t *testing.T, c *claimRig, store OAuthTokenStore) {
	t.Helper()
	op, err := PluginAs[*OAuth2Plugin](c.mgr, "oauth2")
	if err != nil {
		t.Fatal(err)
	}
	op.tokenStore = store
}

func TestAccountClaimDropsProviderTokens(t *testing.T) {
	c := newClaimRig(t)
	tokens := newSQLOAuthTokenStore(t)
	withClaimTokenStore(t, c, tokens)
	ctx := context.Background()

	uid, code := c.oauth("corp") // squatter, unverified victim@example.com
	if uid == "" {
		t.Fatalf("setup: squatter sign-in = %d", code)
	}
	if _, err := tokens.Get(ctx, uid, "corp"); err != nil {
		t.Fatalf("setup: squatter's tokens were not stored: %v", err)
	}
	// A row for a registered provider that is no longer linked, left by
	// an earlier partial failure: a retry must reach it too.
	if err := tokens.Save(ctx, OAuthTokenRecord{UserID: uid, Provider: "google", AccessToken: "x", RefreshToken: "y"}); err != nil {
		t.Fatal(err)
	}

	if got := c.magicLink("victim@example.com"); got != uid {
		t.Fatalf("setup: magic link signed into %s, want %s", got, uid)
	}
	for _, prov := range []string{"corp", "google"} {
		if _, err := tokens.Get(ctx, uid, prov); !errors.Is(err, ErrOAuthTokenNotFound) {
			t.Errorf("SECURITY: the squatter's stored %s tokens survived the account claim (err=%v)", prov, err)
		}
	}
}

type failingDeleteTokens struct{ OAuthTokenStore }

func (failingDeleteTokens) Delete(context.Context, string, string) error {
	return errors.New("token store down")
}

// A token row that cannot be dropped fails the claim, so the account stays
// unverified and the next proof retries.
func TestAccountClaimFailsWhenTokensSurvive(t *testing.T) {
	c := newClaimRig(t)
	tokens := newSQLOAuthTokenStore(t)
	withClaimTokenStore(t, c, tokens)
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: squatter sign-in = %d", code)
	}
	withClaimTokenStore(t, c, failingDeleteTokens{tokens})
	if w := c.json("/auth/magic-link/send", map[string]string{"email": "victim@example.com"}, ""); w.Code != http.StatusOK {
		t.Fatalf("send: %d", w.Code)
	}
	form := "token=" + c.links.last(t)
	w := c.do(http.MethodPost, "/auth/magic-link/verify", form, "application/x-www-form-urlencoded", "")
	if w.Code == http.StatusFound {
		t.Error("SECURITY: the claim succeeded although the squatter's provider tokens could not be dropped")
	}
	if c.verified(uid) {
		t.Error("account marked verified after a failed claim")
	}
}

// A sign-in refused as link_removed stores no provider tokens.
func TestOAuthLoginRefusedAsLinkRemovedStoresNoTokens(t *testing.T) {
	const email = "victim@example.com"
	var hooked *hookedLinks
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &hookedLinks{EntityUserStore: s}
		return hooked
	})
	tokens := newSQLOAuthTokenStore(t)
	withClaimTokenStore(t, c, tokens)
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: squatter sign-in = %d", code)
	}
	// Lookup 2 is the post-mint re-check: the claim lands between the
	// sign-in's resolution and that re-check.
	hooked.arm(1, func() {
		if got := c.magicLink(email); got != uid {
			t.Errorf("setup: magic link signed into %s, want %s", got, uid)
		}
	})
	if got, _ := c.oauth("corp"); got != "" {
		t.Fatalf("setup: the overtaken sign-in kept a session")
	}
	hooked.mustHaveFired(t)
	if _, err := tokens.Get(context.Background(), uid, "corp"); !errors.Is(err, ErrOAuthTokenNotFound) {
		t.Errorf("SECURITY: a sign-in refused as link_removed stored the squatter's provider tokens (err=%v)", err)
	}
}

// The claim lands after the post-mint re-check passed (lookup 2) and before
// the tokens were stored: the save used to come last, so it wrote the
// squatter's grant back after the claim had dropped it (2026-11-01 review).
// The save now precedes the re-check, so the claim's drop always follows it.
func TestOAuthClaimAfterLinkRecheckLeavesNoTokens(t *testing.T) {
	const email = "victim@example.com"
	var hooked *hookedLinks
	c := newClaimRig(t, func(s *EntityUserStore) UserStore {
		hooked = &hookedLinks{EntityUserStore: s}
		return hooked
	})
	tokens := newSQLOAuthTokenStore(t)
	withClaimTokenStore(t, c, tokens)
	uid, code := c.oauth("corp")
	if uid == "" {
		t.Fatalf("setup: squatter sign-in = %d", code)
	}
	hooked.arm(2, func() {
		if got := c.magicLink(email); got != uid {
			t.Errorf("setup: magic link signed into %s, want %s", got, uid)
		}
	})
	c.oauthStatus("corp")
	hooked.mustHaveFired(t)
	if _, err := tokens.Get(context.Background(), uid, "corp"); !errors.Is(err, ErrOAuthTokenNotFound) {
		t.Errorf("SECURITY: the claimed account still holds the squatter's provider tokens (err=%v)", err)
	}
}

// oauthStatus runs one sign-in and returns only the callback's status: the
// claim purges the session it set, so oauth's session read would fail.
func (c *claimRig) oauthStatus(provider string) int {
	c.t.Helper()
	w1 := httptest.NewRecorder()
	c.r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/auth/oauth/"+provider, nil))
	loc := w1.Header().Get("Location")
	state := loc[strings.LastIndex(loc, "state=")+len("state="):]
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/"+provider+"/callback?state="+url.QueryEscape(state)+"&code=c", nil)
	for _, ck := range w1.Result().Cookies() {
		if ck.Name == oauthStateCookie {
			req.AddCookie(ck)
		}
	}
	w2 := httptest.NewRecorder()
	c.r.ServeHTTP(w2, req)
	return w2.Code
}

// ── 4. The replay map cannot be filled into a global lockout ───────────────

func TestOAuthNonceFloodDoesNotLockOutCallbacks(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	liveUntil := time.Now().Add(stateTTL)
	for i := range nonceMaxEntries {
		p.usedNonces[fmt.Sprintf("flood-%d", i)] = liveUntil
	}
	p.lastNonceSweep = time.Now()

	state, err := p.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); !ok {
		t.Fatal("SECURITY: a full replay map refused a legitimate callback (global lockout)")
	}
	if got := len(p.usedNonces); got > nonceMaxEntries {
		t.Fatalf("nonce map grew past its cap: %d", got)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); ok {
		t.Fatal("SECURITY: the state just consumed replayed after eviction")
	}
}

// A callback with no code spends nothing: the nonce is recorded only for
// a callback that can complete.
func TestOAuthCallbackWithoutCodeRecordsNoNonce(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{name: "mock"})
	r := mountOAuth2Routes(mgr)
	op, _ := PluginAs[*OAuth2Plugin](mgr, "oauth2")
	state, err := op.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: op.stateCookieName(), Value: state})
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code-less callback = %d, want 400", w.Code)
	}
	op.noncesMu.Lock()
	n := len(op.usedNonces)
	op.noncesMu.Unlock()
	if n != 0 {
		t.Errorf("a code-less callback recorded %d nonce(s)", n)
	}
}

// ── 5. Concurrent refreshes spend a refresh token once ─────────────────────

func newSharedSQLOAuthTokenStore(t *testing.T) *SQLOAuthTokenStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1) // :memory: is per connection
	s, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		EncryptionKey: []byte("test-oauth-token-store-key-0123456789"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// rotatingTokenEndpoint models a provider with single-use, rotating
// refresh tokens: a spent refresh token answers 400 invalid_grant.
type rotatingTokenEndpoint struct {
	mu    sync.Mutex
	spent map[string]bool
	n     int
	hits  atomic.Int32
	delay time.Duration
	// before, when set, runs on each request before it is answered.
	before func(refresh string) (status int)
}

func (e *rotatingTokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.hits.Add(1)
	_ = r.ParseForm()
	rt := r.PostForm.Get("refresh_token")
	if e.before != nil {
		if st := e.before(rt); st != 0 {
			w.WriteHeader(st)
			return
		}
	}
	time.Sleep(e.delay)
	e.mu.Lock()
	if e.spent[rt] {
		e.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	e.spent[rt] = true
	e.n++
	n := e.n
	e.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  fmt.Sprintf("at-%d", n),
		"refresh_token": fmt.Sprintf("rt-%d", n),
		"expires_in":    3600,
	})
}

func newRotatingGoogle(t *testing.T, e *rotatingTokenEndpoint) *GoogleProvider {
	t.Helper()
	e.spent = map[string]bool{}
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	prov := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	prov.tokenEndpoint = srv.URL
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	prov.httpClient = &http.Client{Timeout: 10 * time.Second, Transport: tr}
	return prov
}

func TestConcurrentRefreshSpendsRefreshTokenOnce(t *testing.T) {
	e := &rotatingTokenEndpoint{delay: 100 * time.Millisecond}
	prov := newRotatingGoogle(t, e)
	store := newSharedSQLOAuthTokenStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, OAuthTokenRecord{
		UserID: "u1", Provider: "google", AccessToken: "at-0", RefreshToken: "rt-0", // not-a-secret: fixture
		Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	const callers = 8
	var wg sync.WaitGroup
	results := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = ValidOAuthToken(ctx, store, prov, "u1")
		}()
	}
	wg.Wait()
	for i := range callers {
		if errs[i] != nil {
			t.Errorf("caller %d: %v", i, errs[i])
		} else if results[i] != "at-1" {
			t.Errorf("caller %d got %q, want at-1", i, results[i])
		}
	}
	if got := e.hits.Load(); got != 1 {
		t.Errorf("SECURITY: the single-use refresh token was POSTed %d times", got)
	}
}

// Another replica refreshed first: our exchange of the now-spent token
// fails, and the newer record the other replica stored is the answer.
func TestRefreshFailureReturnsNewerRecord(t *testing.T) {
	store := newSharedSQLOAuthTokenStore(t)
	ctx := context.Background()
	e := &rotatingTokenEndpoint{}
	e.before = func(string) int {
		_ = store.Save(ctx, OAuthTokenRecord{
			UserID: "u1", Provider: "google", AccessToken: "at-other", RefreshToken: "rt-other", // not-a-secret: fixture
			Expiry: time.Now().Add(time.Hour),
		})
		return http.StatusBadRequest
	}
	prov := newRotatingGoogle(t, e)
	if err := store.Save(ctx, OAuthTokenRecord{
		UserID: "u1", Provider: "google", AccessToken: "at-0", RefreshToken: "rt-0", // not-a-secret: fixture
		Expiry: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := RefreshOAuthToken(ctx, store, prov, "u1")
	if err != nil {
		t.Fatalf("refresh lost to another replica answered an error: %v", err)
	}
	if rec.AccessToken != "at-other" {
		t.Errorf("got %q, want the other replica's at-other", rec.AccessToken)
	}
}

// ── 7. A token with no expires_in does not expire on arrival ───────────────

func TestNonExpiringProviderTokenIsUsable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gho_classic","token_type":"bearer"}`))
	}))
	t.Cleanup(srv.Close)
	gh := NewGitHubProvider("cid", "csec", "http://localhost/cb")
	gh.tokenEndpoint = srv.URL
	gh.httpClient = srv.Client()
	tok, err := gh.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if !tok.Expiry.IsZero() {
		t.Errorf("GitHub token with no expires_in got Expiry %v, want zero (non-expiring)", tok.Expiry)
	}
	g := NewGoogleProvider("cid", "csec", "http://localhost/cb")
	g.tokenEndpoint = srv.URL
	g.httpClient = srv.Client()
	if tok, err := g.ExchangeCode(context.Background(), "code"); err != nil || !tok.Expiry.IsZero() {
		t.Errorf("Google token with no expires_in: Expiry %v err %v, want zero", tok.Expiry, err)
	}

	store := newSQLOAuthTokenStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, OAuthTokenRecord{UserID: "u1", Provider: "github", AccessToken: "gho_classic"}); err != nil { // not-a-secret: fake token in a test store
		t.Fatal(err)
	}
	at, err := ValidOAuthToken(ctx, store, gh, "u1")
	if err != nil || at != "gho_classic" {
		t.Fatalf("ValidOAuthToken on a non-expiring token = %q, %v; want the stored token", at, err)
	}
}

// ── 8 + 9. The authenticated link flow ─────────────────────────────────────

func TestLinkFlowStoresProviderTokens(t *testing.T) {
	plugin, r, store, mgr := linkFixture(t, &OAuth2UserInfo{
		ID: "prov-link-tok", Email: "owner@example.com", Provider: "stub", EmailVerified: true,
	})
	tokens := newSQLOAuthTokenStore(t)
	plugin.tokenStore = tokens
	owner := store.preExistingUser("owner@example.com")
	sess, err := mgr.SessionStore().Create(context.Background(), owner.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if w := linkCallbackReq(t, plugin, r, owner.GetID(), sess.Token); w.Code != http.StatusFound {
		t.Fatalf("link = %d %s", w.Code, w.Body.String())
	}
	rec, err := tokens.Get(context.Background(), owner.GetID(), "stub")
	if err != nil || rec.AccessToken != "tok" {
		t.Fatalf("linked provider's tokens not stored: %+v %v", rec, err)
	}
}

// racedLinkStore lets another account bind the identity between the link
// flow's ownership check and its LinkOAuth, which keeps the first owner
// (the ON CONFLICT semantics of EntityUserStore).
type racedLinkStore struct {
	*linkingUserStore
	other string
	raced bool
}

func (s *racedLinkStore) FindByOAuth(ctx context.Context, provider, providerID string) (User, error) {
	if !s.raced {
		s.raced = true
		s.mu.Lock()
		s.links[provider+":"+providerID] = s.other
		s.mu.Unlock()
		return nil, ErrUserNotFound
	}
	return s.linkingUserStore.FindByOAuth(ctx, provider, providerID)
}

func (s *racedLinkStore) LinkOAuth(_ context.Context, userID, provider, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linkCalls++
	if _, taken := s.links[provider+":"+providerID]; !taken {
		s.links[provider+":"+providerID] = userID
	}
	return nil
}

func TestLinkFlowLosingBindingRaceIsRefused(t *testing.T) {
	inner := newLinkingUserStore()
	owner := inner.preExistingUser("owner@example.com")
	other := inner.preExistingUser("other@example.com")
	store := &racedLinkStore{linkingUserStore: inner, other: other.GetID()}
	sink := &recordingSink{}
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour, SessionCookie: "session_id",
		UserStore: store, AllowInMemoryStores: true, AuditSink: sink,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers: map[string]OAuth2Provider{"stub": &stubOAuthProvider{name: "stub", userInfo: &OAuth2UserInfo{
			ID: "contested", Email: "owner@example.com", EmailVerified: true,
		}}},
		StateSecret: "test-secret-state-01",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	sess, err := mgr.SessionStore().Create(context.Background(), owner.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	w := linkCallbackReq(t, plugin, r, owner.GetID(), sess.Token)
	if w.Code != http.StatusConflict {
		t.Errorf("link that lost the binding race = %d, want 409", w.Code)
	}
	for _, ev := range sink.snapshot() {
		if ev.Kind == "oauth.linked" {
			t.Errorf("SECURITY: oauth.linked audited for %s, but the identity is bound to %s", ev.UserID, other.GetID())
		}
	}
}

// ── 10. JWKS and discovery fetches ─────────────────────────────────────────

// A failed JWKS refresh with a usable stale set backs off instead of
// re-fetching on every login.
func TestJWKSRefreshFailureBacksOff(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) > 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}})
	}))
	t.Cleanup(srv.Close)
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Millisecond}
	ctx := context.Background()
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	for range 5 {
		if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
			t.Fatalf("stale key not served during the outage: %v", err)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("JWKS fetched %d times during an outage, want 2 (warm-up + one failed refresh)", got)
	}
}

// A slow JWKS refresh does not hold every other login behind the cache
// lock: a caller with a usable stale set is answered from it.
func TestJWKSRefreshDoesNotBlockStaleReaders(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	var hits atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) > 1 {
			close(entered)
			<-release
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}})
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Millisecond}
	ctx := context.Background()
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	go func() { _, _ = c.getKey(ctx, srv.URL, "kid-a") }()
	<-entered
	done := make(chan error, 1)
	go func() {
		_, err := c.getKey(ctx, srv.URL, "kid-a")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("stale reader: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("a login waited on another login's JWKS fetch while a stale key set was usable")
	}
	unblock()
}

// During a key rotation two logins carry the same new kid. The first spends
// the kid's refetch slot and fetches; the second, finding the slot spent,
// failed at once instead of waiting for that fetch (2026-11-01 review).
func TestJWKSConcurrentRotatedKidWaitsForInFlightFetch(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	keyB := mustRSAKey(t, 2048)
	var hits atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		keys := []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}
		if hits.Add(1) > 1 {
			close(entered)
			<-release
			keys = append(keys, rsaJWKMap("kid-b", &keyB.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Hour}
	ctx := context.Background()
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() {
		_, err := c.getKey(ctx, srv.URL, "kid-b")
		first <- err
	}()
	<-entered
	second := make(chan error, 1)
	go func() {
		_, err := c.getKey(ctx, srv.URL, "kid-b")
		second <- err
	}()
	time.Sleep(20 * time.Millisecond) // let the second login reach the spent slot
	unblock()
	for i, ch := range []chan error{first, second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("login %d during the rotation: %v", i+1, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("login %d never returned", i+1)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("JWKS fetched %d times, want 2 (warm-up + one rotation refetch)", got)
	}
}

// A refresh that fails on the caller's own deadline (a request timeout
// shorter than the HTTP client's) still backs off: a hanging IdP only ever
// fails that way, and without the backoff one login per stale period paid
// its whole deadline.
func TestJWKSDeadlineFailureBacksOff(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) > 1 {
			<-r.Context().Done() // hang until the caller gives up
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}})
	}))
	t.Cleanup(srv.Close)
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Millisecond}
	if _, err := c.getKey(context.Background(), srv.URL, "kid-a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := c.getKey(ctx, srv.URL, "kid-a")
		cancel()
		if err != nil {
			t.Fatalf("stale key not served: %v", err)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("JWKS fetched %d times while the IdP hung, want 2 (warm-up + one timed-out refresh)", got)
	}
}

// A stale-TTL refresh already in flight answers with the old key set; a
// login with a freshly rotated kid that waited behind it reused that
// outcome, spent the kid's forced-refetch slot without a fetch that could
// see the new key, and failed, as did every later login with that kid
// until forcedRefetchGap passed (2026-11-01 review, round 2).
func TestJWKSForcedRefetchDoesNotReuseStaleInFlightFetch(t *testing.T) {
	keyA := mustRSAKey(t, 2048)
	keyB := mustRSAKey(t, 2048)
	var hits atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		keys := []map[string]any{rsaJWKMap("kid-a", &keyA.PublicKey)}
		switch hits.Add(1) {
		case 1:
		case 2: // the stale refresh: started before the rotation
			close(entered)
			<-release
		default:
			keys = append(keys, rsaJWKMap("kid-b", &keyB.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	c := &jwksCache{httpClient: srv.Client(), ttl: time.Millisecond}
	ctx := context.Background()
	if _, err := c.getKey(ctx, srv.URL, "kid-a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	go func() { _, _ = c.getKey(ctx, srv.URL, "kid-a") }() // starts the stale refresh
	<-entered
	rotated := make(chan error, 1)
	go func() {
		_, err := c.getKey(ctx, srv.URL, "kid-b")
		rotated <- err
	}()
	time.Sleep(20 * time.Millisecond) // the rotated login queues behind the stale fetch
	unblock()
	select {
	case err := <-rotated:
		if err != nil {
			t.Errorf("rotated kid refused although the IdP serves it: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the rotated login never returned")
	}
}

// A caller's own deadline bounds its wait for discovery, even while
// another caller's fetch is in flight.
func TestOIDCDiscoveryWaitHonoursCallerDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	var relOnce sync.Once
	unblock := func() { relOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer: srv.URL, ClientID: "c", ClientSecret: "s", RedirectURL: "https://app.example.com/cb",
		HTTPClient: newOIDCTestHTTPClient(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = p.ensureDiscovery(context.Background()) }()
	<-entered
	done := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, _ = p.ensureDiscovery(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("a caller with a 50ms deadline waited on another caller's discovery fetch")
	}
	unblock()
}

// ── 11. Provider registry keys travel in the state token and the path ──────

func TestOAuthProviderKeyShapeIsValidated(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: accepted a provider key no flow can complete", name)
			}
		}()
		fn()
	}
	mustPanic("config key with a dot", func() {
		NewOAuth2Plugin(OAuth2Config{
			Providers:   map[string]OAuth2Provider{"corp.sso": &mockProvider{name: "corp"}},
			StateSecret: "test-state-secret-01",
		})
	})
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	mustPanic("RegisterProvider with a dot", func() { p.RegisterProvider("a.b", &mockProvider{name: "a"}) })
	mustPanic("RegisterProvider empty", func() { p.RegisterProvider("", &mockProvider{name: "a"}) })
	mustPanic("RegisterProvider with a slash", func() { p.RegisterProvider("a/b", &mockProvider{name: "a"}) })
	p.RegisterProvider("keycloak-prod_1", &mockProvider{name: "oidc"}) // a valid key
}

// ── 12. A link_conflict refusal names the local account it collided with ──

func TestOAuthLinkConflictEventNamesLocalUser(t *testing.T) {
	mock := &mockProvider{name: "mock", tokenResp: &OAuth2Token{AccessToken: "a"}, // not-a-secret: fixture
		userResp: &OAuth2UserInfo{ID: "ext-9", Email: "bob@example.com", EmailVerified: true}}
	mgr, users := newOAuth2Manager(t, mock)
	sink := &recordingSink{}
	mgr.config.AuditSink = sink
	bob := seedUser(t, users, "bob@example.com", "pw-existing-1")
	r := mountOAuth2Routes(mgr)
	if w := runCallback(t, mgr, r, "mock"); w.Code != http.StatusConflict {
		t.Fatalf("collision = %d, want 409", w.Code)
	}
	found := false
	for _, ev := range sink.snapshot() {
		if ev.Kind == "oauth.refused" && ev.Meta["reason"] == "link_conflict" {
			found = true
			if ev.UserID != bob.GetID() {
				t.Errorf("link_conflict event UserID = %q, want the colliding account %s (erasure scrubs by it)", ev.UserID, bob.GetID())
			}
		}
	}
	if !found {
		t.Fatal("no link_conflict event")
	}
}

// ── 6. Stored tokens are read under the key the callback stored them ──────

type refreshingMock struct {
	mockProvider
	refreshed atomic.Int32
}

func (m *refreshingMock) RefreshToken(context.Context, string) (*OAuth2Token, error) {
	m.refreshed.Add(1)
	return &OAuth2Token{AccessToken: "kc-refreshed", Expiry: time.Now().Add(time.Hour)}, nil // not-a-secret: fixture
}

// An OIDC provider registered as "keycloak" reports Name() "oidc"; the
// callback stores its tokens under "keycloak", and the plugin's token
// methods read them there.
func TestPluginTokenMethodsUseRegistryKey(t *testing.T) {
	prov := &refreshingMock{mockProvider: mockProvider{
		name:      "oidc",
		tokenResp: &OAuth2Token{AccessToken: "kc-access", RefreshToken: "kc-rt", Expiry: time.Now().Add(time.Hour)}, // not-a-secret: fixture
		userResp:  &OAuth2UserInfo{ID: "kc-1", Email: "kc@example.com"},
	}}
	users := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour, SessionCookie: "session_id",
		UserStore: users, AllowInMemoryStores: true,
	})
	tokens := newSQLOAuthTokenStore(t)
	op := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"keycloak": prov},
		StateSecret: "test-secret-key-state",
		TokenStore:  tokens,
	})
	mgr.Use(op)
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := mountOAuth2Routes(mgr)
	if w := runCallback(t, mgr, r, "keycloak"); w.Code != http.StatusFound {
		t.Fatalf("login = %d %s", w.Code, w.Body.String())
	}
	u, _, err := users.FindByEmail(context.Background(), "kc@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at, err := op.ValidOAuthToken(ctx, "keycloak", u.GetID())
	if err != nil || at != "kc-access" {
		t.Fatalf("ValidOAuthToken(keycloak) = %q, %v; want the stored kc-access", at, err)
	}
	rec, err := op.RefreshOAuthToken(ctx, "keycloak", u.GetID())
	if err != nil || rec.AccessToken != "kc-refreshed" {
		t.Fatalf("RefreshOAuthToken(keycloak) = %+v, %v", rec, err)
	}
	stored, err := tokens.Get(ctx, u.GetID(), "keycloak")
	if err != nil || stored.AccessToken != "kc-refreshed" {
		t.Fatalf("refreshed token not stored under the registry key: %+v %v", stored, err)
	}
	if _, err := op.ValidOAuthToken(ctx, "nope", u.GetID()); err == nil {
		t.Error("an unregistered provider key answered a token")
	}
}
