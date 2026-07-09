package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// capturingSink records every SecurityEvent for assertion. It is the
// AuditSink used by the fail-closed tests to prove the audit trail never
// carries a plaintext credential.
type capturingSink struct {
	mu     sync.Mutex
	events []SecurityEvent
}

func (c *capturingSink) SecurityEvent(_ context.Context, ev SecurityEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

// assertNoLeak fails the test if needle appears anywhere in the captured
// audit events (Kind or any Meta value).
func (c *capturingSink) assertNoLeak(t *testing.T, needle string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, ev := range c.events {
		if strings.Contains(ev.Kind, needle) {
			t.Errorf("audit event[%d].Kind contains plaintext: %q", i, ev.Kind)
		}
		for k, v := range ev.Meta {
			if strings.Contains(v, needle) {
				t.Errorf("audit event[%d].Meta[%q] contains plaintext: %q", i, k, v)
			}
		}
	}
}

// runMw drives TokenMiddleware with a gfsk_ credential, pre-seeding an
// "outer" user in ctx (to prove a failed gfsk_ credential clears it). It
// returns the principal the downstream handler observed.
func runMwClearsUser(t *testing.T, users UserStore, accounts ServiceAccountStore, tokens APITokenStore, cred string) User {
	t.Helper()
	outer := &BasicUser{ID: "outer-user", Email: "outer@example.com"}
	var seen User
	mw := TokenMiddleware(users, accounts, tokens)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = GetCurrentUser(r.Context())
	}))
	req := bearerRequest("GET", "/x", cred, "")
	req = req.WithContext(handler.SetUser(req.Context(), outer)) // outer identity present
	h.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

// ─── 1. plaintext never stored ──────────────────────────────────────────────

func TestAPIToken_PlaintextNeverStored(t *testing.T) {
	db, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{
		Name: "secret", OwnerKind: "user", OwnerID: "u1", Scopes: []string{"a:b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "name", "owner_kind", "owner_id", "prefix", "hash",
		"scopes", "expires_at", "last_used_at", "revoked_at", "created_at"}
	rawVals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range rawVals {
		ptrs[i] = &rawVals[i]
	}
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM auth_api_tokens WHERE id = $1", strings.Join(cols, ", ")), rec.ID,
	).Scan(ptrs...); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	for i, col := range cols {
		got := fmt.Sprintf("%v", rawVals[i])
		if strings.Contains(got, pt) {
			t.Errorf("column %q contains the plaintext token: %q", col, got)
		}
	}
	// Sanity: the plaintext really is a full gfsk_ token (else the test is vacuous).
	if !strings.HasPrefix(pt, TokenPrefix) || len(pt) != 45 {
		t.Fatalf("test plaintext is not a real token shape: %q", pt)
	}
}

// ─── 2. unknown / revoked / expired → anonymous, outer user CLEARED ─────────

func TestAPIToken_UnknownTokenClearsUser(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	// A valid-format gfsk_ credential that is simply not in the store.
	cred, _ := generateAPITokenPlaintext()
	if seen := runMwClearsUser(t, users, nil, ts, cred); seen != nil {
		t.Errorf("IDENTITY LEAK: unknown gfsk_ token left outer user %+v in ctx", seen)
	}
}

func TestAPIToken_RevokedTokenClearsUser(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.Revoke(ctx, rec.ID, "user", "u"); err != nil {
		t.Fatal(err)
	}
	if seen := runMwClearsUser(t, users, nil, ts, pt); seen != nil {
		t.Errorf("IDENTITY LEAK: revoked token left outer user %+v in ctx", seen)
	}
}

func TestAPIToken_ExpiredTokenClearsUser(t *testing.T) {
	db, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{
		Name: "t", OwnerKind: "user", OwnerID: "u", TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate expiry into the past.
	if _, err := db.ExecContext(ctx,
		"UPDATE auth_api_tokens SET expires_at = $1 WHERE id = $2",
		time.Now().Add(-time.Hour), rec.ID); err != nil {
		t.Fatal(err)
	}
	if seen := runMwClearsUser(t, users, nil, ts, pt); seen != nil {
		t.Errorf("IDENTITY LEAK: expired token left outer user %+v in ctx", seen)
	}
}

// ─── 3. disabled service account → anonymous ───────────────────────────────

func TestAPIToken_DisabledServiceAccountAnonymous(t *testing.T) {
	_, ts, ss := newTokenTestDB(t)
	ctx := context.Background()
	sa := NewServiceAccount("bot", []string{"deploy"})
	if err := ss.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "service", OwnerID: sa.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.SetDisabled(ctx, sa.ID, true); err != nil {
		t.Fatal(err)
	}
	if seen := runMwClearsUser(t, nil, ss, ts, pt); seen != nil {
		t.Errorf("IDENTITY LEAK: disabled service account resolved as %+v", seen)
	}
}

// owner_missing: a valid token whose owner no longer resolves must go
// anonymous AND emit a token.auth_failed reason=owner_missing audit event.
func TestAPIToken_OwnerMissingAudits(t *testing.T) {
	ctx := context.Background()
	_, ts, _ := newTokenTestDB(t)
	// Empty user store: the token's owner ("ghost") is gone.
	users := &staticUserStore{byID: map[string]User{}}
	pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &capturingSink{}
	mw := TokenMiddleware(users, nil, ts, WithTokenAudit(sink))
	var seen User
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = GetCurrentUser(r.Context())
	}))
	req := bearerRequest("GET", "/x", pt, "")
	req = req.WithContext(handler.SetUser(req.Context(), &BasicUser{ID: "outer"}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != nil {
		t.Errorf("owner_missing token resolved as %+v, want anonymous", seen)
	}
	var gotReason bool
	sink.mu.Lock()
	for _, ev := range sink.events {
		if ev.Kind == "token.auth_failed" && ev.Meta["reason"] == "owner_missing" {
			gotReason = true
		}
	}
	sink.mu.Unlock()
	if !gotReason {
		t.Errorf("missing token.auth_failed reason=owner_missing audit event")
	}
	sink.assertNoLeak(t, pt)
}

// ─── 4. non-gfsk bearer passes through, does NOT clear an outer user ─────────

func TestAPIToken_NonGfskBearerPassesThrough(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	outer := &BasicUser{ID: "outer-user"}
	var seen User
	mw := TokenMiddleware(users, nil, ts)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = GetCurrentUser(r.Context())
	}))
	// A bearer credential that is NOT a gfsk_ token (JWT/session owns it).
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+"plain-jwt-value")
	req = req.WithContext(handler.SetUser(req.Context(), outer))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen == nil || seen.GetID() != "outer-user" {
		t.Errorf("non-gfsk bearer must pass through untouched; saw %+v, want outer-user", seen)
	}
}

// ─── 5. create endpoint ignores body owner_kind/owner_id ────────────────────

func TestAPIToken_CreateIgnoresBodyOwner(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true})
	mgr.Init(nil)
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice"}
	// Body tries to mint a token owned by "evil" / kind "service".
	body := `{"name":"pwn","owner_kind":"service","owner_id":"evil","scopes":["a:b"]}`
	req := bearerRequestWithJSON("POST", "/auth/tokens", body)
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The token MUST be owned by alice (the session user), not "evil".
	aliceToks, err := ts.List(ctx, "user", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceToks) != 1 {
		t.Errorf("alice should own 1 token, got %d (owner not forced to session user)", len(aliceToks))
	}
	evilToks, _ := ts.List(ctx, "user", "evil")
	if len(evilToks) != 0 {
		t.Errorf("body owner_id leaked: %d tokens owned by 'evil'", len(evilToks))
	}
	svcToks, _ := ts.List(ctx, "service", "evil")
	if len(svcToks) != 0 {
		t.Errorf("body owner_kind leaked: %d service tokens owned by 'evil'", len(svcToks))
	}
}

// ─── 6. revoke is owner-scoped (A cannot revoke B's token) ──────────────────

func TestAPIToken_RevokeOwnerScopedHTTP(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true})
	mgr.Init(nil)
	plugin := NewTokensPlugin(ts)
	plugin.Init(mgr)

	alice := &BasicUser{ID: "alice"}
	bob := &BasicUser{ID: "bob"}

	// Alice creates a token.
	body := `{"name":"alice-tok"}`
	createReq := bearerRequestWithJSON("POST", "/auth/tokens", body)
	createReq = createReq.WithContext(handler.SetUser(createReq.Context(), alice))
	createRec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("alice create: %d", createRec.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	// Bob tries to revoke Alice's token → 404 (owner-scoped).
	delReq := httptest.NewRequest("DELETE", "/auth/tokens/"+created.ID, nil)
	delReq.SetPathValue("id", created.ID)
	delReq = delReq.WithContext(handler.SetUser(delReq.Context(), bob))
	delRec := httptest.NewRecorder()
	plugin.revokeTokenHandler().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNotFound {
		t.Errorf("bob revoking alice's token: status=%d, want 404", delRec.Code)
	}

	// Alice can still revoke her own token.
	delReq2 := httptest.NewRequest("DELETE", "/auth/tokens/"+created.ID, nil)
	delReq2.SetPathValue("id", created.ID)
	delReq2 = delReq2.WithContext(handler.SetUser(delReq2.Context(), alice))
	delRec2 := httptest.NewRecorder()
	plugin.revokeTokenHandler().ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNoContent {
		t.Errorf("alice revoking own token: status=%d, want 204", delRec2.Code)
	}
}

// ─── 7. empty-scopes token: HasScope false, RequireScope 403 ───────────────

func TestAPIToken_EmptyScopesDenied(t *testing.T) {
	ctx := context.Background()
	_, ts, _ := newTokenTestDB(t)
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	pt, _, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u"}) // no scopes
	if err != nil {
		t.Fatal(err)
	}
	// HasScope must be false for every scope on an empty-scopes token.
	// We drive it through the middleware so scopes come from ctx.
	var hs []bool
	mw := TokenMiddleware(users, nil, ts)
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hs = append(hs, HasScope(r.Context(), "anything:read"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))
	if len(hs) != 1 || hs[0] {
		t.Errorf("empty-scopes token HasScope = %v, want false", hs)
	}
	// RequireScope 403s.
	chain := TokenMiddleware(users, nil, ts)(RequireScope("posts:read")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, bearerRequest("GET", "/x", pt, ""))
	if rec.Code != http.StatusForbidden {
		t.Errorf("empty-scopes token through RequireScope: status=%d, want 403", rec.Code)
	}
}

// ─── 8. error strings + audit events never contain the plaintext ───────────

func TestAPIToken_AuditNeverContainsPlaintext(t *testing.T) {
	ctx := context.Background()
	_, ts, _ := newTokenTestDB(t)
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	pt, rec, err := IssueToken(ctx, ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.Revoke(ctx, rec.ID, "user", "u"); err != nil {
		t.Fatal(err)
	}
	sink := &capturingSink{}
	mw := TokenMiddleware(users, nil, ts, WithTokenAudit(sink))
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))

	if len(sink.events) == 0 {
		t.Fatal("expected a token.auth_failed audit event, got none")
	}
	var sawReason bool
	sink.mu.Lock()
	for _, ev := range sink.events {
		if ev.Kind == "token.auth_failed" && ev.Meta["reason"] == "revoked" {
			sawReason = true
		}
	}
	sink.mu.Unlock()
	if !sawReason {
		t.Errorf("audit did not record token.auth_failed reason=revoked")
	}
	sink.assertNoLeak(t, pt)
}

func TestAPIToken_IssueTokenErrorsNoPlaintext(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	// A valid token's plaintext must not appear in any validation error.
	good, _, err := IssueToken(context.Background(), ts, TokenSpec{Name: "t", OwnerKind: "user", OwnerID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = IssueToken(context.Background(), ts, TokenSpec{OwnerKind: "user", OwnerID: "u"}) // empty name
	if err == nil || strings.Contains(err.Error(), good) {
		t.Errorf("validation error leaked plaintext: %v", err)
	}
}

// ─── 9. malformed gfsk_ credential → anonymous, no panic ───────────────────

func TestAPIToken_MalformedGfskTokenAnonymous(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	users := &staticUserStore{byID: map[string]User{"u": &BasicUser{ID: "u"}}}
	valid, err := generateAPITokenPlaintext()
	if err != nil {
		t.Fatal(err)
	}
	malformed := []string{
		valid[:len(TokenPrefix)+3],            // too short
		TokenPrefix + strings.Repeat("z", 40), // valid length, bad charset
		valid[:len(TokenPrefix)],              // bare prefix only
	}
	for i, cred := range malformed {
		seen := runMwClearsUser(t, users, nil, ts, cred)
		if seen != nil {
			t.Errorf("malformed gfsk_[%d] %q resolved as %+v (want anonymous, no panic)", i, cred, seen)
		}
	}
}
