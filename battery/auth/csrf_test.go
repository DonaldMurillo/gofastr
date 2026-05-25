package auth

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSRF_GETSetsCookie(t *testing.T) {
	mw := CSRF()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s cookie on GET; cookies=%v", CSRFCookieName, rec.Result().Cookies())
	}
}

func TestCSRF_POSTRejectsMissingToken(t *testing.T) {
	mw := CSRF()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler should NOT have been called")
	}))
	req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader("data=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on POST w/o token; got %d", rec.Code)
	}
}

func TestCSRF_POSTAcceptsFormBodyToken(t *testing.T) {
	mw := CSRF()
	// 1. Issue a GET to obtain the token cookie.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no CSRF cookie from GET")
	}

	// 2. POST the same token in the form body — should pass.
	called := false
	postHandler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	form := url.Values{CSRFFormField: {cookie.Value}, "data": {"x"}}
	postReq := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	postHandler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("form-body CSRF rejected: %d / %s", postRec.Code, postRec.Body.String())
	}
	if !called {
		t.Error("downstream handler not invoked")
	}
}

func TestCSRF_POSTAcceptsHeaderToken(t *testing.T) {
	mw := CSRF()
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}

	postHandler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	postReq := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(`{"x":1}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set(CSRFHeaderName, cookie.Value)
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	postHandler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("header CSRF rejected: %d / %s", postRec.Code, postRec.Body.String())
	}
}

func TestCSRFInputHTML_EmitsHiddenInput(t *testing.T) {
	mw := CSRF()
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(getRec, getReq)

	var cookie *http.Cookie
	for _, c := range getRec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}

	// Build a fresh request that has the cookie set (simulating the next page).
	r := httptest.NewRequest(http.MethodGet, "/form", nil)
	r.AddCookie(cookie)

	htmlOut := string(CSRFInputHTML(r))
	if !strings.Contains(htmlOut, `name="_csrf"`) {
		t.Errorf("missing name attr: %q", htmlOut)
	}
	if !strings.Contains(htmlOut, `value="`+cookie.Value+`"`) {
		t.Errorf("missing token value: %q", htmlOut)
	}
}

// TestCSRFInputFromCtx_MatchesRequestVariant pins the ctx-based helper:
// for screens that only get a context.Context, CSRFInputFromCtx must
// produce the SAME hidden-input markup as CSRFInputHTML(r). Without
// this, screens have to either thread a *http.Request through or
// reinvent the markup against middleware.TokenFromContext (V3 #4).
func TestCSRFInputFromCtx_MatchesRequestVariant(t *testing.T) {
	mw := CSRF()
	rec := httptest.NewRecorder()
	var ctxToken string
	var fromReq template.HTML
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxToken = CSRFTokenFromCtx(r.Context())
		fromReq = CSRFInputHTML(r)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if ctxToken == "" {
		t.Fatal("ctx had no token after CSRF middleware ran")
	}
	fromCtx := CSRFInputFromCtx(contextWithCSRFTok(ctxToken))
	if string(fromCtx) != string(fromReq) {
		t.Errorf("ctx-based input differs from request-based:\n  ctx: %s\n  req: %s", fromCtx, fromReq)
	}
	if !strings.Contains(string(fromCtx), `name="_csrf"`) {
		t.Errorf("ctx variant missing _csrf name: %q", fromCtx)
	}
}

// TestCSRFInputFromCtx_EmptyWhenNoToken pins the no-middleware path:
// calling CSRFInputFromCtx with a bare context yields "" rather than
// stamping an empty hidden input that would break form decoding.
func TestCSRFInputFromCtx_EmptyWhenNoToken(t *testing.T) {
	if got := CSRFInputFromCtx(context.Background()); got != "" {
		t.Errorf("expected empty markup when no token on ctx, got %q", got)
	}
	if got := CSRFTokenFromCtx(context.Background()); got != "" {
		t.Errorf("expected empty token when none on ctx, got %q", got)
	}
}

// contextWithCSRFTok builds a context carrying tok where the auth
// helpers expect to find it (the middleware's internal ctx key).
// Using middleware.TokenFromContext indirectly via a real request is
// brittle for unit tests; the helper exposes the same surface.
func contextWithCSRFTok(tok string) context.Context {
	// We rely on the middleware running once to set ctx; replay via a
	// fresh request whose cookie carries tok, then capture ctx in a handler.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: tok})
	rec := httptest.NewRecorder()
	var captured context.Context
	CSRF()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Context()
	})).ServeHTTP(rec, req)
	return captured
}

// TestCSRF_WithDevCSRFKey_StableAcrossInstantiation pins V3 #5: a
// dev-mode key persisted to disk survives a "process restart" (a
// fresh CSRF() construction) so cookies minted under it still verify.
// Without the helper, the auto-key rotates and every browser tab 403s.
func TestCSRF_WithDevCSRFKey_StableAcrossInstantiation(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "dev-csrf.key")

	// First "process": mint a cookie under the on-disk key.
	mw1 := CSRF(WithDevCSRFKey(keyPath))
	rec := httptest.NewRecorder()
	mw1(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("setup: no cookie from first instance")
	}

	// Second "process": brand-new middleware loads the SAME key.
	// Cookie + matching form value must POST cleanly.
	mw2 := CSRF(WithDevCSRFKey(keyPath))
	postReq := httptest.NewRequest(http.MethodPost, "/save",
		strings.NewReader("_csrf="+cookie.Value))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	mw2(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Errorf("cookie minted by first instance rejected by second: %d / %s",
			postRec.Code, postRec.Body.String())
	}
}

// TestCSRF_WithCSRFSkipPaths_PinsPerRouteExemption pins V3 #9: the
// auth-battery convenience for declaring CSRF exemptions inline with
// CSRF() construction, without hand-rolling a Skip closure.
func TestCSRF_WithCSRFSkipPaths_PinsPerRouteExemption(t *testing.T) {
	mw := CSRF(WithCSRFSkipPaths("/webhooks/", "/health"))

	// /webhooks/* — skipped.
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("/webhooks/stripe POST: should bypass CSRF, got %d", rec.Code)
	}

	// /save — still enforced (no cookie → 403).
	rec2 := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/save", nil))
	if rec2.Code != http.StatusForbidden {
		t.Errorf("/save POST: should still be enforced, got %d", rec2.Code)
	}

	// Bearer auth still skipped — the option layers on top of
	// SkipBearerAuth, not in place of it.
	bearerReq := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(`{}`))
	bearerReq.Header.Set("Authorization", "Bearer xxx")
	bearerReq.Header.Set("Content-Type", "application/json")
	bearerRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(bearerRec, bearerReq)
	if bearerRec.Code != http.StatusTeapot {
		t.Errorf("bearer-auth API POST should still be skipped, got %d", bearerRec.Code)
	}
}

func TestCSRF_SkipsBearerAuth(t *testing.T) {
	mw := CSRF()
	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer xxx")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Error("Bearer-auth request blocked by CSRF — should have been skipped")
	}
}
