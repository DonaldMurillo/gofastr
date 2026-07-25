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

// TestPDFBlocksInternalSubresource pins the half of the print CSP story
// the PDF path depends on.
//
// Attack: chromepdf base64s the shelled print HTML into a
// `data:text/html;base64,…` URL and navigates headless Chrome to it.
// printCSP was applied only as an HTTP *response header* on the print
// routes, and a `data:` document has no headers — so on the PDF path the
// CSP had exactly zero effect. First-party components put caller strings
// into URL attributes (ui.Avatar{Src} and friends), so a receipt PDF
// rendering a user-stored avatar URL fetches whatever it names, and the
// fetched bytes are rendered INTO the downloaded PDF: a readable exfil
// channel for 169.254.169.254, loopback admin panels, and RFC1918.
//
// The document therefore has to carry its own policy. The browser half of
// the block — Chrome refusing to resolve anything at all — is pinned by
// the chromium-tagged test in battery/print/chromepdf.
func TestPDFBlocksInternalSubresource(t *testing.T) {
	out := renderShell(shellInput{Title: "receipt", BaseCSS: "x", PageCSS: "y"})

	if !strings.Contains(out, `http-equiv="Content-Security-Policy"`) {
		t.Fatalf("SECURITY: [ssrf] the print shell emits no in-document CSP; on the PDF path the response header does not apply. Shell:\n%s", out)
	}
	// Attribute-escaped in the markup; browsers decode entities before
	// parsing the policy, so compare against the escaped form.
	if !strings.Contains(out, render.Escape(printCSP)) {
		t.Errorf("SECURITY: [ssrf] the in-document CSP differs from the route header policy — the PDF and HTML paths must not diverge. Shell:\n%s", out)
	}
	// default-src 'self' is what makes an absolute cross-origin
	// subresource unreachable. On a data: URL the document origin is
	// opaque, so 'self' matches nothing — which is the intent.
	if !strings.Contains(printCSP, "default-src 'self'") {
		t.Errorf("print CSP no longer restricts default-src: %q", printCSP)
	}
	// The meta must precede any element that can trigger a fetch,
	// otherwise the policy installs too late to gate it.
	metaIdx := strings.Index(out, "Content-Security-Policy")
	for _, tag := range []string{"<link", "<script", "<img", "<body"} {
		if i := strings.Index(out, tag); i >= 0 && i < metaIdx {
			t.Errorf("SECURITY: [ssrf] %s appears before the CSP meta — the policy installs too late to gate it", tag)
		}
	}
}
