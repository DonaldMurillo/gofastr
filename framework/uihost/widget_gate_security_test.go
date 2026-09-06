package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// widget.Definition.RequireSession fails closed when no host installed a
// session predicate, so uihost must install one at mount time;
// otherwise the gate can only ever say no and the knob is unusable.
//
// This pins the wiring, not the gate itself (core-ui/widget owns that).
func TestUIHostInstallsWidgetSessionCheck(t *testing.T) {
	widget.SetSessionCheck(nil)
	t.Cleanup(func() { widget.SetSessionCheck(nil) })

	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a)
	ds.Mount(router.New())

	check := widget.SessionCheck()
	if check == nil {
		t.Fatal("uihost.Routes did not install a widget session check; RequireSession would 403 forever")
	}
	// An unauthenticated request must still be refused by it.
	if check(&http.Request{Header: http.Header{}}) {
		t.Error("installed check accepted a request with no session cookie")
	}
}

// r4GateUser is the minimal principal the authenticated gate looks for:
// whatever the app's session middleware resolved onto the request context.
type r4GateUser struct{}

func (r4GateUser) GetID() string      { return "u-r4" }
func (r4GateUser) GetEmail() string   { return "r4@example.com" }
func (r4GateUser) GetRoles() []string { return nil }

// Definition.RequireAuthenticated is the widget level for signals "not safe
// to expose anonymously" (2026-09-05 round-4 finding). RequireSession kept
// its any-browser-session meaning on purpose (per-session scoping,
// anti-recon), so this is the gate that must fail for the anonymous session
// handlePage auto-mints on the first page render: under uihost every visitor
// holds a valid session, so session possession gates exactly nothing.
//
// Property: possession of the self-minted anonymous session must NOT satisfy
// RequireAuthenticated on /state, /chrome, or the SSR-inline chrome surface;
// a request carrying a resolved principal MUST be served.
// Family: F17 Authorization at derived surfaces
func TestRequireAuthenticatedWidgetRefusesAnonSession(t *testing.T) {
	// The predicates are process-global; reset so this test owns them and
	// cannot poison siblings (mirrors every other test that touches them).
	widget.SetSessionCheck(nil)
	widget.SetAuthenticatedCheck(nil)
	t.Cleanup(func() {
		widget.SetSessionCheck(nil)
		widget.SetAuthenticatedCheck(nil)
	})

	rtr := router.New()
	def := widget.New("r4-anon-gate").
		Signal("secret", widget.SignalFunc(func() (any, error) {
			return "r4-secret-value", nil
		})).
		Slot("body", anonGateSlot{}).
		Build()
	def.RequireAuthenticated = true
	widget.Mount(rtr, &def)

	a := app.NewApp("r4gate")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.Mount(rtr)

	// Step 1: an anonymous visitor loads any page. handlePage mints a
	// session cookie unconditionally (there is no login anywhere in this
	// app) and embeds it in the response.
	page := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	pageW := httptest.NewRecorder()
	rtr.ServeHTTP(pageW, page)
	if pageW.Code != http.StatusOK {
		t.Fatalf("page render: status %d, want 200", pageW.Code)
	}
	var token string
	for _, c := range pageW.Result().Cookies() {
		if c.Name == sessionCookieDevName || c.Name == sessionCookieSecureName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("page render set no session cookie; the anonymous mint is the premise of this test")
	}
	// The anonymous page must not carry the gated widget's SSR-inlined
	// chrome either: /chrome 403s for this caller, and inlining it would
	// hand out exactly the HTML the endpoint refuses.
	if strings.Contains(pageW.Body.String(), "r4-anon-gate chrome") {
		t.Errorf("SECURITY: [uihost] RequireAuthenticated widget chrome SSR-inlined into the anonymous page — " +
			"injectWidgetSSR must apply the same gate as /state and /chrome, or the inline path leaks the chrome " +
			"one hop around the endpoint gate")
	}

	// Step 2: replay exactly that anonymous cookie against the gated widget
	// surfaces. Possession of a self-minted anonymous session must NOT
	// satisfy RequireAuthenticated.
	for _, tc := range []struct{ name, path string }{
		{"state", "/core-ui/widget/r4-anon-gate/state"},
		{"chrome", "/core-ui/widget/r4-anon-gate/chrome"},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://localhost"+tc.path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: token})
		w := httptest.NewRecorder()
		rtr.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Errorf("SECURITY: [uihost] RequireAuthenticated widget %s served 200 to a caller whose only credential is "+
				"the anonymous session the first page load auto-minted (body=%q). Definition.RequireAuthenticated promises "+
				"callers whose signals are \"not safe to expose anonymously\" a gate; the anonymous session every visitor "+
				"holds must not satisfy it",
				tc.name, truncate(w.Body.String(), 200))
		}
	}

	// Step 3 (control): the same surfaces must serve a request whose
	// context carries the principal battery/auth's SessionMiddleware
	// resolves — otherwise the gate is just a permanent 403 and the level
	// is unusable.
	authed := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		req = req.WithContext(handler.SetUser(req.Context(), r4GateUser{}))
		req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: token})
		w := httptest.NewRecorder()
		rtr.ServeHTTP(w, req)
		return w
	}
	if w := authed("/core-ui/widget/r4-anon-gate/state"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "r4-secret-value") {
		t.Errorf("authenticated caller: /state = %d (body=%q), want 200 carrying the signal — the gate must be satisfiable",
			w.Code, truncate(w.Body.String(), 200))
	}
	if w := authed("/core-ui/widget/r4-anon-gate/chrome"); w.Code != http.StatusOK {
		t.Errorf("authenticated caller: /chrome = %d, want 200 — the gate must be satisfiable", w.Code)
	}
	authedPage := authed("/")
	if authedPage.Code != http.StatusOK || !strings.Contains(authedPage.Body.String(), "r4-anon-gate chrome") {
		t.Errorf("authenticated caller: page = %d, want 200 with the widget chrome SSR-inlined (body has marker: %v)",
			authedPage.Code, strings.Contains(authedPage.Body.String(), "r4-anon-gate chrome"))
	}
}

// anonGateSlot renders a marker body so /chrome and the SSR inline path have
// content worth gating.
type anonGateSlot struct{}

func (anonGateSlot) Render() render.HTML {
	return render.HTML("<p>r4-anon-gate chrome</p>")
}
