package main

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// chromeBlueprintYAML is a marketing-layout blueprint (module + two
// marketing screens, one of them the login form) with an auth block and
// a dark palette, so the auth-aware header, the theme toggle, and the
// appTheme() Extend path all render in one generation.
func chromeBlueprintYAML() string {
	return `
app:
  name: Chroma
  module: github.com/example/chroma
  auth:
    enabled: true
    dev_mode: true
  theme:
    primary: "#4338CA"
    dark:
      primary: "#8B80F2"
screens:
  - name: home
    route: /
    layout: marketing
    title: Chroma
    body:
      - kind: hero
        props:
          title: Ship it
  - name: pricing
    route: /pricing
    layout: marketing
    title: Pricing
    body:
      - kind: hero
        props:
          title: Pricing
  - name: login
    route: /login
    layout: marketing
    title: Sign in
    body:
      - kind: login_form
`
}

// chromeFixture renders the marketing blueprint and returns its files.
func chromeFixture(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, chromeBlueprintYAML())
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	return filesByName(mustRenderBlueprintFiles(t, bp))
}

// chromeFileNames are the marketing-chrome packages' files, exactly
// what the app owns after generation: the canonical siteheader /
// sitefooter packages `gofastr generate package` copies, tests
// included, with the self-import rewritten to the app's module. A
// missing file is a broken app (the Go imports a package that is not
// there, or the owned style has no sheet).
var chromeFileNames = []string{
	"sitefooter/sitefooter.go",
	"sitefooter/sitefooter.style.css",
	"sitefooter/sitefooter_chromium_test.go",
	"sitefooter/sitefooter_style.gen.go",
	"sitefooter/sitefooter_test.go",
	"siteheader/siteheader.go",
	"siteheader/siteheader.style.css",
	"siteheader/siteheader.tokens.css",
	"siteheader/siteheader_chromium_test.go",
	"siteheader/siteheader_style.gen.go",
	"siteheader/siteheader_test.go",
	"siteheader/siteheader_tokens.gen.go",
}

// A marketing blueprint with no module cannot import its own chrome
// packages, so generation refuses instead of writing an app.go that
// does not build.
func TestMarketingChromeNeedsModule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	yml := strings.Replace(chromeBlueprintYAML(), "  module: github.com/example/chroma\n", "", 1)
	writeTestFile(t, path, yml)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if bp.App.Module != "" {
		t.Fatalf("fixture still carries a module: %q", bp.App.Module)
	}
	_, err = renderBlueprintFiles(bp)
	if err == nil || !strings.Contains(err.Error(), "marketing screens need a Go module") {
		t.Fatalf("err = %v, want the missing-module refusal", err)
	}
}

// TestMarketingChromeShipsOwnedPackages asserts the generator emits the
// header and footer as the app's own packages — the replacement for the
// deleted ui.SiteHeader / ui.SiteFooter — and emits NOTHING chrome-shaped
// for a blueprint with no marketing screens (the app shell keeps its
// sidebar-only frame).
func TestMarketingChromeShipsOwnedPackages(t *testing.T) {
	files := chromeFixture(t)
	var emitted []string
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if strings.HasPrefix(name, "siteheader/") || strings.HasPrefix(name, "sitefooter/") {
			emitted = append(emitted, name)
		}
	}
	if !slices.Equal(emitted, chromeFileNames) {
		t.Errorf("marketing blueprint chrome files = %v, want exactly %v", emitted, chromeFileNames)
	}
	// The copied tests import the app's own package, never the framework's
	// canonical copy: an unrewritten import ships code that cannot compile.
	for _, name := range chromeFileNames {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.Contains(files[name], "cmd/gofastr/packages") {
			t.Errorf("%s still imports the canonical package instead of github.com/example/chroma", name)
		}
	}

	// The converse: a plain app blueprint (screens, no marketing layout)
	// must not ship chrome packages it never mounts.
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Plain
  module: example.com/plain
entities:
  - name: notes
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: dashboard
    route: /
    title: Dashboard
    body:
      - kind: entity_list
        entity: notes
        fields: [title]
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		if strings.HasPrefix(f.name, "siteheader/") || strings.HasPrefix(f.name, "sitefooter/") {
			t.Errorf("non-marketing blueprint emits chrome package file %s", f.name)
		}
	}
}

// TestMarketingChromeAppGoWiring asserts app.go calls the app's own
// chrome packages and never the deleted kit components: the marketing
// layout takes the header element siteheader renders (its owned style's
// scope root is the banner landmark itself) rather than wrapping it, and
// the theme carries the header package's own tokens — declared palette
// or not, or every var() in the owned sheets reads nothing.
func TestMarketingChromeAppGoWiring(t *testing.T) {
	files := chromeFixture(t)
	appGo := files["app.go"]
	if appGo == "" {
		t.Fatal("no app.go in generated files")
	}
	for _, want := range []string{
		`"github.com/example/chroma/siteheader"`,
		`"github.com/example/chroma/sitefooter"`,
		"siteheader.Render(siteheader.Config{",
		"sitefooter.Render(sitefooter.Config{",
		"theme.Extend(siteheader.Tokens)",
		// The phone menu is focus-trapped: headless.Disclosure's Trap.
	} {
		if !strings.Contains(appGo, want) {
			t.Errorf("app.go lacks %q:\n%s", want, appGo)
		}
	}
	for _, banned := range []string{"ui.SiteHeader", "ui.SiteFooter"} {
		if strings.Contains(appGo, banned) {
			t.Errorf("app.go still references the deleted %s component; marketing chrome is the app's own siteheader/sitefooter package now", banned)
		}
	}
	// siteheader renders the banner landmark itself; a second wrapper
	// would nest one landmark inside another.
	if strings.Contains(appGo, "html.Header(html.HeaderConfig{Banner: true}") {
		t.Errorf("app.go wraps the header in its own banner landmark; siteheader.Render already renders it")
	}

	headerGo := files["siteheader/siteheader.go"]
	if !strings.Contains(headerGo, "html.Header(html.HeaderConfig{Banner: true}") {
		t.Errorf("siteheader.go does not render the banner landmark itself:\n%s", headerGo)
	}
	if !strings.Contains(headerGo, "Trap:    true") && !strings.Contains(headerGo, "Trap: true") {
		t.Errorf("siteheader.go drops the phone menu's focus trap (headless.Disclosure Trap):\n%s", headerGo)
	}

	footerGo := files["sitefooter/sitefooter.go"]
	if !strings.Contains(footerGo, "html.Footer(html.FooterConfig{ContentInfo: true}") {
		t.Errorf("sitefooter.go does not render the contentinfo landmark itself:\n%s", footerGo)
	}
}

// TestMarketingChromeThemeExtendWithoutPalette asserts the Extend path a
// blueprint with NO declared palette takes: the default theme must still
// carry siteheader's tokens, or the owned sheets' var() reads nothing.
func TestMarketingChromeThemeExtendWithoutPalette(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, marketingLinksFixtureYAML(false))
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files := filesByName(mustRenderBlueprintFiles(t, bp))
	appGo := files["app.go"]
	if !strings.Contains(appGo, "style.DefaultTheme().Extend(siteheader.Tokens)") {
		t.Errorf("theme-less marketing app does not Extend the default theme with siteheader.Tokens:\n%s", appGo)
	}
}

// TestMarketingChromeOwnedSheetsAreClean runs the ownstyle checks over
// the emitted sheets exactly as the generated app's own `gofastr verify`
// would: default-theme built-ins plus the tokens file the same emission
// wrote. The generator refuses to emit a sheet with a finding, so this
// also proves that refusal is wired (a dirty template fails generation
// in mustRenderBlueprintFiles before any assertion here).
func TestMarketingChromeOwnedSheetsAreClean(t *testing.T) {
	files := chromeFixture(t)

	builtins := style.ThemeToTokens(style.DefaultTheme())
	tokensSrc := ownstyle.SheetSource{
		File: "siteheader/siteheader.tokens.css",
		Src:  files["siteheader/siteheader.tokens.css"],
	}
	_, appTokens, tokenDiags := ownstyle.CheckTokenFiles([]ownstyle.SheetSource{tokensSrc}, builtins)
	for _, d := range tokenDiags {
		if ownstyle.Suppressed(tokensSrc.Src, d.Diag) {
			continue
		}
		t.Errorf("%s:%d:%d: %s %s %s", d.File, d.Diag.Line, d.Diag.Col, d.Diag.Severity, d.Diag.Rule, d.Diag.Message)
	}
	tokens := ownstyle.CheckTokens(builtins, appTokens)
	for _, sheet := range []string{"siteheader/siteheader.style.css", "sitefooter/sitefooter.style.css"} {
		src := files[sheet]
		diags := ownstyle.Check(sheet, src, ownstyle.KindScoped, tokens)
		parsedSheet, parseDiags := ownstyle.Parse(src)
		diags = append(diags, parseDiags...)
		if parsedSheet != nil {
			_, modelDiags := ownstyle.Model(parsedSheet)
			diags = append(diags, modelDiags...)
		}
		for _, d := range diags {
			if ownstyle.Suppressed(src, d) {
				continue
			}
			t.Errorf("%s:%d:%d: %s %s %s", sheet, d.Line, d.Col, d.Severity, d.Rule, d.Message)
		}
	}
}

// TestMarketingChromeGeneratedFilesAreCurrent pins the emitted
// _style.gen.go / _tokens.gen.go to the sheets beside them, the same
// bytes GOFASTR1814 recomputes: a gen file that does not carry its
// sheet's source hash is stale the moment it lands, and `gofastr verify`
// in the generated app fails on it.
func TestMarketingChromeGeneratedFilesAreCurrent(t *testing.T) {
	files := chromeFixture(t)
	for _, tc := range []struct{ sheet, gen string }{
		{"siteheader/siteheader.style.css", "siteheader/siteheader_style.gen.go"},
		{"siteheader/siteheader.tokens.css", "siteheader/siteheader_tokens.gen.go"},
		{"sitefooter/sitefooter.style.css", "sitefooter/sitefooter_style.gen.go"},
	} {
		sheet, gen := files[tc.sheet], files[tc.gen]
		if sheet == "" || gen == "" {
			t.Fatalf("missing %s or %s", tc.sheet, tc.gen)
		}
		want := "Source hash: sha256:" + ownstyle.SourceHash(sheet)
		if !strings.Contains(gen, want) {
			t.Errorf("%s is not current for %s (want %q): the generated app's gofastr verify (GOFASTR1814) would fail on it", tc.gen, tc.sheet, want)
		}
	}
}
