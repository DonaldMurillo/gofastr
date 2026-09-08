package auth

// Pins: every site that mints the session cookie emits SameSite=Strict,
// by construction — the battery has ONE session-cookie literal, inside
// mintSessionCookie (core.go), used by login, logout, magic-link verify,
// and the OAuth callback. Found by the 2026-09-06 adversarial pass
// (round 5). The login and logout mints were already pinned by name
// (TestCorePlugin_LoginCookieUsesStrictSameSite /
// TestCorePlugin_LogoutCookieUsesStrictSameSite); magic-link verify and
// the OAuth callback had minted Lax since their original commit with no
// decision comment. The OAuth STATE cookie (oauthStateCookie) is NOT
// the session cookie and keeps its Lax: it must ride the provider's
// top-level redirect back.
// TestSessionCookieSingleMintSite (below) scans the package's non-test
// sources and fails if any http.Cookie literal names the session cookie
// outside mintSessionCookie.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMagicLinkRedCookieStrict: a same-origin form POST redeeming a valid
// magic link mints the session cookie; that mint must carry
// SameSite=Strict like the password-login mint it parallels.
func TestMagicLinkRedCookieStrict(t *testing.T) {
	r, plugin := magicLinkCSRFRouter(t)

	token, err := createPurposeToken(context.Background(), plugin.tokenStore, purposeMagicLink, "alice@example.com", time.Hour)
	if err != nil {
		t.Fatalf("setup broken: createPurposeToken: %v", err)
	}

	req := magicConfirmReq(token) // same-origin form POST, no cross-site markers
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := mintedSession(rec); got == "" {
		t.Fatalf("setup broken: same-origin verify minted no session (status %d, body %q)", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "SameSite=Strict") {
		t.Errorf("SECURITY: [session-cookie-samesite-mint] magic-link verify minted the session cookie as %q, "+
			"want SameSite=Strict — password login's mint is Strict (TestCorePlugin_LoginCookieUsesStrictSameSite); "+
			"a Lax session rides any top-level cross-site GET and re-opens the CSRF exposure Strict closed",
			rec.Header().Get("Set-Cookie"))
	}
}

// TestOauthCallbackRedCookieStrict: the provider callback completing a
// legitimate same-browser flow mints the session cookie; that mint must
// carry SameSite=Strict too. The state cookie may stay Lax (it must ride
// the provider's redirect); the session cookie has no such return trip.
func TestOauthCallbackRedCookieStrict(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{
		name:      "mock",
		tokenResp: &OAuth2Token{AccessToken: "tok"},
		userResp:  &OAuth2UserInfo{ID: "u-1", Email: "alice@example.com"},
	})
	r := mountOAuth2Routes(mgr)

	// Start the flow in "this browser": the redirect plants the state
	// cookie the callback requires (browser binding).
	redirectReq := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, redirectReq)
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("setup broken: parse redirect: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("setup broken: no state in the provider redirect")
	}

	cb := httptest.NewRequest(http.MethodGet, "/auth/oauth/mock/callback?state="+url.QueryEscape(state)+"&code=abc", nil)
	for _, c := range w.Result().Cookies() {
		cb.AddCookie(c)
	}
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, cb)

	if cw.Code != http.StatusFound {
		t.Fatalf("setup broken: callback status %d: %s", cw.Code, cw.Body.String())
	}
	// The response clears the state cookie AND mints the session; only the
	// session mint is under test (the state cookie keeps its Lax — it must
	// ride the provider's redirect, oauth2.go:241-244).
	if got := mintedSession(cw); got == "" {
		t.Fatalf("setup broken: callback minted no session cookie (Set-Cookie %q)", cw.Header().Values("Set-Cookie"))
	}
	var sessionHeader string
	for _, h := range cw.Header().Values("Set-Cookie") {
		if strings.HasPrefix(h, "session_id=") {
			sessionHeader = h
		}
	}
	if !strings.Contains(sessionHeader, "SameSite=Strict") {
		t.Errorf("SECURITY: [session-cookie-samesite-mint] OAuth callback minted the session cookie as %q, "+
			"want SameSite=Strict — password login's mint is Strict (TestCorePlugin_LoginCookieUsesStrictSameSite); "+
			"a Lax session rides any top-level cross-site GET and re-opens the CSRF exposure Strict closed",
			sessionHeader)
	}
}

// TestSessionCookieSingleMintSite: the package's non-test sources hold
// exactly ONE construction site for the session cookie — mintSessionCookie.
// Any http.Cookie literal elsewhere that derives its Name from the
// session-cookie config (a SessionCookie field reference) re-opens the
// drift this round closed (magic-link and OAuth shipped Lax for their
// whole lives because each hand-rolled its own literal). The OAuth STATE
// cookie is exempt by name: it is not the session cookie and must stay
// Lax to ride the provider's redirect.
func TestSessionCookieSingleMintSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("setup broken: read package dir: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("setup broken: parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name == "mintSessionCookie" {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Cookie" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "http" {
					return true
				}
				checked++
				if namesSessionCookie(lit) {
					pos := fset.Position(lit.Pos())
					t.Errorf("SECURITY: [session-cookie-samesite-mint] %s:%d constructs an http.Cookie naming the session "+
						"cookie outside mintSessionCookie — every session-cookie literal outside the one helper is how the "+
						"magic-link and OAuth mints drifted to Lax; route it through mintSessionCookie (the OAuth STATE "+
						"cookie, oauthStateCookie, is a different cookie and exempt)", name, pos.Line)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("setup broken: no http.Cookie literals found in non-test sources — the scan no longer sees the package")
	}
}

// namesSessionCookie reports whether the cookie literal's Name field is
// derived from the session-cookie config (any .SessionCookie field
// selection), rather than a fixed non-session cookie name like
// oauthStateCookie.
func namesSessionCookie(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Name" {
			continue
		}
		sessionCookieRef := false
		ast.Inspect(kv.Value, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "SessionCookie" {
				sessionCookieRef = true
			}
			return !sessionCookieRef
		})
		if sessionCookieRef {
			return true
		}
	}
	return false
}
