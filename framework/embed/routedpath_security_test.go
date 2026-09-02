package embed

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Property: a request path whose segments do not survive percent-decoding
// intact is refused BEFORE any authorization decision, on every decoding
// trick that turns one router segment into several for anything that decodes
// first. TestEncodedPathCannotOutrunTheRouter pins the encoded-dot and
// encoded-authority classes; these are the remaining shapes: a RAW backslash
// inside a segment (some proxies normalise it to "/" before the app sees the
// request), a lowercase encoded separator mid-path, an encoded separator
// appended to dot segments, and the encoded backslash alone.
//
// Surfaces: RoutedPath itself, and the middleware end-to-end (the handler
// must never run — a 400 is not an authorization decision).
func TestRoutedPathRefusesSeparatorDecoding(t *testing.T) {
	refused := []string{
		`/reports/a\b`,          // raw backslash in a segment
		"/reports/a%5cb",        // encoded backslash, lowercase hex
		"/reports/row%2f42",     // encoded separator mid-path
		"/reports/..%2fadmin",   // dot segment joined by an encoded separator
		"/__gofastr/plugin%5Cx", // encoded backslash forging a runtime subtree
	}
	for _, raw := range refused {
		u, err := url.ParseRequestURI(raw)
		if err != nil {
			t.Fatalf("ParseRequestURI(%q): %v", raw, err)
		}
		if got, ok := RoutedPath(u); ok {
			t.Errorf("RoutedPath(%q) = (%q, true), want refused", raw, got)
		}
	}

	// End to end: the middleware answers 400 and the handler never runs,
	// so no reach decision was made on a path the router disagrees about.
	h := middlewareHost(t)
	grant := grantFor(t, h)
	for _, raw := range refused {
		p := &probe{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		req.Header.Set(GrantHeader, grant)
		h.Middleware()(p.handler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q through Middleware: status %d, want 400", raw, rec.Code)
		}
		if p.reached {
			t.Errorf("GET %q reached the handler despite a non-canonical path", raw)
		}
	}

	// The ordinary-escape counterparts still pass: a segment may carry any
	// byte that decodes to itself, and a double-encoded dot stays ONE
	// segment that happens to be named "%2e%2e" — compared as-is, never
	// collapsed into a traversal.
	for raw, want := range map[string]string{
		"/reports/quarterly%20review": "/reports/quarterly review",
		"/reports/%252e%252e":         "/reports/%2e%2e",
	} {
		u, err := url.ParseRequestURI(raw)
		if err != nil {
			t.Fatalf("ParseRequestURI(%q): %v", raw, err)
		}
		got, ok := RoutedPath(u)
		if !ok || got != want {
			t.Errorf("RoutedPath(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}
}

// Property: reach prefixes match on SEGMENT boundaries only — a sibling
// that shares the spelling (case, suffix, parent) is out of reach. The
// prefix-matching rule is all that separates "/api/orders" from
// "/api/orders-archive", and case-sensitivity is what keeps a granted
// subtree from absorbing the app's own differently-cased routes.
func TestMayReachBoundaryIsSegmentAligned(t *testing.T) {
	h, err := New(Config{
		Surfaces: []Surface{{
			Name:    "reports",
			Screen:  testScreen{"/reports"},
			Origins: []string{"https://acme.example"},
			Reach:   []string{"/api/orders"},
		}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s, ok := h.Lookup("reports")
	if !ok {
		t.Fatal("surface missing")
	}

	in := []string{"/reports", "/reports/42", "/reports/42/rows", "/api/orders", "/api/orders/7"}
	out := []string{
		"/reports-archive",    // suffix sibling
		"/Reports/42",         // case variant is a different route
		"/api",                // a PARENT of the reach entry is not in reach
		"/api/orders-archive", // suffix sibling of the reach entry
		"/api/ordersx",        // spelling extension
		"/report",             // spelling prefix
	}
	for _, p := range in {
		if !s.MayReach(p) {
			t.Errorf("MayReach(%q) = false, want true", p)
		}
	}
	for _, p := range out {
		if s.MayReach(p) {
			t.Errorf("MayReach(%q) = true, want false (segment boundary violated)", p)
		}
	}

	// End to end through the middleware: the suffix sibling 403s and the
	// handler never runs, with the refusal naming the Reach entry.
	mh := middlewareHost(t)
	grant := grantFor(t, mh)
	for _, p := range []string{"/api/posts-archive", "/API/posts"} {
		pr := &probe{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, p, nil)
		req.Header.Set(GrantHeader, grant)
		mh.Middleware()(pr.handler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %q: status %d, want 403", p, rec.Code)
		}
		if pr.reached {
			t.Errorf("POST %q reached the handler outside the surface's reach", p)
		}
	}
}

// Property: the CSRF exemption is exactly the documented set — the two
// credential-free handshake endpoints on any method, safe-method requests
// inside the embed surface space, and grant-bearing requests wherever they
// go (the grant verifies or the request 401s; no ambient credential exists
// for a double-submit check to protect). Nothing else: a state-changing
// request to an app route without a grant stays gated, and a case-variant of
// the surface prefix is a different path, not an exemption.
func TestCSRFExemptAdmitsOnlyDocumentedCases(t *testing.T) {
	exempt := []struct {
		name   string
		method string
		path   string
		grant  bool
	}{
		{"exchange endpoint", http.MethodPost, ExchangePath, false},
		{"refresh endpoint", http.MethodPost, RefreshPath, false},
		{"surface shell GET", http.MethodGet, "/__gofastr/embed/reports", false},
		{"surface shell HEAD", http.MethodHead, "/__gofastr/embed/reports", false},
		{"surface content OPTIONS", http.MethodOptions, "/__gofastr/embed/reports/content", false},
		{"grant-bearing app POST", http.MethodPost, "/api/invoices/42", true},
		{"grant-bearing app DELETE", http.MethodDelete, "/api/invoices/42", true},
	}
	for _, tc := range exempt {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.grant {
			req.Header.Set(GrantHeader, "gfsomegrant")
		}
		if !CSRFExempt(req) {
			t.Errorf("%s %s (%s): CSRFExempt = false, want true", tc.method, tc.path, tc.name)
		}
	}

	gated := []struct {
		name   string
		method string
		path   string
	}{
		{"app route POST, no grant", http.MethodPost, "/api/invoices/42"},
		{"app route PUT, no grant", http.MethodPut, "/api/invoices/42"},
		{"surface prefix POST, no grant", http.MethodPost, "/__gofastr/embed/reports"},
		{"case-variant prefix", http.MethodGet, "/__GOFASTR/embed/reports"},
		{"loader script POST", http.MethodPost, LoaderPath},
	}
	for _, tc := range gated {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if CSRFExempt(req) {
			t.Errorf("%s %s (%s): CSRFExempt = true, want false", tc.method, tc.path, tc.name)
		}
	}

	// A forged grant header buys the exemption but never a session: the
	// middleware refuses it before any handler runs (pinned here so the
	// exemption above cannot be read as a bypass on its own).
	h := middlewareHost(t)
	p := &probe{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/embed/dashboard/data", nil)
	req.Header.Set(GrantHeader, "forged-grant")
	h.Middleware()(p.handler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("forged grant: status %d, want 401", rec.Code)
	}
	if p.reached {
		t.Error("a forged grant header reached the handler")
	}
}
