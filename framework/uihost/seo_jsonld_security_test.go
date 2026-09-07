package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/seo"
)

// Pins: Schema.org URL fields pass the same head-URL allow-list the
// canonical/og/twitter arms of the same SEO bundle use; hostile schemes
// are dropped at seo.Render and never reach the served page head.
// Pinned siblings: TestSEO_BundleURLsRejectUnsafeSchemes
// + TestSEO_TypedURLsRejectUnsafeSchemes (seo_security_test.go) enforce the
// head-URL scheme allow-list on the canonical/og/twitter arms of the very same
// SEO bundle, and llmmd_seo.go:34-37 states the principle: "An unsafe
// scheme/host suppressed in the head must not leak" — the JSON-LD Schema arm
// of the same bundle was never moved onto the gate.
// Property: every URL-typed SEO field emitted into <head> passes the head-URL
// allow-list — a javascript:/data: value in Article.URL/Image (or any Schema.org
// URL field) is the same phishing primitive the og/twitter pins reject, and the
// ld+json script is served into the live page head.
// Surfaces: core-ui/seo/seo.go::Render marshals Schema.org URL fields verbatim
// into the ld+json body; framework/uihost/uihost.go::screenHeadHTML :1591-1593
// appends seo.Render(resolved.Schema...) with no isSafeHeadURL pass, while the
// canonical/hreflang arms ten lines up are gated.
// Finding: verified end-to-end — a javascript: image is reflected into the
// served page's head through the Schema arm while the og/twitter arms of the
// same value are dropped (the divergence this test pins).
// Fix direction: run the same isSafeHeadURL gate over the Schema.org URL
// fields (in seo.Render, or over the bundle before screenHeadHTML emits it),
// dropping unsafe values the way ogTags/twitterTags do.

// jsonldDangerousURLs is the scheme-hostile subset of dangerousURLs
// (seo_security_test.go): every URL-typed Schema.org field must refuse these
// the same way the og/twitter arms of the same bundle already do. Markers are
// chosen to survive JSON escaping (< > & become \u003c sequences).
var jsonldDangerousURLs = []struct{ name, url, marker string }{
	{"javascript", "javascript:alert(1)", "javascript:alert(1)"},
	{"data", "data:text/html,<svg/onload=1>", "data:text/html,"},
	{"file", "file:///etc/passwd", "file:///etc/passwd"},
	{"blob", "blob:https://evil.example/123", "blob:https://evil.example/123"},
	{"protocol-relative", "//evil.example/payload", "//evil.example/payload"},
}

// hostileArticle builds an Article carrying the hostile URL in both of its
// URL-typed fields.
func hostileArticle(u string) seo.Article {
	art := seo.NewArticle()
	art.Headline = "Hostile"
	art.URL = u
	art.Image = u
	return art
}

// TestSEOJSONLDSchemeDropped: hostile schemes in Schema.org URL fields must
// not reach the emitted ld+json, neither at the seo.Render level nor through
// the served page head.
func TestSEOJSONLDSchemeDropped(t *testing.T) {
	// Leg A — package-emitter level: seo.Render output.
	for _, u := range jsonldDangerousURLs {
		t.Run("render/"+u.name, func(t *testing.T) {
			out := string(seo.Render(hostileArticle(u.url)))
			if strings.Contains(out, u.marker) {
				t.Errorf("SECURITY: [seo-jsonld-scheme] seo.Render emitted hostile URL %q verbatim into the "+
					"ld+json body: %s — every URL-typed head field passes the isSafeHeadURL allow-list in the "+
					"og/twitter/canonical arms of the same bundle, the Schema.org URL fields must not be the "+
					"ungated arm", u.url, out)
			}
		})
	}

	// Leg B — end-to-end: the served page head via the ScreenSEO bundle path
	// (the CMS/per-record shape renderBundleSEOPage documents).
	for _, u := range jsonldDangerousURLs {
		t.Run("page/"+u.name, func(t *testing.T) {
			page := renderBundleSEOPage(t, SEO{Schema: []seo.Thing{hostileArticle(u.url)}})
			if strings.Contains(page, u.marker) {
				t.Errorf("SECURITY: [seo-jsonld-scheme] hostile URL %q from the ScreenSEO bundle reached the "+
					"served page head through the JSON-LD arm while the og/twitter arms of the same value are "+
					"dropped: screenHeadHTML emits seo.Render(resolved.Schema) with no isSafeHeadURL pass",
					u.url)
			}
		})
	}

	// Contrast: the same value in the og:image arm of the very same bundle is
	// already suppressed (family divergence, pinned by the sibling test).
	page := renderBundleSEOPage(t, SEO{OG: &OG{Image: "javascript:alert(1)"}})
	if strings.Contains(page, "javascript:alert(1)") {
		t.Errorf("SECURITY: [seo-jsonld-scheme] contrast: og:image arm regressed — javascript: value survived")
	}

	// Positive control: a safe https image still flows through the JSON-LD arm.
	safe := seo.NewArticle()
	safe.Headline = "Safe"
	safe.Image = "https://example.com/hero.png"
	page = renderBundleSEOPage(t, SEO{Schema: []seo.Thing{safe}})
	if !strings.Contains(page, "https://example.com/hero.png") {
		t.Errorf("SECURITY: [seo-jsonld-scheme] control: safe https image dropped from the JSON-LD output — "+
			"the gate is a scheme allow-list, not a blanket reject: %s", page)
	}
}
