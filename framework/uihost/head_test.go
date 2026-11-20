package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Head injection test components
// ---------------------------------------------------------------------------

// seoTestComp is a component that also implements SEOScreen.
type seoTestComp struct {
	headHTML string
}

func (c *seoTestComp) Render() render.HTML {
	return render.Text("SEO page")
}

func (c *seoTestComp) HeadHTML() string {
	return c.headHTML
}

// ---------------------------------------------------------------------------
// A. WithHeadHTML escape hatch
// ---------------------------------------------------------------------------

func TestWithHeadHTML(t *testing.T) {
	page := serveHomeWithOpts(t, "HeadTest",
		WithHeadHTML(`<link rel="icon" href="/static/favicon.ico"><meta name="theme-color" content="#f7f5ee">`),
	)

	assertContains(t, page, `<link rel="icon" href="/static/favicon.ico">`)
	assertContains(t, page, `<meta name="theme-color" content="#f7f5ee">`)

	// Head content must be inside <head>, not <body>
	headClose := strings.Index(page, "</head>")
	bodyOpen := strings.Index(page, "<body")
	iconAt := strings.Index(page, `<link rel="icon"`)
	if iconAt >= headClose || iconAt >= bodyOpen {
		t.Errorf("head HTML must appear before </head> and <body>; iconAt=%d headClose=%d bodyOpen=%d",
			iconAt, headClose, bodyOpen)
	}
}

func TestWithHeadHTMLNotInjectedByDefault(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	page := rec.Body.String()

	assertNotContains(t, page, `<link rel="icon"`)
	assertNotContains(t, page, `theme-color`)
}

// ---------------------------------------------------------------------------
// B. Typed convenience methods
// ---------------------------------------------------------------------------

func TestWithFavicon(t *testing.T) {
	page := serveHomeWithOpts(t, "FaviconTest", WithFavicon("/static/favicon.ico"))
	assertContains(t, page, `<link rel="icon" href="/static/favicon.ico">`)
}

func TestWithThemeColor(t *testing.T) {
	page := serveHomeWithOpts(t, "ThemeColorTest", WithThemeColor("#f7f5ee"))
	assertContains(t, page, `<meta name="theme-color" content="#f7f5ee">`)
}

func TestWithDescription(t *testing.T) {
	page := serveHomeWithOpts(t, "DescTest", WithDescription("A test site for head injection"))
	assertContains(t, page, `<meta name="description" content="A test site for head injection">`)
}

func TestWithOpenGraph(t *testing.T) {
	page := serveHomeWithOpts(t, "OGTest", WithOpenGraph(OG{
		Title:       "My Site",
		Description: "Best site ever",
		Image:       "https://example.com/og.png",
		URL:         "https://example.com",
		Type:        "website",
	}))
	assertContains(t, page, `<meta property="og:title" content="My Site">`)
	assertContains(t, page, `<meta property="og:description" content="Best site ever">`)
	assertContains(t, page, `<meta property="og:image" content="https://example.com/og.png">`)
	assertContains(t, page, `<meta property="og:url" content="https://example.com">`)
	assertContains(t, page, `<meta property="og:type" content="website">`)
}

func TestWithTwitterCard(t *testing.T) {
	page := serveHomeWithOpts(t, "TwitterTest", WithTwitterCard(TwitterCard{
		Card:        "summary_large_image",
		Title:       "My Site",
		Description: "Best site",
		Image:       "https://example.com/card.png",
	}))
	assertContains(t, page, `<meta name="twitter:card" content="summary_large_image">`)
	assertContains(t, page, `<meta name="twitter:title" content="My Site">`)
	assertContains(t, page, `<meta name="twitter:description" content="Best site">`)
	assertContains(t, page, `<meta name="twitter:image" content="https://example.com/card.png">`)
}

func TestWithCanonicalURL(t *testing.T) {
	page := serveHomeWithOpts(t, "CanonicalTest", WithCanonicalURL("https://example.com/page"))
	assertContains(t, page, `<link rel="canonical" href="https://example.com/page">`)
}

func TestWithPreconnect(t *testing.T) {
	page := serveHomeWithOpts(t, "PreconnectTest", WithPreconnect("https://fonts.googleapis.com", "https://fonts.gstatic.com"))
	assertContains(t, page, `<link rel="preconnect" href="https://fonts.googleapis.com">`)
	assertContains(t, page, `<link rel="preconnect" href="https://fonts.gstatic.com">`)
}

// ---------------------------------------------------------------------------
// C. Per-screen SEOScreen interface
// ---------------------------------------------------------------------------

func TestSEOScreenOverride(t *testing.T) {
	application := app.NewApp("SEOTest")
	application.SetDefaultLayout(bareLayout("main"))

	seoComp := &seoTestComp{
		headHTML: `<meta name="description" content="Per-screen SEO"><meta property="og:title" content="Custom OG Title">`,
	}
	application.RegisterScreen(app.NewScreen("/about", seoComp).WithTitle("About"), nil)

	host := New(application)

	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/about", nil))
	page := rec.Body.String()

	assertContains(t, page, `<meta name="description" content="Per-screen SEO">`)
	assertContains(t, page, `<meta property="og:title" content="Custom OG Title">`)
}

func TestSEOScreenPlusGlobalHead(t *testing.T) {
	// Both global (WithHeadHTML) and per-screen head should appear
	application := app.NewApp("CombinedTest")
	application.SetDefaultLayout(bareLayout("main"))

	seoComp := &seoTestComp{
		headHTML: `<meta property="og:title" content="Screen OG">`,
	}
	application.RegisterScreen(app.NewScreen("/about", seoComp).WithTitle("About"), nil)
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)

	host := New(application,
		WithFavicon("/favicon.ico"),
	)

	// About page: global favicon + per-screen OG
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/about", nil))
	page := rec.Body.String()

	assertContains(t, page, `<link rel="icon" href="/favicon.ico">`)          // global
	assertContains(t, page, `<meta property="og:title" content="Screen OG">`) // per-screen

	// Home page: global favicon, NO per-screen OG
	rec2 := httptest.NewRecorder()
	host.ServeHTTP(rec2, httptest.NewRequest("GET", "/", nil))
	page2 := rec2.Body.String()

	assertContains(t, page2, `<link rel="icon" href="/favicon.ico">`)
	assertNotContains(t, page2, `Screen OG`)
}

func TestSEOScreenNoOverride(t *testing.T) {
	// Screen that doesn't implement SEOScreen should work fine
	page := serveHomeWithOpts(t, "NoSEOTest", WithFavicon("/favicon.ico"))

	assertContains(t, page, `<link rel="icon" href="/favicon.ico">`)
	// No crash, no extra head content
	assertNotContains(t, page, `og:title`)
}

// serveHomeWithOpts builds the standard one-home-screen app, hosts it with
// opts, serves "/", and returns the rendered page — the scaffold the
// head-option tests share.
func serveHomeWithOpts(t *testing.T, name string, opts ...Option) string {
	t.Helper()
	return serveHomeWithComp(t, name, &testHomeComp{}, opts...)
}

// serveHomeWithComp is serveHomeWithOpts for a screen that renders comp
// instead of the stock home component — the scaffold the head-scrub and
// SEO-bundle tests build their per-payload screens from.
func serveHomeWithComp(t *testing.T, name string, comp component.Component, opts ...Option) string {
	t.Helper()
	application := app.NewApp(name)
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", comp).WithTitle("Home"), nil)
	host := New(application, opts...)
	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec.Body.String()
}

// ---------------------------------------------------------------------------
// D. All typed helpers combined
// ---------------------------------------------------------------------------

func TestAllTypedHelpersCombined(t *testing.T) {
	page := serveHomeWithOpts(t, "AllHelpers",
		WithFavicon("/favicon.ico"),
		WithThemeColor("#0f0"),
		WithDescription("All helpers test"),
		WithOpenGraph(OG{Title: "OG Title", URL: "https://example.com"}),
		WithTwitterCard(TwitterCard{Card: "summary", Title: "Twitter Title"}),
		WithCanonicalURL("https://example.com/"),
		WithPreconnect("https://cdn.example.com"),
	)
	// All tags should be present in <head>
	for _, want := range []string{
		`<link rel="icon" href="/favicon.ico">`,
		`<meta name="theme-color" content="#0f0">`,
		`<meta name="description" content="All helpers test">`,
		`<meta property="og:title" content="OG Title">`,
		`<meta name="twitter:card" content="summary">`,
		`<link rel="canonical" href="https://example.com/">`,
		`<link rel="preconnect" href="https://cdn.example.com">`,
	} {
		assertContains(t, page, want)
	}

	// Everything before </head>
	headClose := strings.Index(page, "</head>")
	for _, want := range []string{
		`<link rel="icon"`,
		`<meta name="theme-color"`,
		`<meta property="og:title"`,
		`<link rel="canonical"`,
	} {
		at := strings.Index(page, want)
		if at >= headClose {
			t.Errorf("%q found after </head> (at=%d, headClose=%d)", want, at, headClose)
		}
	}
}

// ---------------------------------------------------------------------------
// E. HTML escaping / injection safety
// ---------------------------------------------------------------------------

func TestTypedHelpersEscapeAttrs(t *testing.T) {
	// Ensure typed helpers escape user-provided values
	page := serveHomeWithOpts(t, "EscapeTest", WithDescription(`<script>alert("xss")</script>`))

	// The raw <script> tag must NOT appear in output. It must be escaped
	assertNotContains(t, page, `<script>alert("xss")</script>`)
	// The escaped form should be in the content attribute
	assertContains(t, page, `&lt;script&gt;`)
}

// ---------------------------------------------------------------------------
// F. Static export (RenderStaticPage) includes head tags
// ---------------------------------------------------------------------------

func TestRenderStaticPageIncludesHeadTags(t *testing.T) {
	application := app.NewApp("StaticHeadTest")
	application.SetDefaultLayout(bareLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)

	host := New(application,
		WithFavicon("/favicon.ico"),
		WithDescription("Static export head tags"),
	)

	ctx := context.Background()
	page, err := host.RenderStaticPage(ctx, "/")
	if err != nil {
		t.Fatalf("RenderStaticPage: %v", err)
	}

	assertContains(t, page, `<link rel="icon" href="/favicon.ico">`)
	assertContains(t, page, `<meta name="description" content="Static export head tags">`)
	// No SSE meta in static output
	assertNotContains(t, page, `data-sse`)
}

func TestPartialCarriesInertPageMetadata(t *testing.T) {
	application := app.NewApp("SEO")
	host := New(application)
	comp := &seoTestComp{headHTML: `<link rel="alternate" hreflang="es" href="/es"><script>alert(1)</script>`}
	application.RegisterScreen(app.NewScreen("/en", comp), nil)
	res := app.RenderResult{HTML: render.Text("Page"), Component: comp, SwapLayer: "l:site", Fills: []app.Fill{{Addr: "l:site~header", HTML: render.Text("Header")}}}
	request := httptest.NewRequest("GET", "/en", nil)
	request.Header.Set("X-Gofastr-Fills", "2")
	response := httptest.NewRecorder()
	host.writePartialResult(response, request, context.Background(), "/en", res, nil, 0)
	body := response.Body.String()
	if !strings.Contains(body, "data-cui-page-head=") || !strings.Contains(body, "data-cui-page-meta=") || strings.Contains(body, "<link ") || strings.Contains(body, "alert(1)") {
		t.Fatalf("page metadata is absent, active, or unsafe: %s", body)
	}
}
