package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// PROPERTY
//
//	The desktop local identity is a FALLBACK for an app that has no
//	authentication battery. It never speaks for a request another
//	battery has already decided about, including when that decision
//	was "this request is anonymous".
//
// WHY (the deep finding, sec-auditor 2026-09-04; PROVEN by execution)
//
//	localuser.go says, in its own words:
//
//	  "Within desktop mode it is a FALLBACK: a user already in the
//	   context (battery/auth's session middleware ahead of it ...) is
//	   never overridden. ... it is what lets an app that mounts both
//	   auth and desktop keep auth's identity end to end."
//
//	The implementation tests for that with
//
//	  if _, ok := handler.GetUser(r.Context()); !ok { ...install local... }
//
//	but battery/auth's anonymous path is
//
//	  session_middleware.go:89   ctx := handler.SetUser(r.Context(), nil)
//
//	and handler.GetUser type-asserts, so a stored nil comes back
//	ok=FALSE. Auth's explicit "nobody is signed in" is byte-identical to
//	"no middleware ran". Desktop then installs its local identity -
//	whose GetRoles() is ["admin"] (localuser.go:35).
//
//	Measured on this tree: a request auth marked anonymous reaches the
//	handler with roles ["admin"]. In an app that mounts battery/auth (and
//	battery/admin, which authorizes through exactly that structural
//	GetRoles() interface: admin.go:273, rbac_admin.go:569) a signed-out
//	desktop window is an administrator.
//
//	It is worse than a single wrong answer, because the outcome depends
//	on battery REGISTRATION ORDER: if desktop's Use is installed first,
//	auth's anon path overwrites the local user with nil and the request
//	is anonymous instead. Two orderings, two security postures, no test.
//
// FIX SHAPE: decide at Init, not per request. battery/auth installs the
// framework/owner extractor from its package init(), so
// a FOREIGN owner.GetExtractor() (one the desktop battery did not
// install itself) is a reliable "an auth battery is linked into this
// binary" signal, installOwnerExtractor keys on exactly that. When it is true, the desktop battery
// installs no identity at all and auth owns identity end to end. The
// ["admin"] role stays as-is for the auth-free desktop app, which is
// what localuser_test.go pins.

// TestAnonDecisionIsNotOverridden is the core pin.
func TestAnonDecisionIsNotOverridden(t *testing.T) {
	// Stand in for battery/auth being linked into the binary: its
	// package init() installs the framework/owner extractor, which is
	// the signal the fix keys on.
	prev := owner.GetExtractor()
	owner.SetExtractor(func(context.Context) (any, bool) { return nil, false })
	t.Cleanup(func() { owner.SetExtractor(prev) })

	b, _ := newTestBattery(t)
	b.user = &localUser{id: "0123456789abcdef0123456789abcdef"}
	b.installOwnerExtractor() // the seam Init runs; it already sees the foreign extractor
	b.armGate()

	var roles []string
	var sawUser bool
	h := b.localUserMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := handler.GetUser(r.Context())
		sawUser = ok
		if rh, ok := u.(interface{ GetRoles() []string }); ok {
			roles = rh.GetRoles()
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Exactly what battery/auth's anonymous path leaves behind.
	req = req.WithContext(handler.SetUser(req.Context(), nil))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if sawUser || len(roles) > 0 {
		t.Fatalf("a request another battery marked ANONYMOUS reached the handler as roles=%v.\n"+
			"battery/auth's SetUser(ctx, nil) is indistinguishable from \"no middleware ran\", so the desktop "+
			"FALLBACK overrode auth's decision and made a signed-out window an administrator", roles)
	}
}

// TestLocalIdentityStillFillsInAlone is the anti-vacuity half: with no
// auth battery in the process, the desktop app must still get its
// owner id, or every owner-scoped screen goes blank.
func TestLocalIdentityStillFillsInAlone(t *testing.T) {
	prev := owner.GetExtractor()
	owner.SetExtractor(nil)
	t.Cleanup(func() { owner.SetExtractor(prev) })

	b, _ := newTestBattery(t)
	b.user = &localUser{id: "0123456789abcdef0123456789abcdef"}
	b.installOwnerExtractor()
	b.armGate()

	var gotID string
	h := b.localUserMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := handler.GetUser(r.Context()); ok {
			if ih, ok := u.(interface{ GetID() string }); ok {
				gotID = ih.GetID()
			}
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if gotID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("the local identity did not reach the handler (got %q); an auth-free desktop app has no owner id", gotID)
	}
}

// TestUnarmedGateInstallsNoIdentity pins the --serve posture: with the
// boot gate unarmed there is no window handshake, so no anonymous
// browser may become the local admin.
func TestUnarmedGateInstallsNoIdentity(t *testing.T) {
	b, _ := newTestBattery(t)
	b.user = &localUser{id: "0123456789abcdef0123456789abcdef"}

	sawUser := false
	h := b.localUserMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawUser = handler.GetUser(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if sawUser {
		t.Fatal("an unarmed (--serve / $PORT) request received the local admin identity")
	}
}
