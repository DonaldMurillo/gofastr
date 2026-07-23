package uihost

import (
	"bytes"
	"log/slog"
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
func (s *describedScreen) ScreenTitle() string       { return "Pricing" }
func (s *describedScreen) ScreenDescription() string { return "Plans and prices." }

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
	a.Register("/docs/:slug", &describedScreen{}, nil)
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
