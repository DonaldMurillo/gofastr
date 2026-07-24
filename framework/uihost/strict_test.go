package uihost

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/axecov"
)

// bareScreen renders content but declares no title, description, or SEO.
type bareScreen struct{}

func (s *bareScreen) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.HTML("hi"))
}

// describedScreen declares title + description via the metadata interfaces.
type describedScreen struct{}

func (s *describedScreen) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.HTML("hi"))
}
func (s *describedScreen) SetParams(map[string]string) {}
func (s *describedScreen) ScreenTitle() string         { return "Pricing" }
func (s *describedScreen) ScreenDescription() string   { return "Plans and prices." }

// optedOutScreen has a title but opts out of per-page SEO via the
// documented zero-value ScreenSEO return.
type optedOutScreen struct{}

func (s *optedOutScreen) Render() render.HTML {
	return html.Div(html.DivConfig{}, render.HTML("hi"))
}
func (s *optedOutScreen) ScreenTitle() string { return "Internal" }
func (s *optedOutScreen) ScreenSEO() SEO      { return SEO{} }

// strictSiteOptions is the complete site-level SEO surface strict mode
// requires.
func strictSiteOptions() []Option {
	return []Option{
		WithStrict(),
		WithDescription("A demo app."),
		WithFavicon("/static/favicon.svg"),
		WithSitemap(SitemapConfig{BaseURL: "https://example.com"}),
		WithRobots(RobotsConfig{}),
	}
}

// mountPanic runs Mount and returns the recovered panic message ("" when
// Mount completed).
func mountPanic(t *testing.T, ds *UIHost) (msg string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg = r.(string)
		}
	}()
	ds.Mount(router.New())
	return ""
}

func TestStrictPassesOnCompleteApp(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("unexpected strict panic:\n%s", msg)
	}
}

func TestStrictFlagsScreenMissingTitleAndDescription(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/bare", &bareScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	if msg == "" {
		t.Fatal("Mount did not panic for a bare screen under strict mode")
	}
	if !strings.Contains(msg, "/bare") || !strings.Contains(msg, "title") {
		t.Fatalf("panic does not name the screen and the missing title:\n%s", msg)
	}
	if !strings.Contains(msg, "description") {
		t.Fatalf("panic does not name the missing description:\n%s", msg)
	}
}

func TestStrictZeroValueScreenSEOOptsOutOfDescription(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/internal", &optedOutScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("ScreenSEO opt-out still flagged:\n%s", msg)
	}
}

func TestStrictSkipsNonPageScreens(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.RegisterScreen(app.NewDialog("/confirm", &bareScreen{}), nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("dialog screen flagged by strict mode:\n%s", msg)
	}
}

func TestStrictFlagsMissingSiteSurface(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, WithStrict()) // no description, favicon, sitemap, robots
	msg := mountPanic(t, ds)
	if msg == "" {
		t.Fatal("Mount did not panic for missing site-level SEO surface")
	}
	for _, want := range []string{"description", "icon", "sitemap", "robots"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("panic missing %q finding:\n%s", want, msg)
		}
	}
}

func TestStrictOffByDefault(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/bare", &bareScreen{}, nil)
	ds := New(a) // no WithStrict
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("strict checks ran without WithStrict:\n%s", msg)
	}
}

func TestStrictAxeCoverageEnforcedInDevOnly(t *testing.T) {
	newHost := func(t *testing.T) *UIHost {
		a := app.NewApp("demo")
		a.Register("/", &describedScreen{}, nil)
		a.Register("/pricing", &describedScreen{}, nil)
		return New(a, strictSiteOptions()...)
	}

	t.Run("prod boot ignores the manifest", func(t *testing.T) {
		t.Chdir(t.TempDir()) // no manifest anywhere
		if msg := mountPanic(t, newHost(t)); msg != "" {
			t.Fatalf("axe coverage enforced outside dev:\n%s", msg)
		}
	})

	t.Run("dev with no manifest warns and serves", func(t *testing.T) {
		// A fresh clone / fresh generate has never run the axe suite —
		// blocking boot there would wall first contact behind a Chrome
		// run. Absence warns (scream but serve); gaps fail (below).
		t.Chdir(t.TempDir())
		t.Setenv("GOFASTR_DEV", "1")
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		defer slog.SetDefault(prev)
		if msg := mountPanic(t, newHost(t)); msg != "" {
			t.Fatalf("missing manifest must warn, not fail boot:\n%s", msg)
		}
		if !strings.Contains(buf.String(), "axe coverage unverified") {
			t.Fatalf("missing manifest produced no warning; log was:\n%s", buf.String())
		}
	})

	t.Run("dev with a gap names the uncovered route", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("GOFASTR_DEV", "1")
		if err := axecov.Record(".", "/", "dark"); err != nil {
			t.Fatal(err)
		}
		msg := mountPanic(t, newHost(t))
		if !strings.Contains(msg, "/pricing") {
			t.Fatalf("uncovered /pricing not named:\n%s", msg)
		}
		if strings.Contains(msg, `"/" `) {
			t.Fatalf("covered route flagged:\n%s", msg)
		}
	})

	t.Run("dev with full coverage passes", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("GOFASTR_DEV", "1")
		for _, p := range []string{"/", "/pricing"} {
			if err := axecov.Record(".", p, "dark"); err != nil {
				t.Fatal(err)
			}
		}
		if msg := mountPanic(t, newHost(t)); msg != "" {
			t.Fatalf("full coverage still flagged:\n%s", msg)
		}
	})
}

// captureLog swaps the default slog handler for a buffer for the test's
// duration and returns the buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestStrictConfigWarnLevelLogsAndServes(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/bare", &bareScreen{}, nil)
	buf := captureLog(t)
	ds := New(a,
		WithStrict(StrictConfig{
			ScreenTitles:       StrictWarn,
			ScreenDescriptions: StrictWarn,
			SiteDescription:    StrictWarn,
			SiteIcon:           StrictWarn,
			Sitemap:            StrictWarn,
			Robots:             StrictWarn,
		}),
	)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("warn-level checks must serve, not panic:\n%s", msg)
	}
	logged := buf.String()
	for _, want := range []string{"/bare", "title", "description", "sitemap"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("warn log missing %q; log was:\n%s", want, logged)
		}
	}
}

func TestStrictConfigOffLevelIsSilent(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/bare", &bareScreen{}, nil)
	buf := captureLog(t)
	ds := New(a,
		WithStrict(StrictConfig{
			ScreenTitles:       StrictOff,
			ScreenDescriptions: StrictOff,
			SiteDescription:    StrictOff,
			SiteIcon:           StrictOff,
			Sitemap:            StrictOff,
			Robots:             StrictOff,
			AxeCoverage:        StrictOff,
		}),
	)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("off-level checks must not panic:\n%s", msg)
	}
	if strings.Contains(buf.String(), "strict") {
		t.Fatalf("off-level checks must not log; log was:\n%s", buf.String())
	}
}

func TestStrictConfigMixedLevelsSplitCorrectly(t *testing.T) {
	// Title stays enforced (zero value), description demoted to warn:
	// boot must fail naming ONLY the title.
	a := app.NewApp("demo")
	a.Register("/bare", &bareScreen{}, nil)
	buf := captureLog(t)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{ScreenDescriptions: StrictWarn}))
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "title") {
		t.Fatalf("enforced title finding missing from panic:\n%s", msg)
	}
	if strings.Contains(msg, "description") {
		t.Fatalf("warn-level description leaked into the panic:\n%s", msg)
	}
	if !strings.Contains(buf.String(), "description") {
		t.Fatalf("warn-level description not logged; log was:\n%s", buf.String())
	}
}

func TestStrictConfigExemptScreens(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/machine/feed", &bareScreen{}, nil)
	a.Register("/internal/tools/report", &bareScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{
		ExemptScreens: []string{"/machine/feed", "/internal/*"},
	}))
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("exempt screens still checked:\n%s", msg)
	}
}

func TestStrictConfigManifestMissingCanBeEnforced(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	applyOption(ds, WithStrict(StrictConfig{AxeManifestMissing: StrictAbsenceEnforce}))
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "manifest") {
		t.Fatalf("AxeManifestMissing: StrictEnforce did not fail boot:\n%s", msg)
	}
}

// applyOption applies one more Option to an already-constructed host —
// test shorthand for composing strictSiteOptions with a custom config.
func applyOption(ds *UIHost, opt Option) { opt(ds) }

func TestStrictAxeCoverageSkipsScreenlessApp(t *testing.T) {
	// An app with no page screens (API-only, or dialogs/drawers only) has
	// nothing an axe test could scan — dev boot must not demand a manifest.
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	a := app.NewApp("demo")
	a.RegisterScreen(app.NewDialog("/confirm", &bareScreen{}), nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("screen-less app failed the axe-coverage check:\n%s", msg)
	}
}

func TestStrictAxeCoverageResolvesDynamicRoutes(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/docs/:slug", &staticPathsScreen{}, nil)
	// The manifest holds concrete scanned URLs; a concrete path must
	// count as coverage for the dynamic pattern it resolves to.
	for _, p := range []string{"/", "/docs/install"} {
		if err := axecov.Record(".", p, "dark"); err != nil {
			t.Fatal(err)
		}
	}
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("concrete scan did not cover its dynamic route:\n%s", msg)
	}
}

// staticPathsScreen is a dynamic-route screen that declares concrete
// instances — the mechanism that makes a dynamic route visible to the
// sitemap and therefore demandable by the axe-coverage check.
type staticPathsScreen struct{ describedScreen }

func (s *staticPathsScreen) StaticPaths(ctx context.Context) []map[string]string {
	return []map[string]string{{"slug": "install"}}
}

func TestWithStrictBareCallResetsRelaxedConfig(t *testing.T) {
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a,
		WithStrict(StrictConfig{SiteDescription: StrictOff, SiteIcon: StrictOff, Sitemap: StrictOff, Robots: StrictOff}),
		WithStrict(), // documented: bare call enforces everything — must reset
	)
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "robots") {
		t.Fatalf("bare WithStrict after a relaxed config did not restore enforcement:\n%s", msg)
	}
}

func TestWithStrictRejectsMultipleConfigs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithStrict with two configs did not panic")
		}
	}()
	WithStrict(StrictConfig{}, StrictConfig{})
}

func TestStrictFlagsInvalidSitemapBaseURL(t *testing.T) {
	newHost := func(base string) *UIHost {
		a := app.NewApp("demo")
		a.Register("/", &describedScreen{}, nil)
		return New(a,
			WithStrict(),
			WithDescription("A demo app."),
			WithFavicon("/favicon.svg"),
			WithSitemap(SitemapConfig{BaseURL: base}),
			WithRobots(RobotsConfig{}),
		)
	}
	// Path-bearing bases are rejected too: the static exporter adds its
	// own BasePath, so a path here gets prefixed twice.
	for _, bad := range []string{"", "example.com", "ftp://example.com", "https://user:pw@example.com", "https://example.com?x=1", "https://example.com/app"} {
		if msg := mountPanic(t, newHost(bad)); !strings.Contains(msg, "BaseURL") {
			t.Fatalf("invalid sitemap BaseURL %q not flagged:\n%s", bad, msg)
		}
	}
	if msg := mountPanic(t, newHost("https://example.com/")); msg != "" {
		t.Fatalf("bare origin with trailing slash flagged:\n%s", msg)
	}
}

func TestStrictCorruptManifestFailsBoot(t *testing.T) {
	// A missing manifest warns (fresh checkout); a CORRUPT one must not
	// be mistaken for absence — that would relax enforcement exactly
	// when the coverage record is untrustworthy.
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GOFASTR_DEV", "1")
	if err := os.MkdirAll(filepath.Join(dir, ".gofastr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, axecov.FileName), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "manifest") {
		t.Fatalf("corrupt manifest did not fail boot:\n%s", msg)
	}
}

func TestStrictDynamicRouteWithoutStaticPathsWarnsNotDemands(t *testing.T) {
	// A dynamic route with no StaticPaths is invisible to the sitemap,
	// so a sitemap-driven axe gate can never cover it. Strict must not
	// demand what the gate cannot discover — it screams instead.
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	buf := captureLog(t)
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/orders/:id", &describedScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("undiscoverable dynamic route was demanded:\n%s", msg)
	}
	logged := buf.String()
	if !strings.Contains(logged, "/orders/:id") || !strings.Contains(logged, "StaticPaths") {
		t.Fatalf("invisible dynamic route not warned about; log was:\n%s", logged)
	}
}

// emptyPathsScreen declares StaticPaths but returns no instances — as
// invisible to the sitemap as not implementing it at all.
type emptyPathsScreen struct{ describedScreen }

func (s *emptyPathsScreen) StaticPaths(ctx context.Context) []map[string]string { return nil }

func TestStrictDynamicRouteWithEmptyStaticPathsWarnsNotDemands(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	buf := captureLog(t)
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/orders/:id", &emptyPathsScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	if msg := mountPanic(t, ds); msg != "" {
		t.Fatalf("undiscoverable (empty StaticPaths) dynamic route was demanded:\n%s", msg)
	}
	if !strings.Contains(buf.String(), "/orders/:id") {
		t.Fatalf("empty-StaticPaths route not warned about; log was:\n%s", buf.String())
	}
}

func TestStrictDynamicRouteWithStaticPathsIsDemanded(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GOFASTR_DEV", "1")
	if err := axecov.Record(".", "/", "dark"); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("demo")
	a.Register("/", &describedScreen{}, nil)
	a.Register("/docs/:slug", &staticPathsScreen{}, nil)
	ds := New(a, strictSiteOptions()...)
	msg := mountPanic(t, ds)
	if !strings.Contains(msg, "/docs/:slug") {
		t.Fatalf("uncovered StaticPaths-bearing dynamic route not demanded:\n%s", msg)
	}
}
