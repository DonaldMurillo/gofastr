package embed

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A grant is delegated authority parked in a page the app does not control.
// What it may reach is decided by the surface, not by whether the app author
// remembered to gate a route — the framework mounts /mcp, {auth}/tokens and
// /admin/* itself, so "the author will gate it" was never a property anyone
// could hold.

func TestGrantReachesOnlyItsSurfaceAndDeclaredPrefixes(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	try := func(path string) int {
		p := &probe{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set(GrantHeader, grant)
		h.Middleware()(p.handler()).ServeHTTP(rec, req)
		return rec.Code
	}

	// In reach: the surface's own subtree, /__gofastr/*, and declared Reach.
	for _, ok := range []string{
		"/embed/dashboard",
		"/embed/dashboard/rows",
		"/__gofastr/action",
		"/api/posts",
		"/api/posts/42",
	} {
		if got := try(ok); got == http.StatusForbidden {
			t.Errorf("%s was refused; it is in the surface's reach", ok)
		}
	}

	// Out of reach: everything else, including the framework's own routes.
	for _, denied := range []string{
		"/admin/users",
		"/auth/tokens",
		"/mcp",
		"/api/invoices",
		"/openapi.json",
		"/embed/dashboard-admin", // adjacent name, NOT a subtree
	} {
		if got := try(denied); got != http.StatusForbidden {
			t.Errorf("%s answered %d; a grant for one surface must not reach it", denied, got)
		}
	}
}

// Traversal must not smuggle a path past the prefix match.
func TestReachMatchesTheCleanedPath(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/embed/dashboard/../../admin/users", nil)
	req.Header.Set(GrantHeader, grant)
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if p.reached {
		t.Fatal("a traversal reached an admin route under the surface's prefix")
	}
}

// The refusal has to name the fix. "My embed silently doesn't work" is the
// failure mode that produced most of this feature's bugs.
func TestReachRefusalNamesTheReachEntry(t *testing.T) {
	h := middlewareHost(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/42", nil)
	req.Header.Set(GrantHeader, grantFor(t, h))
	p := &probe{}
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"dashboard", "/api/invoices/42", `"/api/invoices"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the 403 body does not mention %s:\n%s", want, body)
		}
	}
}

func TestReachRefusesConfigurationsThatCannotBeRight(t *testing.T) {
	for _, bad := range []struct{ reach, why string }{
		{"/", "the whole app"},
		{"/mcp", "a framework-mounted route"},
		{"/admin", "the back office"},
		{"/auth/tokens", "token minting"},
		{"/admin/reports", "beneath a framework-mounted route"},
		{"relative", "not absolute"},
	} {
		_, err := New(Config{
			Surfaces: []Surface{{
				Name: "dashboard", Screen: testScreen{"/embed/dashboard"},
				Origins: []string{"https://acme.com"},
				Reach:   []string{bad.reach},
			}},
			BurnStore: NewMemoryBurnStore(),
		})
		if err == nil {
			t.Errorf("Reach %q (%s) was accepted at boot", bad.reach, bad.why)
		}
	}

	// A legitimate one still boots.
	if _, err := New(Config{
		Surfaces: []Surface{{
			Name: "dashboard", Screen: testScreen{"/embed/dashboard"},
			Origins: []string{"https://acme.com"},
			Reach:   []string{"/api/orders"},
		}},
		BurnStore: NewMemoryBurnStore(),
	}); err != nil {
		t.Errorf("a legitimate Reach was refused: %v", err)
	}
}
