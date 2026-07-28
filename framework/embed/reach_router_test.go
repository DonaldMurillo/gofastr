package embed

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The gate and the router must agree about which path is being requested.
//
// Every case here reached a handler before RoutedPath existed: MayReach saw the
// percent-DECODED path and cleaned it, while net/http's ServeMux matched on the
// escaped one, where %2e%2e and %2F are ordinary bytes inside a single segment.
func TestEncodedPathCannotOutrunTheRouter(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			// Cleans to "/reports" — the surface's own subtree — while the
			// router matches the subtree pattern "/api/docs/".
			name: "encoded dot segments clean into the surface subtree",
			raw:  "/api/docs/%2e%2e/%2e%2e/reports",
		},
		{
			// Decodes to "/__gofastr/private", which reads as a runtime
			// endpoint; the router sees one segment and dispatches "/{slug}".
			name: "encoded separator forges a runtime prefix",
			raw:  "/__gofastr%2Fprivate",
		},
		{
			name: "encoded dot segments climb out of the surface subtree",
			raw:  "/reports/%2e%2e/admin/users",
		},
		{
			name: "mixed-case encoding is not a bypass",
			raw:  "/api/docs/%2E%2E/%2e%2e/reports",
		},
		{
			name: "encoded backslash",
			raw:  `/__gofastr%5Cprivate`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.ParseRequestURI(tc.raw)
			if err != nil {
				t.Fatalf("ParseRequestURI(%q): %v", tc.raw, err)
			}
			if got, ok := RoutedPath(u); ok {
				t.Fatalf("RoutedPath(%q) = (%q, true), want refused — "+
					"URL.Path=%q EscapedPath=%q", tc.raw, got, u.Path, u.EscapedPath())
			}
		})
	}
}

// Ordinary escapes must still work. A refusal that also refuses legitimate
// paths would be traded for an outage.
func TestRoutedPathKeepsOrdinaryEscapes(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"/reports/quarterly%20review", "/reports/quarterly review"},
		{"/reports/a%2Bb", "/reports/a+b"},
		{"/reports/caf%C3%A9", "/reports/café"},
		{"/reports/", "/reports/"},
		{"/reports", "/reports"},
	}
	for _, tc := range cases {
		u, err := url.ParseRequestURI(tc.raw)
		if err != nil {
			t.Fatalf("ParseRequestURI(%q): %v", tc.raw, err)
		}
		got, ok := RoutedPath(u)
		if !ok {
			t.Fatalf("RoutedPath(%q) refused a legitimate path", tc.raw)
		}
		if got != tc.want {
			t.Fatalf("RoutedPath(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// End to end through the middleware: a non-canonical path is refused before any
// authorization decision, and the handler never runs.
func TestMiddlewareRefusesNonCanonicalPaths(t *testing.T) {
	h := middlewareHost(t)
	grant := grantFor(t, h)

	p := &probe{}
	req := httptest.NewRequest(http.MethodGet, "/api/docs/%2e%2e/%2e%2e/embed/dashboard", nil)
	req.Header.Set(GrantHeader, grant)
	rec := httptest.NewRecorder()
	h.Middleware()(p.handler()).ServeHTTP(rec, req)

	if p.reached {
		t.Fatalf("handler ran for %q (URL.Path=%q) — the reach gate was bypassed",
			req.URL.EscapedPath(), req.URL.Path)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// Surface.Path gets the same reserved-prefix validation as Reach. Validating
// only Reach meant Reach: []string{"/auth"} failed at boot with a clear error
// while Path: "/auth" booted clean and handed every grant the auth battery.
func TestSurfacePathIsValidatedLikeReach(t *testing.T) {
	for _, p := range []string{"/auth", "/admin", "/mcp", "/print", "/api/docs", "/"} {
		_, err := New(Config{
			Surfaces:  []Surface{{Name: "s", Screen: testScreen{p}, Origins: []string{"https://acme.example"}}},
			BurnStore: NewMemoryBurnStore(),
		})
		if err == nil {
			t.Fatalf("New accepted Surface.Path = %q — a grant would reach every route beneath it", p)
		}
		if !strings.Contains(err.Error(), "Path") {
			t.Fatalf("New(%q) error = %q, want it to name the Path field", p, err)
		}
	}
}

// A trailing slash used to disable the surface's own subtree: "/reports/"
// matched neither "/reports" nor "/reports/rows", so the surface rendered and
// then 403'd every one of its own islands.
func TestSurfacePathTrailingSlashStillOwnsItsSubtree(t *testing.T) {
	h, err := New(Config{
		Surfaces:  []Surface{{Name: "reports", Screen: testScreen{"/reports/"}, Origins: []string{"https://acme.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s, ok := h.Lookup("reports")
	if !ok {
		t.Fatal("surface not found")
	}
	for _, p := range []string{"/reports", "/reports/", "/reports/rows"} {
		if !s.MayReach(p) {
			t.Fatalf("MayReach(%q) = false for a surface declared at %q", p, "/reports/")
		}
	}
}

// A battery that moved its prefix takes its protection with it.
func TestAddReservedPrefixesRefusesASurfaceOverAMountedBattery(t *testing.T) {
	h, err := New(Config{
		Surfaces:  []Surface{{Name: "s", Screen: testScreen{"/back-office"}, Origins: []string{"https://acme.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Boots fine: "/back-office" is not a default reserved prefix.
	if err := h.AddReservedPrefixes("/back-office"); err == nil {
		t.Fatal("AddReservedPrefixes accepted a prefix a surface already sits on")
	}
	// And an unrelated prefix is accepted.
	if err := h.AddReservedPrefixes("/somewhere-else"); err != nil {
		t.Fatalf("AddReservedPrefixes(unrelated) = %v, want nil", err)
	}
}
