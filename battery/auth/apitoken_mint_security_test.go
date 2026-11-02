package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Found by the 2026-11-01 battery/auth bug bash.
//
// POST /auth/tokens turned any signed-in principal into a permanent `*:*`
// token. On an account with a second factor that included a session that
// predates enrollment (never stepped up, refused by 2FA disable, passkey add
// and password change) and a bare bearer JWT, which carries no step-up. The
// token management routes now hold the bar passkey add holds: on a 2FA
// account, only a cookie session that passed the factor.

type tokenMintRig struct {
	t      *testing.T
	r      *router.Router
	jwtH   http.Handler
	sessH  http.Handler
	tokens *SQLAPITokenStore
}

func newTokenMintRig(t *testing.T) *tokenMintRig {
	t.Helper()
	_, ts, _ := newTokenTestDB(t)
	store := newMemoryUserStore()
	hash, err := HashPassword("victim-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), "victim@example.com", hash, []string{"user"}); err != nil {
		t.Fatal(err)
	}
	mgr := New(AuthConfig{
		JWTSecret:           "mint-secret-0123456789abcdef-0123456789abcdef",
		SessionCookie:       "session_id",
		SessionTTL:          24 * time.Hour,
		UserStore:           store,
		AllowInMemoryStores: true,
	})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{RateLimit: &RateLimiterConfig{MaxAttempts: 1000, Window: time.Minute, BlockDuration: time.Minute}}))
	mgr.Use(NewTokensPlugin(ts))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return &tokenMintRig{t: t, r: r, sessH: SessionMiddleware(mgr)(r), jwtH: RequireAuth(mgr.JWT())(r), tokens: ts}
}

func (g *tokenMintRig) do(h http.Handler, method, path, body, bearer, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session_id="+cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func (g *tokenMintRig) login() (cookie, jwt string) {
	g.t.Helper()
	w := g.do(g.sessH, http.MethodPost, "/auth/login", `{"email":"victim@example.com","password":"victim-password-1"}`, "", "")
	if w.Code != http.StatusOK {
		g.t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c.Value
		}
	}
	return cookie, resp.Token
}

func (g *tokenMintRig) enroll(cookie string) {
	g.t.Helper()
	w := g.do(g.sessH, http.MethodPost, "/auth/2fa/enroll", "", "", cookie)
	var er struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &er)
	w = g.do(g.sessH, http.MethodPost, "/auth/2fa/verify", fmt.Sprintf(`{"code":%q}`, totpNow(er.Secret)), "", cookie)
	if w.Code != http.StatusOK {
		g.t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
}

const mintAll = `{"name":"x","scopes":["*:*"]}`

func TestTokenMintNeedsSteppedUpSessionOn2FAAccount(t *testing.T) {
	g := newTokenMintRig(t)
	early, earlyJWT := g.login()
	owner, _ := g.login()

	// Before enrollment both credentials mint: no factor to pass.
	if w := g.do(g.sessH, http.MethodPost, "/auth/tokens", mintAll, "", early); w.Code != http.StatusOK {
		t.Fatalf("control: pre-enrollment mint = %d %s", w.Code, w.Body.String())
	}
	g.enroll(owner)

	if w := g.do(g.sessH, http.MethodPost, "/auth/tokens", mintAll, "", early); w.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a session that predates 2FA enrollment minted a permanent token: %d %s", w.Code, w.Body.String())
	}
	if w := g.do(g.sessH, http.MethodGet, "/auth/tokens", "", "", early); w.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a session that predates 2FA enrollment listed tokens: %d", w.Code)
	}
	if w := g.do(g.jwtH, http.MethodPost, "/auth/tokens", mintAll, earlyJWT, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("SECURITY: a bearer JWT minted a permanent token on a 2FA account: %d %s", w.Code, w.Body.String())
	}
	// The session that enrolled passed the factor and still mints.
	if w := g.do(g.sessH, http.MethodPost, "/auth/tokens", mintAll, "", owner); w.Code != http.StatusOK {
		t.Errorf("stepped-up session refused: %d %s", w.Code, w.Body.String())
	}
	// A stale pre-enrollment cookie sent first does not hide the verified
	// one behind it, the way RequireTwoFA judges the pair.
	req := httptest.NewRequest(http.MethodGet, "/auth/tokens", nil)
	req.Header.Set("Cookie", "session_id="+early+"; session_id="+owner)
	w := httptest.NewRecorder()
	g.sessH.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("[pre-enrollment, verified] cookies: %d, want 200", w.Code)
	}
}

// ttl_seconds was multiplied into a Duration before the negative check ran:
// -2^55 wrapped to 0 (a permanent token) and 584 years wrapped to 290ms.
func TestTokenMintRefusesWrappingTTL(t *testing.T) {
	g := newTokenMintRig(t)
	cookie, _ := g.login()
	for _, ttl := range []string{"-1", "-36028797018963968", "18446744074", "9223372036854775807"} {
		w := g.do(g.sessH, http.MethodPost, "/auth/tokens", `{"name":"x","ttl_seconds":`+ttl+`}`, "", cookie)
		if w.Code != http.StatusBadRequest {
			t.Errorf("ttl_seconds=%s: status %d, want 400: %s", ttl, w.Code, w.Body.String())
		}
	}
	w := g.do(g.sessH, http.MethodPost, "/auth/tokens", fmt.Sprintf(`{"name":"x","ttl_seconds":%d}`, maxTokenTTLSeconds), "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("max ttl refused: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.ExpiresAt == nil || time.Until(*got.ExpiresAt) < 200*365*24*time.Hour {
		t.Errorf("max ttl expires at %v, want ~292 years out", got.ExpiresAt)
	}
}

// A revoked_at or expires_at that SQLite hands back in a shape the parser
// cannot read (unixepoch(), julianday('now'), an unlisted layout) read as
// NULL, so an operator's out-of-band revoke left the token live.
func TestTokenUnreadableRevokedAtFailsClosed(t *testing.T) {
	ctx := context.Background()
	for _, expr := range []string{"unixepoch()", "julianday('now')", "'2026-11-01T10:00:00.123+0000'", "CURRENT_TIMESTAMP"} {
		db, ts, _ := newTokenTestDB(t)
		pt, rec, err := IssueToken(ctx, ts, TokenSpec{Name: "n", OwnerKind: OwnerKindUser, OwnerID: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("UPDATE auth_api_tokens SET revoked_at = "+expr+", expires_at = "+expr+" WHERE id = $1", rec.ID); err != nil {
			t.Fatal(err)
		}
		got, err := ts.FindByHash(ctx, sha256hex(pt))
		if err != nil {
			t.Fatal(err)
		}
		if got.RevokedAt == nil {
			t.Errorf("SECURITY: revoked_at = %s read as not revoked", expr)
		}
		if got.ExpiresAt == nil || got.ExpiresAt.After(time.Now()) {
			t.Errorf("expires_at = %s read as live: %v", expr, got.ExpiresAt)
		}
		users := &staticUserStore{byID: map[string]User{"alice": &BasicUser{ID: "alice"}}}
		var seen User
		TokenMiddleware(users, nil, ts)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen = GetCurrentUser(r.Context())
		})).ServeHTTP(httptest.NewRecorder(), bearerRequest(http.MethodGet, "/x", pt, ""))
		if seen != nil {
			t.Errorf("SECURITY: token with revoked_at = %s authenticated", expr)
		}
	}
	// NULL stays NULL.
	_, ts, _ := newTokenTestDB(t)
	pt, _, _ := IssueToken(ctx, ts, TokenSpec{Name: "n", OwnerKind: OwnerKindUser, OwnerID: "alice"})
	if got, _ := ts.FindByHash(ctx, sha256hex(pt)); got.RevokedAt != nil || got.ExpiresAt != nil {
		t.Errorf("NULL timestamps read as set: %+v", got)
	}
}

// A brand of 12 or more chars filled the fixed 12-char display prefix, so
// every token listed and audited alike.
func TestTokenDisplayPrefixKeepsRandomChars(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	for _, brand := range []string{"", "gfsk_", "acmecorpprod_", "abcdefghijklmno_"} {
		_, a, err := IssueToken(ctx, ts, TokenSpec{Name: "a", OwnerKind: OwnerKindUser, OwnerID: "u", Prefix: brand})
		if err != nil {
			t.Fatal(err)
		}
		_, b, _ := IssueToken(ctx, ts, TokenSpec{Name: "b", OwnerKind: OwnerKindUser, OwnerID: "u", Prefix: brand})
		want := brand
		if want == "" {
			want = TokenPrefix
		}
		if !strings.HasPrefix(a.Prefix, want) || len(a.Prefix) != len(want)+tokenDisplayRandLen {
			t.Errorf("brand %q: display prefix %q, want the brand plus %d chars", brand, a.Prefix, tokenDisplayRandLen)
		}
		if a.Prefix == b.Prefix {
			t.Errorf("brand %q: two tokens share display prefix %q", brand, a.Prefix)
		}
	}
	if got := tokenDisplayPrefix("no-underscore-here-at-all"); got != "no-underscor" {
		t.Errorf("unbranded input: %q", got)
	}
}

// TokensPlugin reported its token brand ("acme_") as an embed-reserved path,
// and the AuthManager, the battery the framework actually reads, reported
// nothing, so a relocated BasePath lost the boot-time Reach guard.
func TestAuthReservesItsBasePathFromEmbeds(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, BasePath: "/account"})
	var er interface{ ReservedEmbedPrefixes() []string } = mgr
	if got := er.ReservedEmbedPrefixes(); len(got) != 1 || got[0] != "/account" {
		t.Errorf("AuthManager.ReservedEmbedPrefixes = %v, want [/account]", got)
	}
	p := NewTokensPlugin(nil).WithPrefix("acme_")
	if err := p.Init(mgr); err != nil {
		t.Fatal(err)
	}
	if got := p.ReservedEmbedPrefixes(); len(got) != 1 || got[0] != "/account" {
		t.Errorf("TokensPlugin.ReservedEmbedPrefixes = %v, want [/account]", got)
	}
}
