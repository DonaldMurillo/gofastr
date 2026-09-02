package uihost

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
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

// explicitIDComp declares its action id explicitly (app.ScreenComponentID),
// the documented fix the panic message above recommends for a derived-id
// collision. It must participate in the SAME claimed-id map: an explicit
// id that collides with another screen's DERIVED id is the same silent
// cross-wiring, just entered through the escape hatch.
type explicitIDComp struct{ id string }

func (c *explicitIDComp) Render() render.HTML { return render.Raw("<p>explicit</p>") }
func (c *explicitIDComp) Actions() {
	component.On("save", func(*component.ComponentContext) {})
}
func (c *explicitIDComp) ScreenTitle() string        { return "Explicit" }
func (c *explicitIDComp) ScreenDescription() string  { return "" }
func (c *explicitIDComp) ScreenType() app.ScreenType { return app.ScreenPage }
func (c *explicitIDComp) ComponentID() string        { return c.id }

// TestExplicitIDCollisionAlsoRefused pins the other edge of the collision
// guard: the panic must fire when an EXPLICIT ComponentID collides with
// another screen's path-derived id, not only when two paths derive the
// same id. An explicit id that silently shadows (or is shadowed by) a
// derived one cross-wires data-action clicks between the two screens
// exactly like the derived/derived case the sibling test pins.
func TestExplicitIDCollisionAlsoRefused(t *testing.T) {
	a := app.NewApp("action-collide-explicit")
	// Derives id "admin-users" via pathToActionID.
	a.RegisterScreen(app.NewScreen("/admin/users", &actionTestComp{
		html:    "<p>a</p>",
		actions: func() { component.On("save", func(*component.ComponentContext) {}) },
	}).WithTitle("Users A"), nil)
	// Declares the SAME id explicitly from an unrelated route.
	a.RegisterScreen(app.NewScreen("/billing", &explicitIDComp{id: pathToActionID("/admin/users")}).WithTitle("Billing"), nil)
	ds := New(a)

	var pv any
	func() {
		defer func() { pv = recover() }()
		ds.AutoCompileActions()
	}()
	if pv == nil {
		t.Fatalf("SECURITY: [action-id-collision] an explicit ComponentID(%q) colliding with the id derived "+
			"from /admin/users registered silently: the claimed-id guard only catches derived/derived "+
			"collisions, and one screen's data-action clicks would run the other's handler",
			pathToActionID("/admin/users"))
	}
	msg := fmt.Sprint(pv)
	for _, route := range []string{"/admin/users", "/billing"} {
		if !strings.Contains(msg, route) {
			t.Errorf("refusal should name both colliding routes, want %q in %q", route, msg)
		}
	}
}
