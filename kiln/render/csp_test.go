package render_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/render"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Testimonial: documents that kiln-served page responses carry a strict CSP
// is correct; reported issue k-raw-1 (CSP prong) does not reproduce. While
// renderFullPage itself sets no CSP, every page is served through
// app.Router(), whose framework security-headers middleware emits
// "default-src 'self'; ...; base-uri 'self'" with no unsafe-inline.
//
// TestPageResponseHasStrictCSP asserts every kiln-served page carries a
// Content-Security-Policy header so the documented strict CSP is actually
// emitted. Without it, a `raw` node's unescaped HTML could execute inline
// script.
func TestPageResponseHasStrictCSP(t *testing.T) {
	app, _ := newTestApp(t)
	w := world.New()
	w.Pages["/p"] = &world.Page{
		Path: "/p", Title: "P", Type: "page",
		Tree: world.Node{Kind: "div"},
	}
	if err := render.Apply(app, w); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	res, _ := get(t, app.Router(), "/p")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	csp := res.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("page response is missing Content-Security-Policy header")
	}
	// Strict: must scope default-src/script-src to 'self' and must NOT
	// allow unsafe-inline (which would defeat the point against raw HTML).
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP missing default-src 'self': %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("CSP must not allow unsafe-inline: %q", csp)
	}
}
