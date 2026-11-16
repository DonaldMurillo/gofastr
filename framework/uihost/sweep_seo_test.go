package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestSweepSeoSitemapPartMissFallsThroughToPage(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Register("/sitemap/about", &plainComp{}, nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/sitemap/about", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "<urlset") || !strings.Contains(w.Body.String(), "hi") {
		t.Fatalf("GET /sitemap/about: status %d, want the page:\n%s", w.Code, w.Body.String())
	}
}

func TestSweepSeoSitemapSkipsRedirects(t *testing.T) {
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	a.Redirect("/old", "/")
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	if doc, _ := ds.SitemapXML(""); strings.Contains(doc, "/old") {
		t.Fatalf("the sitemap lists a redirect:\n%s", doc)
	}
}

type sweepSeoQueryKeyComp struct{ plainComp }

func (sweepSeoQueryKeyComp) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"id": "a?b"}}
}

func TestSweepSeoSitemapPolicySeesEscapedPath(t *testing.T) {
	noQuery := app.PolicyFunc(func(ctx context.Context) app.Decision {
		if r := app.RequestFromContext(ctx); r != nil && r.URL.RawQuery == "" {
			return decide.Allow()
		}
		return decide.Block(403)
	})
	a := app.NewApp("x")
	a.RegisterScreen(app.NewScreen("/orders/:id", &sweepSeoQueryKeyComp{}).WithPolicy(noQuery), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	doc, _ := ds.SitemapXML("")
	if !strings.Contains(doc, "<loc>https://x.test/orders/a%3Fb</loc>") {
		t.Fatalf("a key holding ? reached the policy as a query:\n%s", doc)
	}
}

func TestSweepSeoJoinBaseURLAddsMountOnce(t *testing.T) {
	for _, c := range [][3]string{
		{"https://x.test/", "", "https://x.test"},
		{"https://x.test", "/docs/", "https://x.test/docs"},
		{"https://x.github.io/gofastr/", "/gofastr", "https://x.github.io/gofastr"},
	} {
		if got := joinBaseURL(c[0], c[1]); got != c[2] {
			t.Errorf("joinBaseURL(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestSweepSeoRobotsBasePathOnlyMovesSitemap(t *testing.T) {
	ds := New(app.NewApp("x"),
		WithSitemap(SitemapConfig{BaseURL: "https://x.test"}),
		WithRobots(RobotsConfig{Disallow: []string{"/app"}}))
	doc, _ := ds.RobotsTXT("/docs")
	if !strings.Contains(doc, "Disallow: /app\n") || strings.Contains(doc, "/docs/app") ||
		!strings.Contains(doc, "Sitemap: https://x.test/docs/sitemap.xml\n") {
		t.Fatalf("robots.txt under /docs:\n%s", doc)
	}
}

func TestSweepSeoRobotsRootRuleNotPrefixed(t *testing.T) {
	ds := New(app.NewApp("x"), WithRobots(RobotsConfig{Disallow: []string{"/"}}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	doc, _ := ds.RobotsTXT("")
	if strings.Count(doc, "Disallow:") != 1 {
		t.Fatalf("a root rule was repeated per locale:\n%s", doc)
	}
}

func TestSweepSeoFrontMatterDropsUnsafeURLs(t *testing.T) {
	bad := "javascript:alert(1)"
	fm := screenSEOFrontMatter("T", SEO{
		Canonical: bad,
		OG:        &OG{Title: "o", Image: bad},
		Hreflangs: []HreflangLink{{Lang: "en", URL: bad}, {Lang: "fr", URL: "https://x.test/fr"}, {Lang: "de"}},
	})
	if strings.Contains(fm, "javascript") || strings.Contains(fm, `"en"`) || strings.Contains(fm, `"de"`) || !strings.Contains(fm, `- "fr"`) {
		t.Fatalf("front matter kept what the head drops:\n%s", fm)
	}
	if !strings.HasPrefix(fm, "---\ntitle: \"T\"\n") {
		t.Fatalf("front matter does not open with the title:\n%s", fm)
	}
}

func TestSweepSeoYAMLQuoteEscapesControls(t *testing.T) {
	got := yamlDoubleQuote("a\"b\\c\nd\re\tf\x01g\x7fh: #i")
	want := `"a\"b\\c\nd\re\tf\x01g\x7fh: #i"`
	if got != want {
		t.Fatalf("yamlDoubleQuote = %s, want %s", got, want)
	}
}

func TestSweepSeoDescriptionNameIsCaseAndSpaceFree(t *testing.T) {
	_, first := scanDescriptions(`<meta name=" DESCRIPTION " content="a"><meta name="description" content="b">`, 1)
	if !strings.Contains(first, `content="a"`) {
		t.Fatalf("first description = %q, want the upper-case one", first)
	}
}

func TestSweepSeoOrganizationOmitsEmptyParts(t *testing.T) {
	ds := New(app.NewApp("x"), WithOrganization(OrganizationConfig{Name: "Acme", URL: "https://acme.test"}))
	ld := ds.organizationJSONLD()
	if !strings.Contains(ld, `"Acme"`) || strings.Contains(ld, "contactPoint") || strings.Contains(ld, "address") {
		t.Fatalf("an organization with no contact or address:\n%s", ld)
	}
}

func TestSweepSeoRenderLocalesIncludeFallback(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "k", i18n.Message{Text: "v"})
	ctx := i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en"))
	var keys []string
	for _, l := range New(app.NewApp("x")).renderLocales(ctx) {
		keys = append(keys, l.key)
	}
	if strings.Join(keys, ",") != "en,fr" {
		t.Fatalf("render locales = %v, want [en fr]", keys)
	}
}

func TestSweepSeoPerLocaleWaiterRetriesAfterPanic(t *testing.T) {
	var p perLocale[int]
	started, release := make(chan struct{}), make(chan struct{})
	go func() {
		defer func() { _ = recover() }()
		p.get("k", func() int { close(started); <-release; panic("refused") })
	}()
	<-started
	got := make(chan int)
	go func() { got <- p.get("k", func() int { return 7 }) }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if v := <-got; v != 7 {
		t.Fatalf("a caller that waited on a panicking build got %d, want its own build", v)
	}
}

func TestSweepSeoAlternatesNeedBaseURL(t *testing.T) {
	ds := New(app.NewApp("x"))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	if links := ds.localeAlternates(context.Background(), "/a"); links != nil {
		t.Fatalf("alternates without a BaseURL: %v", links)
	}
}

func TestSweepSeoOGURLCarriesPrefix(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	ds := New(app.NewApp("x"), WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	ds.SetPathLocales(set)
	ctx := i18n.WithPathLocale(context.Background(), set, "es")
	declared := &OG{URL: "https://x.test/pricing"}
	s := ds.localizeSEO(ctx, "/pricing", SEO{OG: declared})
	if s.OG.URL != "https://x.test/es/pricing" || declared.URL != "https://x.test/pricing" {
		t.Fatalf("og:url = %q, declared now %q", s.OG.URL, declared.URL)
	}
}

func TestSweepSeoLocaleRenderCarriesPathLocale(t *testing.T) {
	ds := New(app.NewApp("x"))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	tr := i18n.NewTranslator(i18n.NewMapCatalog(), "en")
	if got := i18n.PathPrefix(ds.localeRenderFor(tr, "es").ctx); got != "/es" {
		t.Fatalf("es render prefix = %q, want /es", got)
	}
	if got := i18n.PathPrefix(ds.localeRenderFor(tr, "de").ctx); got != "" {
		t.Fatalf("a locale outside the set got prefix %q", got)
	}
}
