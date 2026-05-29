package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type headHTMLComp struct{ head string }

func (c *headHTMLComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("SEO")) }
func (c *headHTMLComp) HeadHTML() string    { return c.head }

func renderHeadPage(t *testing.T, opts ...Option) string {
	t.Helper()
	application := app.NewApp("HeadSec")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	host := New(application, opts...)
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec.Body.String()
}

func renderScreenHeadPage(t *testing.T, head string) string {
	t.Helper()
	application := app.NewApp("ScreenHeadSec")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &headHTMLComp{head: head}).WithTitle("Home"), nil)
	host := New(application)
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec.Body.String()
}

// dangerousHead is the canonical block-list for caller-supplied head
// HTML. The previous contract treated WithHeadHTML / SEOScreen.HeadHTML
// as a near-unbounded escape hatch (only <script> was stripped); the
// new contract scrubs the broader family of "active in head" tags so
// CMS-supplied metadata can't ship a meta-refresh redirect, an iframe
// embed, or an inline <style>.
var dangerousHeadTags = []struct {
	name, payload, forbidden string
}{
	{"meta-refresh", `<meta http-equiv="refresh" content="0;url=https://evil.example">`, `http-equiv="refresh"`},
	{"iframe", `<iframe src="https://evil.example/frame"></iframe>`, `<iframe`},
	{"object", `<object data="https://evil.example/p.swf"></object>`, `<object`},
	{"embed", `<embed src="https://evil.example/p.swf">`, `<embed`},
	{"base-js", `<base href="javascript:alert(1)">`, `<base`},
	{"link-modulepreload-js", `<link rel="modulepreload" href="javascript:alert(1)">`, `javascript:`},
	{"link-preload-script", `<link rel="preload" as="script" href="https://evil.example/x.js">`, `rel="preload"`},
	{"style", `<style>body{display:none}</style>`, `<style>`},
	{"svg", `<svg><circle></circle></svg>`, `<svg>`},
	{"audio", `<audio src="https://evil.example/a.mp3" autoplay></audio>`, `<audio`},
	{"form", `<form action="https://evil.example/submit"></form>`, `<form`},
	{"img", `<img src="https://evil.example/p.png">`, `<img`},
	{"marquee", `<marquee>x</marquee>`, `<marquee`},
	// Unclosed openers: the browser's lenient parser still creates the
	// element and fires the handler. A regex that requires a closing tag
	// (or self-close) leaves these untouched.
	{"svg-unclosed", `<svg onload=alert(document.cookie)>`, `<svg`},
	{"iframe-unclosed", `<iframe src=x onload=alert(1)>`, `<iframe`},
	{"details-unclosed", `<details ontoggle=alert(1)>`, `<details`},
	{"video-unclosed", `<video onloadstart=alert(1) src=x>`, `<video`},
}

func TestWithHeadHTML_StripsDangerousTags(t *testing.T) {
	for _, tc := range dangerousHeadTags {
		t.Run(tc.name, func(t *testing.T) {
			page := renderHeadPage(t, WithHeadHTML(tc.payload))
			if strings.Contains(page, tc.forbidden) {
				t.Fatalf("WithHeadHTML kept %q (forbidden=%q)", tc.payload, tc.forbidden)
			}
		})
	}
}

func TestSEOScreen_StripsDangerousTags(t *testing.T) {
	for _, tc := range dangerousHeadTags {
		t.Run(tc.name, func(t *testing.T) {
			page := renderScreenHeadPage(t, tc.payload)
			if strings.Contains(page, tc.forbidden) {
				t.Fatalf("SEOScreen.HeadHTML kept %q (forbidden=%q)", tc.payload, tc.forbidden)
			}
		})
	}
}

// formControlXSS covers interactive form controls that browsers hoist
// out of <head> into <body> and then fire an event handler on via
// autofocus. They are NOT in dangerousHeadTagsRe / voidHeadTagsRe, so
// the only thing that neutralises them is a generic on*= / autofocus
// attribute strip. The property: no surviving head tag may carry an
// event-handler attribute or autofocus.
var formControlXSS = []struct {
	name, payload string
}{
	{"input", `<input type=hidden onfocus=alert(1) autofocus>`},
	{"select", `<select autofocus onfocus=alert(document.cookie)></select>`},
	{"textarea", `<textarea autofocus oninput=alert(1)></textarea>`},
	{"keygen", `<keygen autofocus onfocus=alert(1)>`},
	// HTML5 allows '/' as an attribute separator (no whitespace). A
	// whitespace-only attribute scrub misses on*/autofocus tucked behind
	// a slash, leaving the handler live on a body-hoisted form control.
	{"slash-sep", `<input/onfocus=alert(1)/autofocus>`},
}

func TestHeadHTML_NeutralisesAutofocusXSS(t *testing.T) {
	for _, tc := range formControlXSS {
		t.Run(tc.name, func(t *testing.T) {
			for _, render := range map[string]func() string{
				"WithHeadHTML": func() string { return renderHeadPage(t, WithHeadHTML(tc.payload)) },
				"SEOScreen":    func() string { return renderScreenHeadPage(t, tc.payload) },
			} {
				low := strings.ToLower(render())
				if strings.Contains(low, "onfocus") || strings.Contains(low, "oninput") {
					t.Fatalf("event handler survived head scrub: %q", tc.payload)
				}
				if strings.Contains(low, "autofocus") {
					t.Fatalf("autofocus survived head scrub: %q", tc.payload)
				}
			}
		})
	}
}

// dangerousURLs are scheme/escape combinations that have no business in
// a typed SEO URL field. The typed helpers (WithCanonicalURL,
// WithOpenGraph URL/Image, WithTwitterCard Image) flow into rendered
// meta tags — a `javascript:` / `data:` value there is a phishing
// primitive once any consumer (preview crawler, share card) follows it.
var dangerousURLs = []string{
	"javascript:alert(1)",
	"data:text/html,<svg/onload=1>",
	"file:///etc/passwd",
	"blob:https://evil.example/123",
	"//evil.example/payload",
	"https://example.com/%0d%0aX-Test:1",
}

func TestSEO_TypedURLsRejectUnsafeSchemes(t *testing.T) {
	checks := map[string]func(string) Option{
		"canonical":     func(u string) Option { return WithCanonicalURL(u) },
		"og-image":      func(u string) Option { return WithOpenGraph(OG{Image: u}) },
		"og-url":        func(u string) Option { return WithOpenGraph(OG{URL: u}) },
		"twitter-image": func(u string) Option { return WithTwitterCard(TwitterCard{Image: u}) },
	}
	for label, opt := range checks {
		for _, u := range dangerousURLs {
			t.Run(label+"/"+u, func(t *testing.T) {
				page := renderHeadPage(t, opt(u))
				if strings.Contains(strings.ToLower(page), strings.ToLower(u)) {
					t.Fatalf("%s SEO URL %q reflected into page", label, u)
				}
			})
		}
	}
}

// seoBundleComp is a screen that returns a per-page SEO bundle from
// dynamic (e.g. CMS / per-record) data — the path that bypasses the
// sitewide WithCanonicalURL/WithOpenGraph allow-list.
type seoBundleComp struct{ seo SEO }

func (c *seoBundleComp) Render() render.HTML { return html.Div(html.DivConfig{}, render.Text("SEO")) }
func (c *seoBundleComp) ScreenSEO() SEO      { return c.seo }

func renderBundleSEOPage(t *testing.T, s SEO) string {
	t.Helper()
	application := app.NewApp("BundleSEOSec")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &seoBundleComp{seo: s}).WithTitle("Home"), nil)
	host := New(application)
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec.Body.String()
}

// Per-page typed SEO URL fields (the ScreenSEO bundle path) must enforce
// the same scheme allow-list as the sitewide helpers — they flow into
// the same crawler-followed meta/link tags.
func TestSEO_BundleURLsRejectUnsafeSchemes(t *testing.T) {
	checks := map[string]func(string) SEO{
		"canonical": func(u string) SEO { return SEO{Canonical: u} },
		"og-image":  func(u string) SEO { return SEO{OG: &OG{Image: u}} },
		"og-url":    func(u string) SEO { return SEO{OG: &OG{URL: u}} },
		"tw-image":  func(u string) SEO { return SEO{Twitter: &TwitterCard{Image: u}} },
	}
	for label, mk := range checks {
		for _, u := range dangerousURLs {
			t.Run(label+"/"+u, func(t *testing.T) {
				page := renderBundleSEOPage(t, mk(u))
				if strings.Contains(strings.ToLower(page), strings.ToLower(u)) {
					t.Fatalf("%s bundle SEO URL %q reflected into page", label, u)
				}
			})
		}
	}
}

// Sanity: a safe URL still flows through the bundle path.
func TestSEO_BundleURLsAcceptHTTPS(t *testing.T) {
	page := renderBundleSEOPage(t, SEO{Canonical: "https://example.com/about"})
	if !strings.Contains(page, `href="https://example.com/about"`) {
		t.Fatalf("safe bundle canonical URL dropped: page=%s", page)
	}
}

// Sanity: a safe URL still flows through the typed helpers.
func TestSEO_TypedURLsAcceptHTTPS(t *testing.T) {
	page := renderHeadPage(t, WithCanonicalURL("https://example.com/about"))
	if !strings.Contains(page, `href="https://example.com/about"`) {
		t.Fatalf("safe canonical URL dropped: page=%s", page)
	}
}
