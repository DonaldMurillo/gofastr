package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

func TestUIHost_SessionCookieUsesHostPrefix(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "__Host-gofastr-session=") {
		t.Fatalf("SECURITY: [uihost-cookie] session cookie missing __Host- prefix: %q", cookie)
	}
}

func TestUIHost_SessionCookieUsesStrictSameSite(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("SECURITY: [uihost-cookie] session cookie not marked SameSite=Strict: %q", cookie)
	}
}

func TestUIHost_ServerActionRejectsCrossOriginPost(t *testing.T) {
	a := app.NewApp("action-csrf")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	handlerCalled := false
	ic := &actionTestComp{
		actions: func() {
			component.On("test-action", func(ctx *component.ComponentContext) {
				handlerCalled = true
			})
		},
	}
	ds.CompileActions("test-comp", ic)

	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(`{"action":"test-action","params":{},"session":"forged-session","componentId":"test-comp"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [uihost-csrf] cross-origin server action returned %d and invoked=%v. Attack: CSRF against server action endpoint.", rec.Code, handlerCalled)
	}
	if handlerCalled {
		t.Fatal("SECURITY: [uihost-csrf] cross-origin server action invoked handler. Attack: CSRF against server action endpoint.")
	}
}

func TestUIHost_ServerActionRejectsOversizeBody(t *testing.T) {
	ds := newTestUIHost()

	huge := `{"action":"noop","params":{"payload":"` + strings.Repeat("A", 1<<20) + `"},"session":"forged-session","componentId":"missing"}`
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(huge))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [uihost-body] oversize action body returned %d. Attack: unbounded JSON body DoS on server action endpoint.", rec.Code)
	}
}

func TestUIHost_RemovedSignalEndpointReturns404(t *testing.T) {
	// The /__gofastr/signal/{id} surface has been removed (dead server-side
	// signal map + island re-render path with no production callers). A POST
	// to any path under it must be a plain 404 — no handler, no method-only
	// 405, no auth challenge, no body parsing.
	ds := newTestUIHost()

	req := httptest.NewRequest(http.MethodPost, "/__gofastr/signal/anything", strings.NewReader(`{"value":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-removed] POST /__gofastr/signal/anything returned %d, want 404. Attack: removed endpoint still routed.", rec.Code)
	}
}

func TestUIHost_ActionsJSRequiresAuth(t *testing.T) {
	a := app.NewApp("action-js-exposure")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	ic := &actionTestComp{
		actions: func() {
			component.On("test-action", func(ctx *component.ComponentContext) {})
		},
	}
	ds.CompileActions("test-comp", ic)

	req := httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-actions] unauthenticated actions.js returned %d and exposed %q. Attack: action surface discovery without auth.", rec.Code, rec.Body.String())
	}
}

func TestUIHost_ServerActionUnknownComponentReturnsNotFound(t *testing.T) {
	ds := newTestUIHost()

	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(`{"action":"missing","params":{},"session":"forged-session","componentId":"missing-comp"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-action] unknown component probe returned %d. Attack: action endpoint reveals component existence via 200 error responses.", rec.Code)
	}
}

func TestUIHost_ServerActionUnknownActionReturnsNotFound(t *testing.T) {
	a := app.NewApp("action-probe")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	ic := &actionTestComp{
		actions: func() {
			component.On("known-action", func(ctx *component.ComponentContext) {})
		},
	}
	ds.CompileActions("test-comp", ic)

	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(`{"action":"missing","params":{},"session":"forged-session","componentId":"test-comp"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: [uihost-action] unknown action probe returned %d. Attack: action endpoint reveals action names via 200 error responses.", rec.Code)
	}
}

func TestUIHost_CreateSessionRejectsCrossOriginRequest(t *testing.T) {
	ds := newTestUIHost()

	req := httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [uihost-session] cross-origin session minting returned %d. Attack: CSRF can mint sessions from attacker origins.", rec.Code)
	}
}
