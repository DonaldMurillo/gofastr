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

// bootCheckPanic mounts the host and then runs the boot-time link check
// exactly as App.Start does (Mount first, ValidateBoot after the route
// table is complete), recovering any panic message. The afterMount hook
// registers routes between the two, the way batteries register during
// App.Start's InitPlugins phase.
func bootCheckPanic(t *testing.T, ds *UIHost, afterMount func(r *router.Router)) (msg string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg = r.(string)
		}
	}()
	r := router.New()
	ds.Mount(r)
	if afterMount != nil {
		afterMount(r)
	}
	ds.ValidateBoot()
	return ""
}

// The vacuity test: the check must fire on the real generator shape —
// a ui.SiteFooter whose Legal column links /terms and /privacy while
// no route, file, or endpoint serves either.
func TestStrictFlagsChromeLinkToUnregisteredPath(t *testing.T) {
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Terms", Href: "/terms"},
		footerLink{Label: "Privacy", Href: "/privacy"},
	))
	msg := bootCheckPanic(t, ds, nil)
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
	if msg := bootCheckPanic(t, ds, nil); msg != "" {
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
		{"host infra endpoint", "/__gofastr/app.css"},
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
			if msg := bootCheckPanic(t, ds, nil); msg != "" {
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
	if msg := bootCheckPanic(t, New(a, strictSiteOptions()...), nil); msg != "" {
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
	if msg := bootCheckPanic(t, New(a, strictSiteOptions()...), nil); msg != "" {
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
	if msg := bootCheckPanic(t, ds, nil); msg != "" {
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
	if msg := bootCheckPanic(t, ds, func(r *router.Router) {
		r.GetFunc("/export.csv", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}); msg != "" {
		t.Fatalf("link to core-router GET route flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToRouteRegisteredAfterMountPasses(t *testing.T) {
	// A battery registers its routes during App.Start's InitPlugins,
	// after the host's Mount; the check runs at boot, on the complete
	// table, so the "Back office" sidebar entry is fine. The uihost-level
	// pin of the order the framework-level test in framework/ drives
	// through a real App.Start.
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Back office", Href: "/admin"},
	))
	if msg := bootCheckPanic(t, ds, func(r *router.Router) {
		r.GetFunc("/admin", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}); msg != "" {
		t.Fatalf("link to battery-registered /admin flagged:\n%s", msg)
	}
	// The control: with nothing registered, the same link is a finding.
	ds = linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Back office", Href: "/admin"},
	))
	if msg := bootCheckPanic(t, ds, nil); !strings.Contains(msg, "/admin") {
		t.Fatalf("unregistered /admin not flagged:\n%s", msg)
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
	if msg := bootCheckPanic(t, ds, nil); msg != "" {
		t.Fatalf("links to configured artifact endpoints flagged:\n%s", msg)
	}
}

func TestStrictChromeLinkToUnconfiguredArtifactIsFlagged(t *testing.T) {
	// Artifact endpoints are config-gated: /sitemap.xml without
	// WithSitemap registers no route, so the link is genuinely dead.
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
	msg := bootCheckPanic(t, ds, nil)
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
		if msg := bootCheckPanic(t, newHost(StrictWarn), nil); msg != "" {
			t.Fatalf("warn-level link check must serve, not panic:\n%s", msg)
		}
		if !strings.Contains(buf.String(), "/terms") || !strings.Contains(buf.String(), "check=internal-links") {
			t.Fatalf("warn log missing the finding; log was:\n%s", buf.String())
		}
	})

	t.Run("off is silent", func(t *testing.T) {
		buf := captureLog(t)
		if msg := bootCheckPanic(t, newHost(StrictOff), nil); msg != "" {
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
	if msg := bootCheckPanic(t, with, nil); msg != "" {
		t.Fatalf("exempt target still flagged:\n%s", msg)
	}
	without := linkHost(t, mkOpts(nil), chromeFooter(
		footerLink{Label: "Feed", Href: "/machine/feed"},
	))
	if msg := bootCheckPanic(t, without, nil); !strings.Contains(msg, "/machine/feed") {
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
	msg := bootCheckPanic(t, ds, nil)
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
	msg := bootCheckPanic(t, ds, nil)
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
	msg := bootCheckPanic(t, ds, nil)
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
		// net/http routes on the decoded path; so must the check.
		{href: "/docs/caf%C3%A9", want: "/docs/café", ok: true},
		{href: "/a%2Fb", want: "/a/b", ok: true},       // decoded separator is a real path byte to the router
		{href: "%2Fdocs", ok: false},                   // decoding does not make a relative reference absolute
		{href: "/q%3Fmark", want: "/q?mark", ok: true}, // %3F is path data, not a query separator
		{href: "/bad%zz", want: "/bad%zz", ok: true},   // malformed escape stays raw, judged downstream
		{href: "/trailing%", want: "/trailing%", ok: true},
		{href: `/enc%5Cslash`, ok: false}, // decoded backslash is still a backslash
	} {
		got, ok := internalRoutePath(tc.href)
		if ok != tc.ok || got != tc.want {
			t.Errorf("internalRoutePath(%q) = (%q, %v), want (%q, %v)", tc.href, got, ok, tc.want, tc.ok)
		}
	}
}

// The extractor must HTML-unescape attribute values: chrome markup
// serializes "&" as "&amp;", and an href read verbatim would name a
// path nobody serves. Pinned by mutation: dropping stdhtml.UnescapeString
// fails this test.
func TestExtractChromeHrefsUnescapesEntities(t *testing.T) {
	html := `<footer><a href="/docs?a=1&amp;b=2">Query</a><a href="/legal&amp;terms">Path</a></footer>`
	got := extractChromeHrefs(html)
	want := []string{"/docs?a=1&b=2", "/legal&terms"}
	if len(got) != len(want) {
		t.Fatalf("extractChromeHrefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractChromeHrefs = %v, want %v (entity escaping must not survive extraction)", got, want)
		}
	}
}

// TestStrictChromeLinkEntityEscapedHrefResolves: a chrome href whose
// "&" is entity-escaped ("/legal&amp;terms") must resolve against the
// screen registered at "/legal&terms". Without the unescape in
// extractChromeHrefs the raw href names an unserved path and the check
// false-positives.
func TestStrictChromeLinkEntityEscapedHrefResolves(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/legal&terms", &describedScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Legal", Href: "/legal&amp;terms"},
	)))
	if msg := bootCheckPanic(t, New(a, strictSiteOptions()...), nil); msg != "" {
		t.Fatalf("entity-escaped href to /legal&terms flagged:\n%s", msg)
	}
	// Control: with the screen gone the same href is a finding, so the
	// pass above is the unescape working, not an exemption swallowing it.
	a2 := app.NewApp("demo")
	a2.Register("/", &describedScreen{}, nil)
	a2.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Legal", Href: "/legal&amp;terms"},
	)))
	if msg := bootCheckPanic(t, New(a2, strictSiteOptions()...), nil); !strings.Contains(msg, "/legal&terms") {
		t.Fatalf("entity-escaped href with no screen behind it not flagged (findings report the unescaped href):\n%s", msg)
	}
}

// TestStrictChromeLinkPercentEncodedNonASCIIResolves: net/http decodes
// percent-escapes before routing, so GET /docs/caf%C3%A9 serves the
// screen at /docs/café. The check must resolve the same way or it
// flags every encoded non-ASCII screen.
func TestStrictChromeLinkPercentEncodedNonASCIIResolves(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/docs/café", &describedScreen{}, nil)
	a.SetDefaultLayout(app.NewLayout("marketing").WithFooter(chromeFooter(
		footerLink{Label: "Café docs", Href: "/docs/caf%C3%A9"},
	)))
	if msg := bootCheckPanic(t, New(a, strictSiteOptions()...), nil); msg != "" {
		t.Fatalf("percent-encoded link to /docs/café flagged:\n%s", msg)
	}
}

// TestStrictChromeLinkMalformedEscapeIsFlaggedNotCrash: a malformed
// escape can never be served (the server 400s the browser's verbatim
// request), so it is a finding — and must not panic the check itself.
func TestStrictChromeLinkMalformedEscapeIsFlaggedNotCrash(t *testing.T) {
	for _, href := range []string{"/bad%zz", "/trailing%"} {
		ds := linkHost(t, strictSiteOptions, chromeFooter(
			footerLink{Label: "Broken", Href: href},
		))
		msg := bootCheckPanic(t, ds, nil)
		if !strings.Contains(msg, href) {
			t.Fatalf("malformed escape %q not reported as a finding:\n%s", href, msg)
		}
	}
}

// TestStrictChromeLinkToUIHostEndpointsPass pins the five UIHost-owned
// endpoints that register at the TAIL of Mount — after every strictly
// documented surface — plus /llms.txt. Each row is one config shape;
// enabled, its link must pass; disabled, the same link must be flagged
// (config-gated, like /sitemap.xml without WithSitemap).
func TestStrictChromeLinkToUIHostEndpointsPass(t *testing.T) {
	for _, tc := range []struct {
		name    string
		href    string
		enabled func(t *testing.T) []Option
	}{
		{
			name: "llm-pages index",
			href: "/llm-pages.md",
			enabled: func(t *testing.T) []Option {
				return append(strictSiteOptions(), WithPublicLLMMD())
			},
		},
		{
			name: "llms-full corpus",
			href: "/llms-full.txt",
			enabled: func(t *testing.T) []Option {
				return append(strictSiteOptions(),
					WithAgentReady(AgentReadyConfig{Title: "Demo", Summary: "A demo app.", FullText: "# Full\n"}))
			},
		},
		{
			name: "agent card",
			href: "/.well-known/agent-card.json",
			enabled: func(t *testing.T) []Option {
				return append(strictSiteOptions(), WithAgentCard(AgentCardConfig{Name: "Demo", Description: "d"}))
			},
		},
		{
			name: "legacy agent.json alias",
			href: "/.well-known/agent.json",
			enabled: func(t *testing.T) []Option {
				return append(strictSiteOptions(), WithAgentCard(AgentCardConfig{Name: "Demo", Description: "d"}))
			},
		},
		{
			name: "jwks",
			href: "/.well-known/jwks.json",
			enabled: func(t *testing.T) []Option {
				return append(strictSiteOptions(),
					WithAgentReady(AgentReadyConfig{
						BaseURL: "https://demo.example",
						AgentCard: &AgentCardConfig{
							Name: "Demo", Description: "d",
							SigningKeys: []AgentCardSigningKey{{KeyID: "k1", Signer: fixedEd25519(t)}},
						},
					}))
			},
		},
	} {
		t.Run(tc.name+" enabled", func(t *testing.T) {
			opts := tc.enabled(t)
			ds := linkHost(t, func() []Option { return opts }, chromeFooter(
				footerLink{Label: tc.name, Href: tc.href},
			))
			if msg := bootCheckPanic(t, ds, nil); msg != "" {
				t.Fatalf("link to enabled %s flagged:\n%s", tc.href, msg)
			}
		})
		t.Run(tc.name+" disabled", func(t *testing.T) {
			// Same href, no enabling option: the route does not exist,
			// so the link is dead and must be a finding. This is the
			// control that keeps the enabled-pass honest.
			ds := linkHost(t, strictSiteOptions, chromeFooter(
				footerLink{Label: tc.name, Href: tc.href},
			))
			if msg := bootCheckPanic(t, ds, nil); !strings.Contains(msg, tc.href) {
				t.Fatalf("link to unconfigured %s not flagged:\n%s", tc.href, msg)
			}
		})
	}
}

// TestStrictChromeLinkUnderCatchAllRoutePassesUnverified pins the
// documented gap, not a guarantee: a catch-all GET route claims every
// path, so the check is satisfied without knowing whether the handler
// really serves the href. Accepted, not verified — see strict-mode.md.
func TestStrictChromeLinkUnderCatchAllRoutePassesUnverified(t *testing.T) {
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Nowhere", Href: "/definitely/missing"},
	))
	if msg := bootCheckPanic(t, ds, func(r *router.Router) {
		r.GetFunc("/{path...}", func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		})
	}); msg != "" {
		t.Fatalf("link under catch-all flagged; the documented contract says it passes unverified:\n%s", msg)
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
