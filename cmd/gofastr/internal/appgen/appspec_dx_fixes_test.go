package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// stubFontFetcher swaps in a fake font fetcher for the duration of a test so
// the suite never touches the network.
func stubFontFetcher(t *testing.T, fn func(family string) ([]byte, error)) {
	t.Helper()
	prev := FontFetcher
	FontFetcher = fn
	t.Cleanup(func() { FontFetcher = prev })
}

// ---- Item 1: self-hosted fonts ------------------------------------------

// A theme naming fonts must ship the matching woff2 files, and every file the
// emitted @font-face references must actually exist in the file set. No
// silent /fonts/*.woff2 404.
func TestFontsShippedMatchCSS(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		return []byte("wOF2-" + family), nil
	})
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans", "font_body": "Demo Serif"},
		},
	}
	files := filesByName(mustRenderAppSpecFiles(t, bp))
	for _, want := range []string{"static/fonts/test-sans.woff2", "static/fonts/demo-serif.woff2"} {
		if _, ok := files[want]; !ok {
			t.Fatalf("expected font file %s to be shipped; got files: %v", want, keysOf(files))
		}
	}
	// The generated app composes the design system rather than carrying a
	// baked CSS string, so the source names the FAMILIES and
	// style.FontFaceCSS derives the URLs from them at runtime.
	appGo := files["app.go"]
	for _, family := range []string{`Family: "Test Sans"`, `Family: "Demo Serif"`} {
		if !strings.Contains(appGo, family) {
			t.Fatalf("generated fontFaceCSS should declare %s:\n%s", family, appGo)
		}
	}
	if strings.Contains(appGo, "@font-face {") {
		t.Fatalf("the generator baked CSS into the app instead of composing style.FontFaceCSS:\n%s", appGo)
	}
	// The URLs those families resolve to must still be the shipped files:
	// same slug function on both sides, so they cannot drift.
	css := appSpecFontFaceCSS(bp.App.Theme)
	for _, ref := range []string{"/fonts/test-sans.woff2", "/fonts/demo-serif.woff2"} {
		if !strings.Contains(css, ref) {
			t.Fatalf("rendered fontFaceCSS should reference %s:\n%s", ref, css)
		}
	}
	// Every referenced woff2 must be shipped (the whole point of the fix).
	if len(appSpecMissingFontSlugs(bp, mustRenderAppSpecFiles(t, bp))) != 0 {
		t.Fatalf("no font file should be missing when the fetch succeeds")
	}
}

// When the generate-time fetch fails (offline), the app is still emitted but
// the missing files are reported so generate can warn. No silent fallback.
func TestFontsOfflineReportedMissing(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		return nil, errStubOffline
	})
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans"},
		},
	}
	fontFiles, missing := appSpecFontAssets(bp)
	if len(fontFiles) != 0 {
		t.Fatalf("offline fetch must ship no font files, got %d", len(fontFiles))
	}
	if len(missing) != 1 || missing[0] != "static/fonts/test-sans.woff2" {
		t.Fatalf("expected missing static/fonts/test-sans.woff2, got %v", missing)
	}
	// The app still renders (no font file present) and reports the gap.
	files := mustRenderAppSpecFiles(t, bp)
	if got := appSpecMissingFontSlugs(bp, files); len(got) != 1 {
		t.Fatalf("expected 1 missing font, got %v", got)
	}
}

// The generated main.go boot-checks the self-hosted font files so a missing
// asset warns at startup rather than 404ing silently.
func TestFontsBootCheckEmitted(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) { return []byte("x"), nil })
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:   "Fonts",
			Module: "example.com/fonts",
			Theme:  map[string]string{"font_heading": "Test Sans"},
		},
	}
	main := RenderAppSpecMain(bp)
	if !strings.Contains(main, "static/fonts/test-sans.woff2") || !strings.Contains(main, "os.Stat(fontFile)") {
		t.Fatalf("main.go should boot-check the font file:\n%s", main)
	}
}

var errStubOffline = &stubErr{"offline"}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The generated e2e suite drives a subprocess over real HTTP, so it never
// touches framework.TestHarness. Without GOFASTR_SEMANTIC_COVERAGE on the
// server's env, every route that suite covers reports as unreached by
// `gofastr verify`, a thorough e2e suite scoring zero.
func TestGeneratedE2ESetsSemanticCoverageEnv(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) { return []byte("x"), nil })
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Cov", Module: "example.com/cov"},
		Screens: []appspec.AppSpecScreen{{
			Name: "home", Route: "/", Body: []appspec.AppSpecBlock{{Kind: "hero", Props: map[string]any{"title": "Hi"}}},
		}},
	}
	e2e := filesByName(mustRenderAppSpecFiles(t, bp))["e2e_test.go"]
	if e2e == "" {
		t.Fatal("no e2e_test.go emitted")
	}
	if !strings.Contains(e2e, `"GOFASTR_SEMANTIC_COVERAGE=1"`) {
		t.Fatalf("generated e2e test does not enable semantic coverage on the server:\n%s", e2e)
	}
	// It must sit on the SAME env the server subprocess gets, not on the
	// test process. The recording happens server-side.
	if !strings.Contains(e2e, `srv.Env = append(os.Environ(), "PORT="+addr, "GOFASTR_SEMANTIC_COVERAGE=1"`) {
		t.Fatalf("the variable is not on the server subprocess env:\n%s", e2e)
	}
}
