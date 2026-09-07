//go:build red

package print

import (
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T2). Pinned family: the uihost head-emitter gate
// (isSafeHeadURL over canonical/hreflang/og/twitter URL fields,
// seo_security_test.go) and core-ui/html's setURLAttr(urlsafe.Resource)
// policy on <link>/<script src> — URL-typed head fields of a served HTML
// document pass the head-URL allow-list. renderShell is a served HTML
// document's head emitter and was never moved onto the gate.
// Property: URL-typed head fields of the print shell (stylesheet link href,
// autoprint script src) must not carry a live javascript:/vbscript:/data:
// scheme or a protocol-relative host.
// Surfaces: battery/print/shell.go::renderShell :143-145 (AppCSSHref →
// <link rel="stylesheet" href>) and :154-156 (AutoPrintSrc → <script src>) —
// render.Escape only, which is HTML-escaping and scheme-blind; the values are
// filled from host config in print.go:269-280 (cfg.BaseURL + cfg.AppCSSURL,
// cfg.PathPrefix), config-supplied like every SEO URL field.
// Finding: probed — hostile AppCSSHref and AutoPrintSrc render verbatim into
// the served print document's head today.
// Fix direction: run both through the head-URL allow-list (urlsafe.Clean with
// the Resource policy, mirroring html.Style/link emission) before
// interpolating, dropping unsafe values the way ogTags drops unsafe images.

// TestShellAssetsRedSchemeGuarded: hostile schemes in AppCSSHref/AutoPrintSrc
// must not reach the print document head.
func TestShellAssetsRedSchemeGuarded(t *testing.T) {
	attacks := []struct{ name, val string }{
		{"javascript", "javascript:alert(1)"},
		{"vbscript", "vbscript:x"},
		{"data", "data:text/html,x"},
		{"protocol-relative", "//evil.example/p"},
	}
	for _, a := range attacks {
		t.Run("css/"+a.name, func(t *testing.T) {
			out := renderShell(shellInput{Title: "t", AppCSSHref: a.val, BaseCSS: "x", PageCSS: "y"})
			if strings.Contains(out, `href="`+a.val+`"`) {
				t.Errorf("SECURITY: [print-shell-asset-scheme] stylesheet href = %q (from AppCSSHref %q): "+
					"URL-typed head fields pass the head-URL allow-list in the uihost emitter family and "+
					"setURLAttr(urlsafe.Resource) in html's link emission — renderShell escapes the value "+
					"but never checks the scheme, so a hostile config URL lands verbatim in the served "+
					"document's head", a.val, a.val)
			}
		})
		t.Run("script/"+a.name, func(t *testing.T) {
			out := renderShell(shellInput{Title: "t", BaseCSS: "x", PageCSS: "y", AutoPrintSrc: a.val})
			if strings.Contains(out, `src="`+a.val+`"`) {
				t.Errorf("SECURITY: [print-shell-asset-scheme] script src = %q (from AutoPrintSrc %q): "+
					"same head-URL allow-list family — the autoprint script src is escaped but never "+
					"scheme-checked, so a hostile value renders as a live script source in the served "+
					"document's head", a.val, a.val)
			}
		})
	}

	// Controls: the shipped values round-trip — the gate is a scheme
	// allow-list, not a blanket reject.
	out := renderShell(shellInput{
		Title:        "t",
		BaseCSS:      "x",
		PageCSS:      "y",
		AppCSSHref:   "/__gofastr/app.css",
		AutoPrintSrc: "/print/__autoprint.js",
	})
	if !strings.Contains(out, `<link rel="stylesheet" href="/__gofastr/app.css">`) {
		t.Errorf("SECURITY: [print-shell-asset-scheme] control: valid app.css link dropped:\n%s", out)
	}
	if !strings.Contains(out, `<script src="/print/__autoprint.js"></script>`) {
		t.Errorf("SECURITY: [print-shell-asset-scheme] control: valid autoprint script src dropped:\n%s", out)
	}
}
