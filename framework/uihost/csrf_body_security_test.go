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
	// to any path under it must be a plain 404: no handler, no method-only
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

// actionRanFlags records which of the two compiled actions ran, for the
// strict-key tests below.
type actionRanFlags struct{ safe, danger bool }

// strictActionHarness compiles a two-action screen ("safe"/"danger"),
// proves the single-key form executes (so a later failure is about the
// key shape, not the harness), and returns a raw-body POST func, the
// compiled action id, and the ran flags.
func strictActionHarness(t *testing.T) (post func(string) *httptest.ResponseRecorder, id string, ran *actionRanFlags) {
	t.Helper()
	ran = &actionRanFlags{}
	comp := &actionTestComp{html: "<p>dup</p>", actions: func() {
		component.On("safe", func(*component.ComponentContext) { ran.safe = true })
		component.On("danger", func(*component.ComponentContext) { ran.danger = true })
	}}
	a := app.NewApp("dup-action-keys")
	a.RegisterScreen(app.NewScreen("/dup", comp).WithTitle("Dup"), nil)
	ds := New(a)
	ds.AutoCompileActions()

	id = ""
	ds.mu.RLock()
	for k := range ds.actionHandlers {
		if id == "" || k == "dup" {
			id = k
		}
	}
	ds.mu.RUnlock()
	if id == "" {
		t.Fatal("setup: no server actions compiled for the screen")
	}

	sess := ds.CreateSession()
	post = func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		return rec
	}

	// Sanity: the endpoint accepts and executes the single-key form, so
	// any later failure is about the duplicate keys, not the harness.
	if rec := post(`{"action":"safe","params":{},"componentId":"` + id + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("setup: single-key action POST = %d (body %.200s)", rec.Code, rec.Body.String())
	}
	if !ran.safe {
		t.Fatal("setup: single-key POST did not invoke the safe action")
	}
	return post, id, ran
}

// TestServerActionRejectsDuplicateKeys: the action endpoint refuses a
// body with an exact duplicate top-level key. encoding/json resolves
// duplicates last-wins, so a proxy or log that de-duplicates differently
// would see a different request than the server executes.
func TestServerActionRejectsDuplicateKeys(t *testing.T) {
	post, id, ran := strictActionHarness(t)
	body := `{"action":"safe","action":"danger","params":{},"componentId":"` + id + `"}`
	if rec := post(body); rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/action accepted a body with a duplicate "+
			"top-level key with status %d — last-wins decoding executes the second value while "+
			"any first-read intermediary saw the first; the body must be refused (handler.UnmarshalStrict)",
			rec.Code)
	}
	if ran.danger {
		t.Error("SECURITY: [uihost] the last-wins decode EXECUTED the second \"action\" value — " +
			"a body with duplicate top-level keys must be rejected before decode")
	}
}

// TestServerActionRejectsCaseFoldedKeys: "action"/"Action" fold onto the
// same struct field via stdlib json's tag-insensitive match — a duplicate
// modulo folding; survives a dedup-only fix.
func TestServerActionRejectsCaseFoldedKeys(t *testing.T) {
	post, id, ran := strictActionHarness(t)
	body := `{"action":"safe","Action":"danger","params":{},"componentId":"` + id + `"}`
	if rec := post(body); rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [uihost] POST /__gofastr/action accepted a body with case-folded "+
			"top-level keys with status %d — the folded key matches the same field last-wins; "+
			"the body must be refused (handler.UnmarshalStrict)", rec.Code)
	}
	if ran.danger {
		t.Error("SECURITY: [uihost] the case-folded decode EXECUTED the second \"action\" value — " +
			"a body with case-folded top-level keys must be rejected before decode")
	}
}
