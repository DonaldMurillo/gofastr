package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// CHAIN1-R4: pathToActionID (uihost.go:885-894) derives component ids by
// replacing '/' with '-' and dropping ':', so /admin/users and
// /admin-users both derive "admin-users". CompileActions caches
// first-wins (uihost.go:838-840) and never registers the second screen's
// registry, so one screen's compiled server actions silently run on the
// other screen's pages: a data-action click on /admin-users posts
// componentId=admin-users, which resolves in /admin/users' registry and
// executes its Go handler with the wrong page's params — in a channel
// where the handler also cannot see the caller (see the action-identity
// pin).
//
// Property: an action-id collision between two registered screens is
// never silent — either the derived ids do not collide (each screen's
// Go handler is invocable under its own id) or compilation refuses
// loudly. Today the second screen's Go handler is unreachable and
// nothing is reported at boot.
func TestScreenActionIDCollisionNotSilent(t *testing.T) {
	var ranA, ranB bool
	compA := &actionTestComp{html: "<p>a</p>", actions: func() {
		component.On("save", func(*component.ComponentContext) { ranA = true })
	}}
	compB := &actionTestComp{html: "<p>b</p>", actions: func() {
		component.On("save", func(*component.ComponentContext) { ranB = true })
	}}

	a := app.NewApp("action-collide")
	a.RegisterScreen(app.NewScreen("/admin/users", compA).WithTitle("Users A"), nil)
	a.RegisterScreen(app.NewScreen("/admin-users", compB).WithTitle("Users B"), nil)
	ds := New(a)

	// A loud boot refusal (panic/error naming the colliding routes) also
	// satisfies the property; recover and treat it as detected.
	refused := false
	func() {
		defer func() {
			if recover() != nil {
				refused = true
			}
		}()
		ds.AutoCompileActions()
	}()
	if refused {
		return // collision detected at boot: the silent path was avoided
	}

	// Enumerate the ids the host actually registered (white-box: same package).
	ds.mu.RLock()
	ids := make([]string, 0, len(ds.actionHandlers))
	for id := range ds.actionHandlers {
		ids = append(ids, id)
	}
	ds.mu.RUnlock()

	sess := ds.CreateSession()
	invoke := func(id string) {
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/action",
			strings.NewReader(`{"action":"save","params":{},"componentId":"`+id+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
		ds.ServeHTTP(httptest.NewRecorder(), req)
	}
	for _, id := range ids {
		invoke(id)
	}

	if !ranA || !ranB {
		t.Errorf("SECURITY: [action-id-collision] /admin/users and /admin-users both derive "+
			"component id %q via pathToActionID (uihost.go:885-894, '/'→'-'), and CompileActions' "+
			"first-wins cache (uihost.go:838-840) silently drops one screen's registry: registered ids=%v, "+
			"handlerA ran=%v handlerB ran=%v. A data-action on the losing screen cross-wires onto the "+
			"winner's Go handler with the wrong page's params, with no diagnostic at boot.",
			pathToActionID("/admin/users"), ids, ranA, ranB)
	}
}
