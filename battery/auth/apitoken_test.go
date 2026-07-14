package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// ─── test fixtures ──────────────────────────────────────────────────────────

// newTokenTestDB opens an in-memory sqlite DB and returns both SQL stores
// with their schemas ensured. Skips when the sqlite3 driver is absent.
func newTokenTestDB(t *testing.T) (*sql.DB, *SQLAPITokenStore, *SQLServiceAccountStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1) // :memory: is per-connection; one conn shares the DB
	ts, err := NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatalf("NewSQLAPITokenStore: %v", err)
	}
	ss, err := NewSQLServiceAccountStore(db)
	if err != nil {
		t.Fatalf("NewSQLServiceAccountStore: %v", err)
	}
	return db, ts, ss
}

// staticUserStore is a map-backed UserStore for middleware tests. It lets a
// test control exactly which users resolve (success/missing) without a DB.
type staticUserStore struct{ byID map[string]User }

func (s *staticUserStore) FindByEmail(context.Context, string) (User, string, error) {
	return nil, "", ErrUserNotFound
}
func (s *staticUserStore) FindByID(_ context.Context, id string) (User, error) {
	if u, ok := s.byID[id]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}
func (s *staticUserStore) CreateUser(context.Context, string, string, []string) (User, error) {
	return nil, ErrEmailTaken
}

func bearerRequest(method, target, plaintext string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.Header.Set("Authorization", "Bearer "+plaintext)
	return r
}

func bearerRequestWithJSON(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// waitForLastUsed polls the token row until last_used_at is non-nil (the
// async touch goroutine has committed) or the deadline elapses.
func waitForLastUsed(t *testing.T, db *sql.DB, id string) *time.Time {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var raw any
		if err := db.QueryRow("SELECT last_used_at FROM auth_api_tokens WHERE id = $1", id).Scan(&raw); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}
		if p := timePtrFromRaw(raw); p != nil {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("last_used_at never set within timeout")
	return nil
}

// ─── IssueToken ─────────────────────────────────────────────────────────────

func TestIssueToken_ValidatesSpec(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	cases := []struct {
		name string
		spec TokenSpec
		want string
	}{
		{"empty name", TokenSpec{OwnerKind: "user", OwnerID: "u1"}, "name"},
		{"bad owner kind", TokenSpec{Name: "n", OwnerKind: "alien", OwnerID: "u1"}, "owner_kind"},
		{"empty owner id", TokenSpec{Name: "n", OwnerKind: "user"}, "owner_id"},
		{"bad scope format", TokenSpec{Name: "n", OwnerKind: "user", OwnerID: "u1", Scopes: []string{"nope"}}, "scope"},
		{"too many scopes", TokenSpec{Name: "n", OwnerKind: "user", OwnerID: "u1", Scopes: bigScopes(33)}, "scopes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := IssueToken(context.Background(), ts, tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want substring %q", err, tc.want)
			}
		})
	}
}

// TestIssueToken_WildcardScopesIssuable guards the matcher/issuer contract:
// HasScope documents "*:*" as grant-all and "*:read" as read-across-all, and
// scopeMatches supports them — so IssueToken MUST accept them. A regex that
// forbids "*" in the resource half would make those scopes unmintable, i.e.
// a documented, matcher-tested feature reachable only by bypassing IssueToken.
func TestIssueToken_WildcardScopesIssuable(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	for _, scopes := range [][]string{{"*:*"}, {"*:read"}, {"posts:*"}} {
		pt, _, err := IssueToken(context.Background(), ts, TokenSpec{
			Name: "n", OwnerKind: "user", OwnerID: "u1", Scopes: scopes,
		})
		if err != nil {
			t.Fatalf("IssueToken(scopes=%v) rejected a scope the matcher honors: %v", scopes, err)
		}
		if pt == "" {
			t.Fatalf("IssueToken(scopes=%v) returned empty plaintext", scopes)
		}
	}
}

func bigScopes(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "posts:read"
	}
	return out
}

func TestIssueToken_PlaintextFormat(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	pt, rec, err := IssueToken(context.Background(), ts, TokenSpec{
		Name: "ci", OwnerKind: "user", OwnerID: "u1", Scopes: []string{"posts:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pt, TokenPrefix) {
		t.Errorf("plaintext %q missing prefix %q", pt, TokenPrefix)
	}
	if got := len(pt); got != 45 {
		t.Errorf("plaintext length = %d, want 45 (gfsk_ + 40 hex)", got)
	}
	if got := len(rec.Prefix); got != tokenPrefixLen {
		t.Errorf("prefix length = %d, want %d", got, tokenPrefixLen)
	}
	if rec.Prefix != pt[:tokenPrefixLen] {
		t.Errorf("prefix %q != first %d of plaintext %q", rec.Prefix, tokenPrefixLen, pt)
	}
}

// ─── SQL store ──────────────────────────────────────────────────────────────

func TestSQLAPIToken_FindByHash(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u1", Scopes: []string{"a:b"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ts.FindByHash(ctx, sha256hex(pt))
	if err != nil || got == nil {
		t.Fatalf("FindByHash = %v, %v", got, err)
	}
	if got.ID != rec.ID || got.Name != rec.Name {
		t.Errorf("FindByHash returned %+v, want id=%s", got, rec.ID)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "a:b" {
		t.Errorf("scopes = %v", got.Scopes)
	}
	// Unknown hash → (nil, nil).
	if other, err := ts.FindByHash(ctx, sha256hex("gfsk_nope_no_real_token_here")); err != nil || other != nil {
		t.Errorf("unknown hash = %v, %v; want nil,nil", other, err)
	}
}

func TestSQLServiceAccount_CreateGet(t *testing.T) {
	_, _, ss := newTokenTestDB(t)
	ctx := context.Background()
	sa := NewServiceAccount("deploy-bot", []string{"admin", "deploy"})
	if err := ss.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	got, err := ss.Get(ctx, sa.ID)
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if got.Name != "deploy-bot" || len(got.Roles) != 2 {
		t.Errorf("service account = %+v", got)
	}
	if _, err := ss.Get(ctx, "missing"); err != nil {
		t.Errorf("missing Get err = %v, want nil", err)
	}
}

func TestRevoke_IdempotentAndOwnerScoped(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	_, rec, _ := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "alice"})
	if err := ts.Revoke(ctx, rec.ID, "user", "alice"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	// Idempotent: revoking again is a no-op success.
	if err := ts.Revoke(ctx, rec.ID, "user", "alice"); err != nil {
		t.Errorf("idempotent revoke: %v", err)
	}
	// Owner-scoped: bob cannot touch alice's token.
	if err := ts.Revoke(ctx, rec.ID, "user", "bob"); err != ErrTokenNotFound {
		t.Errorf("foreign revoke err = %v, want ErrTokenNotFound", err)
	}
}

// ─── middleware: authentication ─────────────────────────────────────────────

func TestTokenMiddleware_AuthUser(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	users := &staticUserStore{byID: map[string]User{"alice": alice}}

	pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "alice", Scopes: []string{"posts:read"}})
	if err != nil {
		t.Fatal(err)
	}
	var seen User
	var seenScopes []string
	var scopesOK bool
	mw := TokenMiddleware(users, nil, ts)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = GetCurrentUser(r.Context())
		seenScopes, scopesOK = TokenScopes(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))

	if seen == nil || seen.GetID() != "alice" {
		t.Fatalf("principal = %+v, want alice", seen)
	}
	if !scopesOK || len(seenScopes) != 1 || seenScopes[0] != "posts:read" {
		t.Errorf("token scopes in ctx = %v, ok=%v", seenScopes, scopesOK)
	}
}

func TestTokenMiddleware_AuthServiceAccount(t *testing.T) {
	_, ts, ss := newTokenTestDB(t)
	ctx := context.Background()
	sa := NewServiceAccount("ci-runner", []string{"deploy", "reader"})
	if err := ss.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "ci", OwnerKind: "service", OwnerID: sa.ID, Scopes: []string{"deploys:run"}})
	if err != nil {
		t.Fatal(err)
	}
	var seen User
	mw := TokenMiddleware(nil, ss, ts)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = GetCurrentUser(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), bearerRequest("POST", "/deploys", pt, ""))
	if seen == nil || seen.GetID() != sa.ID {
		t.Fatalf("principal = %+v, want service %s", seen, sa.ID)
	}
	// Roles flow through the User interface (feeding RequireRole / access.Can).
	roles := seen.GetRoles()
	if len(roles) != 2 || roles[0] != "deploy" {
		t.Errorf("service-account roles = %v, want [deploy reader]", roles)
	}
	if seen.GetEmail() != "" {
		t.Errorf("service-account email = %q, want empty", seen.GetEmail())
	}
}

// ─── scope matching ─────────────────────────────────────────────────────────

func TestScopeMatches(t *testing.T) {
	cases := []struct {
		held []string
		want string
		ok   bool
	}{
		{[]string{"posts:read"}, "posts:read", true},
		{[]string{"posts:read"}, "posts:write", false},
		{[]string{"posts:*"}, "posts:read", true},
		{[]string{"posts:*"}, "users:read", false},
		{[]string{"*:*"}, "anything:here", true},
		{[]string{"*:*"}, "posts:read", true},
		{nil, "posts:read", false}, // empty scopes deny everything
		{[]string{}, "posts:read", false},
		{[]string{"posts:read", "users:*"}, "users:delete", true},
	}
	for _, c := range cases {
		if got := scopeMatches(c.held, c.want); got != c.ok {
			t.Errorf("scopeMatches(%v, %q) = %v, want %v", c.held, c.want, got, c.ok)
		}
		// The exported wrapper must be byte-for-byte the same matcher (it's
		// reused by framework/pluginhost's capability gate).
		if got := ScopeMatch(c.held, c.want); got != c.ok {
			t.Errorf("ScopeMatch(%v, %q) = %v, want %v", c.held, c.want, got, c.ok)
		}
	}
}

func TestRequireScope_TokenAllowedDenied(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}

	mk := func(scopes []string) string {
		pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u", Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		return pt
	}
	allowed := mk([]string{"posts:read"})
	none := mk(nil) // empty scopes

	check := func(name, plaintext, scope string, wantStatus int) {
		t.Helper()
		chain := TokenMiddleware(users, nil, ts)(RequireScope(scope)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, bearerRequest("GET", "/x", plaintext, ""))
		if rec.Code != wantStatus {
			t.Errorf("%s: status=%d, want %d", name, rec.Code, wantStatus)
		}
	}
	check("exact-match", allowed, "posts:read", http.StatusOK)
	check("missing-verb", allowed, "posts:write", http.StatusForbidden)
	check("empty-scopes", none, "posts:read", http.StatusForbidden)
}

// RequireScope lets non-token (session/JWT) requests pass unscoped.
func TestRequireScope_SessionPasses(t *testing.T) {
	h := RequireScope("posts:read")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// No token scopes in ctx → treated as a session, unscoped → passes.
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &BasicUser{ID: "u"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("session through RequireScope: status=%d, want 200", rec.Code)
	}
}

// A token-authenticated caller must NOT reach the token-management surface:
// otherwise a leaked scoped (or empty-scoped) token could mint a *:* token
// for the same owner, defeating the scope model. The endpoints are
// session-only — TokenMiddleware marks token requests via ctx scopes, so the
// handler distinguishes them from real sessions even though both set a ctx
// user.
func TestTokensPlugin_RejectsTokenAuthCaller(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "plugin-test", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice", Roles: []string{"user"}}
	// Simulate what TokenMiddleware installs on a valid-token request: the
	// owner user AND the token's scopes in ctx.
	tokenCtx := func() context.Context {
		ctx := handler.SetUser(context.Background(), alice)
		return WithTokenScopes(ctx, []string{"posts:read"})
	}

	do := func(method, target, body string, h http.HandlerFunc) int {
		var req *http.Request
		if body != "" {
			req = bearerRequestWithJSON(method, target, body)
		} else {
			req = httptest.NewRequest(method, target, nil)
		}
		req = req.WithContext(tokenCtx())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("POST", "/auth/tokens", `{"name":"pwn","scopes":["*:*"]}`, plugin.createTokenHandler()); code != http.StatusUnauthorized {
		t.Errorf("create via token auth: status=%d, want 401", code)
	}
	if code := do("GET", "/auth/tokens", "", plugin.listTokensHandler()); code != http.StatusUnauthorized {
		t.Errorf("list via token auth: status=%d, want 401", code)
	}
	if code := do("DELETE", "/auth/tokens/some-id", "", plugin.revokeTokenHandler()); code != http.StatusUnauthorized {
		t.Errorf("revoke via token auth: status=%d, want 401", code)
	}
	// No token was minted through the token-auth path.
	list, err := ts.List(context.Background(), OwnerKindUser, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("a token was minted via token-auth caller: %d tokens", len(list))
	}
}

// ─── management endpoints: full cycle ───────────────────────────────────────

func TestTokensPlugin_CreateListRevoke(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "plugin-test", DevMode: true})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}

	// CREATE — owner forced from the session user; body owner_* ignored.
	createBody := `{"name":"ci","scopes":["posts:read"],"ttl_seconds":3600,"owner_kind":"service","owner_id":"evil"}`
	req := bearerRequestWithJSON("POST", "/auth/tokens", createBody)
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Token     string     `json:"token"`
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		Prefix    string     `json:"prefix"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || !strings.HasPrefix(created.Token, TokenPrefix) {
		t.Fatalf("response missing plaintext token: %s", rec.Body.String())
	}
	if created.ExpiresAt == nil {
		t.Error("expected expiresAt set from ttl_seconds")
	}

	// LIST — shows prefix, never the plaintext.
	listReq := bearerRequestWithJSON("GET", "/auth/tokens", "")
	listReq = listReq.WithContext(handler.SetUser(listReq.Context(), alice))
	listRec := httptest.NewRecorder()
	plugin.listTokensHandler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d", listRec.Code)
	}
	if strings.Contains(listRec.Body.String(), created.Token) {
		t.Errorf("LIST LEAKED PLAINTEXT: %s", listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), created.Prefix) {
		t.Errorf("list missing prefix %q: %s", created.Prefix, listRec.Body.String())
	}

	// The token authenticates before revocation.
	users := &staticUserStore{byID: map[string]User{"alice": alice}}
	mw := TokenMiddleware(users, nil, ts)
	verify := func(pt string, wantNil bool) {
		t.Helper()
		var seen User
		h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = GetCurrentUser(r.Context()) }))
		h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))
		if wantNil && seen != nil {
			t.Errorf("expected anonymous after revoke, got %+v", seen)
		}
		if !wantNil && (seen == nil || seen.GetID() != "alice") {
			t.Errorf("expected alice, got %+v", seen)
		}
	}
	verify(created.Token, false)

	// REVOKE — owner-scoped; then the token stops working.
	delReq := httptest.NewRequest("DELETE", "/auth/tokens/"+created.ID, nil)
	delReq.SetPathValue("id", created.ID)
	delReq = delReq.WithContext(handler.SetUser(delReq.Context(), alice))
	delRec := httptest.NewRecorder()
	plugin.revokeTokenHandler().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d, want 204", delRec.Code)
	}
	verify(created.Token, true) // revoked → anonymous
}

// ─── last-used throttling ───────────────────────────────────────────────────

func TestTokenMiddleware_LastUsedThrottle(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	// Wrap to count physical writes (async goroutine calls TouchLastUsed).
	var writeCount atomic.Int32
	counting := &countingTouchStore{APITokenStore: ts, count: &writeCount}

	mw := TokenMiddleware(users, nil, counting)
	fire := func() {
		h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))
	}
	fire() // last_used nil → touch
	fire() // last_used <60s old → skip

	// Wait for the (single) async touch to land, then ensure it doesn't drift.
	db := ts.db
	first := waitForLastUsed(t, db, rec.ID)
	time.Sleep(50 * time.Millisecond) // give any spurious second goroutine room to (not) run
	if c := writeCount.Load(); c != 1 {
		t.Errorf("TouchLastUsed call count = %d, want 1 (throttle)", c)
	}
	var raw any
	if err := db.QueryRow("SELECT last_used_at FROM auth_api_tokens WHERE id = $1", rec.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if p := timePtrFromRaw(raw); p == nil || !p.Equal(*first) {
		t.Errorf("last_used_at drifted after throttle: first=%v now=%v", first, p)
	}
}

// countingTouchStore wraps a real store and counts TouchLastUsed writes.
type countingTouchStore struct {
	APITokenStore
	count *atomic.Int32
}

func (c *countingTouchStore) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	c.count.Add(1)
	return c.APITokenStore.TouchLastUsed(ctx, id, at)
}
