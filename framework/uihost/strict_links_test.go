package uihost

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// footerLink mirrors ui.SiteFooterLink's shape. The test builds the
// markup raw instead of importing framework/ui: that package's init
// registers LoadAlways component styles into the process-global
// registry, which changes every page's CSS link set and breaks the
// registry CSS tests in this package. Raw anchor markup is the same
// href surface the check reads, and the convention every other
// uihost test already follows.
type footerLink struct {
	Label string
	Href  string
}

// chromeFooter stands in for the blueprint generator's marketing footer
// (ui.SiteFooter with a Legal column) — the live instance this check
// exists for: footer link columns declared as data at app-wiring time.
//
// It is raw anchor markup, not a real ui.SiteFooter, and nothing here
// enforces that the two agree: importing framework/ui into this package
// registers ui-button/ui-page-header/ui-sidebar as LoadAlways in the
// process-global style registry, which breaks TestComponentCSS_* in the
// same package (proven by a three-way isolation run). So this pins the
// CHECK, not the component. Fidelity to the generator's real output is
// pinned where framework/ui is importable, by the marketing-link test in
// cmd/gofastr. If SiteFooter ever stops emitting plain href anchors,
// that test is what notices, not this one.
func chromeFooter(links ...footerLink) component.Component {
	var b strings.Builder
	b.WriteString(`<div class="ui-site-footer"><div class="ui-site-footer__grid"><div class="ui-site-footer__col">`)
	b.WriteString(`<p class="ui-site-footer__col-title">Legal</p><ul>`)
	for _, l := range links {
		fmt.Fprintf(&b, `<li><a href=%q>%s</a></li>`, l.Href, l.Label)
	}
	b.WriteString(`</ul></div></div></div>`)
	return app.NewStaticComponent(render.HTML(b.String()))
}

// linkHost builds a strict host whose default layout carries the given
// chrome components, plus one well-formed screen so only the link
// findings can fail the boot.
func linkHost(t *testing.T, mkOpts func() []Option, chrome ...component.Component) *UIHost {
	t.Helper()
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	layout := app.NewLayout("marketing")
	for _, c := range chrome {
		layout = layout.WithFooter(c)
	}
	a.SetDefaultLayout(layout)
	opts := mkOpts()
	return New(a, opts...)
}

// The vacuity test: the check must fire on the real generator shape —
// a ui.SiteFooter whose Legal column links /terms and /privacy while
// no route, file, or endpoint serves either.
func TestStrictFlagsChromeLinkToUnregisteredPath(t *testing.T) {
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Terms", Href: "/terms"},
		footerLink{Label: "Privacy", Href: "/privacy"},
	))
	msg := mountPanic(t, ds)
	if msg == "" {
		t.Fatal("footer links to unregistered /terms and /privacy did not fail strict boot")
	}
	for _, want := range []string{"/terms", "/privacy", `layout "marketing" footer`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic missing %q:\n%s", want, msg)
		}
	}
}

func TestStrictChromeLinkToRegisteredPathPasses(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/terms", &describedScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Terms", Href: "/terms"},
	)))
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("link to registered /terms flagged:\n%s", msg)
	}
}

// One probe per exclusion class: every href below must be left alone
// even though nothing serves its target text.
func TestStrictChromeLinkExclusions(t *testing.T) {
	for _, tc := range []struct {
		name string
		href string
	}{
		{"external https", "https://example.com/pricing"},
		{"external http", "http://example.com"},
		{"mailto", "mailto:support@example.com"},
		{"protocol-relative", "//cdn.example.com/terms"},
		{"pure fragment", "#top"},
		{"query-only", "?tab=legal"},
		{"host infra endpoint", "/__gofastr/pwa/offline"},
		{"brace placeholder", "/docs/{slug}"},
		{"colon placeholder", "/users/:id"},
		{"wildcard placeholder", "/files/:path*"},
		{"angle placeholder", "/report/<year>"},
		{"relative reference", "terms"},
		{"dot-relative reference", "./terms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := linkHost(t, strictSiteOptions, chromeFooter(
				footerLink{Label: "X", Href: tc.href},
			))
			if msg := mountPanic(t, ds); msg != "" {
				t.Fatalf("excluded href %q flagged:\n%s", tc.href, msg)
			}
		})
	}
}

// "path plus fragment"/"path plus query" above only pass because the
// PATH underneath resolves; prove the stripping itself is what carries
// them by checking a path that resolves solely via its stripped form.
func TestStrictChromeLinkStripsFragmentAndQueryBeforeResolving(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/terms", &describedScreen{}, nil)
	// /terms#x and /terms?y must resolve to the /terms screen; a href
	// to /nope#x must still be a finding.
	a.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Terms", Href: "/terms#privacy-policy"},
		footerLink{Label: "Terms", Href: "/terms?lang=de"},
	)))
	if msg := mountPanic(t, New(a, strictSiteOptions()...)); msg != "" {
		t.Fatalf("fragment/query-stripped path not resolved:\n%s", msg)
	}
}

func TestStrictChromeLinkResolvesDynamicRoute(t *testing.T) {
	// A concrete href into a parameterised route counts as served:
	// matching goes through Router.Resolve, not string equality.
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/docs/:slug", &staticPathsScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Docs", Href: "/docs/install"},
	)))
	if msg := mountPanic(t, New(a, strictSiteOptions()...)); msg != "" {
		t.Fatalf("concrete href into /docs/:slug flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToStaticFilePasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "brochure.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := func() []Option {
		return append(strictSiteOptions(), WithStaticDir(dir))
	}
	ds := linkHost(t, opts, chromeFooter())
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("link to existing static file flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToCoreRouterGETRoutePasses(t *testing.T) {
	// Entity CRUD and custom endpoints register on the framework router
	// before Mount; a chrome link to one (an "Export CSV" sidebar entry)
	// must not be a finding.
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Export", Href: "/export.csv"},
	))
	r := router.New()
	r.GetFunc("/export.csv", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	var msg string
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				msg = rec.(string)
			}
		}()
		ds.Mount(r)
	}()
	if msg != "" {
		t.Fatalf("link to core-router GET route flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToConfiguredArtifactsPass(t *testing.T) {
	opts := func() []Option {
		return append(strictSiteOptions(),
			WithPWA(PWAConfig{}),
			WithAgentReady(AgentReadyConfig{Title: "Demo", Summary: "A demo app."}),
		)
	}
	ds := linkHost(t, opts, chromeFooter(
		footerLink{Label: "Site map", Href: "/sitemap.xml"},
		footerLink{Label: "Robots", Href: "/robots.txt"},
		footerLink{Label: "Install", Href: "/manifest.webmanifest"},
		footerLink{Label: "LLM index", Href: "/llms.txt"},
	))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("links to configured artifact endpoints flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToUnconfiguredArtifactIsFlagged(t *testing.T) {
	// The artifact exemption is config-gated: /sitemap.xml without
	// WithSitemap is a genuinely dead link, not an exemption.
	ds := linkHost(t, func() []Option {
		return []Option{
			WithStrict(),
			WithDescription("A demo app."),
			WithFavicon("/static/favicon.svg"),
			// no WithSitemap, no WithRobots: both demoted below so the
			// ONLY enforce-level findings can be the link ones.
			WithStrict(StrictConfig{Sitemap: StrictWarn, Robots: StrictWarn}),
		}
	}, chromeFooter(
		footerLink{Label: "Site map", Href: "/sitemap.xml"},
	))
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "/sitemap.xml") {
		t.Fatalf("link to unconfigured /sitemap.xml not flagged:\n%s", msg)
	}
}

func TestStrictInternalLinksRespectsLevel(t *testing.T) {
	newHost := func(level StrictLevel) *UIHost {
		return linkHost(t, func() []Option {
			return []Option{
				WithStrict(StrictConfig{InternalLinks: level}),
				WithDescription("A demo app."),
				WithFavicon("/static/favicon.svg"),
				WithSitemap(SitemapConfig{BaseURL: "https://example.com"}),
				WithRobots(RobotsConfig{}),
			}
		}, chromeFooter(footerLink{Label: "Terms", Href: "/terms"}))
	}

	t.Run("warn logs and serves", func(t *testing.T) {
		buf := captureLog(t)
		if msg := mountPanic(t, newHost(StrictWarn)); msg != "" {
			t.Fatalf("warn-level link check must serve, not panic:\n%s", msg)
		}
		if !strings.Contains(buf.String(), "/terms") || !strings.Contains(buf.String(), "check=internal-links") {
			t.Fatalf("warn log missing the finding; log was:\n%s", buf.String())
		}
	})

	t.Run("off is silent", func(t *testing.T) {
		buf := captureLog(t)
		if msg := mountPanic(t, newHost(StrictOff)); msg != "" {
			t.Fatalf("off-level link check must not panic:\n%s", msg)
		}
		if strings.Contains(buf.String(), "internal") {
			t.Fatalf("off-level link check must not log; log was:\n%s", buf.String())
		}
	})
}

func TestStrictInternalLinksRespectsExemptScreens(t *testing.T) {
	// ExemptScreens exempts a link whose TARGET falls under an entry:
	// the same vocabulary that exempts routes exempts links into the
	// deliberately-off-bar area.
	mkOpts := func(exempt []string) func() []Option {
		return func() []Option {
			return append(strictSiteOptions(), WithStrict(StrictConfig{ExemptScreens: exempt}))
		}
	}
	with := linkHost(t, mkOpts([]string{"/machine/*"}), chromeFooter(
		footerLink{Label: "Feed", Href: "/machine/feed"},
	))
	if msg := mountPanic(t, with); msg != "" {
		t.Fatalf("exempt target still flagged:\n%s", msg)
	}
	without := linkHost(t, mkOpts(nil), chromeFooter(
		footerLink{Label: "Feed", Href: "/machine/feed"},
	))
	if msg := mountPanic(t, without); !strings.Contains(msg, "/machine/feed") {
		t.Fatalf("non-exempt broken link not flagged:\n%s", msg)
	}
}

// panickingChrome renders fine on requests but blows up on a background
// context: a header that needs the signed-in user.
type panickingChrome struct{}

func (p *panickingChrome) Render() render.HTML { panic("needs request context") }

func TestStrictChromeRenderFailureSkipsSlotAndWarns(t *testing.T) {
	buf := captureLog(t)
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").
		WithHeader(&panickingChrome{}).
		WithFooter(chromeFooter(footerLink{Label: "Terms", Href: "/terms"})))
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	// The panicking header is skipped with a warning, not a boot
	// failure; the footer's finding still fails the boot.
	if !strings.Contains(msg, "/terms") {
		t.Fatalf("footer finding lost while header panicked:\n%s", msg)
	}
	if !strings.Contains(buf.String(), "chrome render failed") {
		t.Fatalf("panicking chrome not warned about; log was:\n%s", buf.String())
	}
}

// ctxHeader is a NewContextComponent-style header: RenderCtx sees a
// context, Render does not. Its links must be checked through the
// RenderCtx path, which is what Layout.Wrap uses on every page.
func ctxHeader() component.Component {
	return app.NewContextComponent(func(ctx context.Context) render.HTML {
		return render.HTML(`<nav><a href="/dashboard">Dashboard</a></nav>`)
	})
}

func TestStrictChecksContextAwareChrome(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").WithHeader(ctxHeader()))
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "/dashboard") {
		t.Fatalf("context-aware header link not checked:\n%s", msg)
	}
}

// A layout attached to a screen (screen-group or per-screen override),
// not the default, is enumerated too.
func TestStrictChecksPerScreenLayoutChrome(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/app", &describedScreen{}, app.NewLayout("app").
		WithSidebar(app.NewStaticComponent(render.HTML(`<nav><a href="/admin/console">Console</a></nav>`))))
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "/admin/console") {
		t.Fatalf("per-screen layout chrome not checked:\n%s", msg)
	}
}

func TestInternalRoutePathClassification(t *testing.T) {
	for _, tc := range []struct {
		href string
		want string
		ok   bool
	}{
		{href: "/terms", want: "/terms", ok: true},
		{href: "/terms#frag", want: "/terms", ok: true},
		{href: "/terms?q=1", want: "/terms", ok: true},
		{href: "#frag", ok: false},
		{href: "?q=1", ok: false},
		{href: "", ok: false},
		{href: "https://example.com/x", ok: false},
		{href: "mailto:a@b.c", ok: false},
		{href: "tel:+1555", ok: false},
		{href: "//cdn.example/x", ok: false},
		{href: "terms", ok: false},
		{href: "./terms", ok: false},
		{href: "/docs/{slug}", ok: false},
		{href: "/users/:id", ok: false},
		{href: "/files/:path*", ok: false},
		{href: "/report/<year>", ok: false},
		{href: `/bad\path`, ok: false},
		{href: "/time/10:30", want: "/time/10:30", ok: true}, // colon mid-segment is not a placeholder
	} {
		got, ok := internalRoutePath(tc.href)
		if ok != tc.ok || got != tc.want {
			t.Errorf("internalRoutePath(%q) = (%q, %v), want (%q, %v)", tc.href, got, ok, tc.want, tc.ok)
		}
	}
}

func TestExtractChromeHrefs(t *testing.T) {
	html := `<div><a class="x" href="/a">A</a><a href='/b'>B</a><span data-href="/c">C</span>` +
		`<svg xlink:href="/d"></svg><a href="/a">dup</a><a href="">empty</a></div>`
	got := extractChromeHrefs(html)
	want := []string{"/a", "/b"}
	if len(got) != len(want) {
		t.Fatalf("extractChromeHrefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractChromeHrefs = %v, want %v", got, want)
		}
	}
}
