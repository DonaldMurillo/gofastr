package print

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestSecPerUserDocNotPublic asserts the CLAUDE.md rule-#6 property: a
// document with the default access policy is NOT world-readable.
func TestSecPerUserDocNotPublic(t *testing.T) {
	b := New(Config{}).Document(Document{ // no Access, no DefaultAccess override
		Name: "invoice", Path: "/invoice/{id}", Build: docBuild("secret"),
	})
	rec := get(t, mount(t, b), "/print/invoice/42")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read = %d, want 401 (per-user doc must not be public)", rec.Code)
	}
}

// TestSecComponentEscapedBodySafe asserts the documented safe pattern: a
// component that runs untrusted data through render.Text stays safe all
// the way through the shell (the shell doesn't double-unescape it).
func TestSecComponentEscapedBodySafe(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "echo", Path: "/echo/{id}",
		Build: func(r *http.Request) (component.Component, error) {
			return stubDoc{html: render.Text(router.Param(r, "id"))}, nil
		},
	})
	body := get(t, mount(t, b), "/print/echo/%3Cscript%3Ealert(1)%3C%2Fscript%3E").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("unescaped script reached the page: %q", body)
	}
}

// TestSecRawBodyIsTrustBoundary pins the battery's actual contract: it
// writes the component's render.HTML VERBATIM (no escaping). Escaping the
// body is the component's job — unlike Title, which the battery escapes
// (see TestTitleEscaped). This documents the trust boundary so a host
// doesn't assume the battery will sanitize raw HTML for it.
func TestSecRawBodyIsTrustBoundary(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "raw", Path: "/raw",
		Build: func(*http.Request) (component.Component, error) {
			return stubDoc{html: render.HTML("<b data-x>raw</b>")}, nil
		},
	})
	body := get(t, mount(t, b), "/print/raw").Body.String()
	if !strings.Contains(body, "<b data-x>raw</b>") {
		t.Errorf("battery should pass render.HTML body verbatim; got %q", body)
	}
}

// TestSecNoStoreOnPerUserDoc asserts per-user documents are not cached by
// shared proxies.
func TestSecNoStoreOnPerUserDoc(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("x"),
	})
	rec := get(t, mount(t, b), "/print/doc")
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestSecCSPInlineStyleNotScript documents the intentional CSP delta:
// inline <style> is allowed (server-generated), inline <script> is NOT.
func TestSecCSPInlineStyleNotScript(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("x"),
	})
	rec := get(t, mount(t, b), "/print/doc")
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Errorf("expected style-src 'unsafe-inline', got %q", csp)
	}
	if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("script-src must stay 'self' (no unsafe-inline), got %q", csp)
	}
}
