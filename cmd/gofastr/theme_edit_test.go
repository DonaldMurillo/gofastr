package main

import (
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	gotoken "go/token"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// newTestServer builds a themeEditServer backed by a real in-process UIHost
// (the same wiring runThemeEdit uses, minus the network listener). No DB,
// no framework.App: UIHost.New takes a core-ui/app.App directly.
func newTestServer(t *testing.T) *themeEditServer {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "theme")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := uitheme.Default()
	a := app.NewApp("theme-edit-test").WithTheme(base)
	a.Register("/preview", &galleryPreviewScreen{}, nil)
	host := uihost.New(a)
	return &themeEditServer{
		host:    host,
		base:    base,
		working: base,
		outPath: filepath.Join(outDir, "theme.go"),
		hosts:   []string{"127.0.0.1:0"},
		origins: []string{"http://127.0.0.1:0"},
		token:   "test-token", // not-a-secret: fixture bearer for the in-process test server
	}
}

// The acceptance test: editing a token through the server registers a
// variant whose served app.css carries the new value. This is worth more
// than a screenshot; it proves the live-preview swap path
// (ApplyTokens → RegisterThemeVariant → /__gofastr/app.css?t=<key>) lands
// the edited token in the CSS the browser fetches.
func TestThemeEditVariantCSSCarriesEditedValue(t *testing.T) {
	srv := newTestServer(t)

	hash, err := srv.applyToken("color-primary", "#FF0000")
	if err != nil {
		t.Fatalf("applyToken: %v", err)
	}
	if hash == "" {
		t.Fatal("applyToken returned empty hash")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/app.css?t="+hash, nil)
	srv.host.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("app.css?t=%s: status %d, want 200", hash, rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "--color-primary: #FF0000") {
		t.Fatalf("variant CSS does not carry the edited value:\n%s", truncate(body, 400))
	}
	// The variant must be served immutable: the content-addressed URL is
	// the cache-busting contract.
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("variant Cache-Control = %q, want immutable", cc)
	}
}

// An invalid value is rejected at the ApplyTokens boundary: the working
// theme is untouched and the error names the offending key. The tool
// surfaces this next to the control rather than silently keeping the old
// value.
func TestThemeEditApplyRejectsInvalidValue(t *testing.T) {
	srv := newTestServer(t)
	before := srv.working.Colors.Primary.Value

	_, err := srv.applyToken("color-primary", "red; --x:}body{display:none}")
	if err == nil {
		t.Fatal("ApplyTokens accepted a CSS-escaping value — the boundary must reject, not sanitise")
	}
	if !strings.Contains(err.Error(), "color-primary") {
		t.Errorf("error does not name the offending key: %v", err)
	}
	if srv.working.Colors.Primary.Value != before {
		t.Error("the working theme was mutated despite a rejection — ApplyTokens must leave the prior state intact")
	}
}

// An unknown key is an error too: a typo in a theme edit must be reported,
// not silently ignored.
func TestThemeEditApplyRejectsUnknownKey(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.applyToken("color-nonexistent", "#000000")
	if err == nil {
		t.Fatal("ApplyTokens accepted an unknown key")
	}
}

// Write-back produces a file that parses as valid Go (format.Source already
// ran inside emitThemeGoSource; parsing is the decisive compile-time check).
func TestThemeEditWritebackProducesParseableGo(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.writeBack(); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	src, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "theme.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("emitted theme.go does not parse: %v\n--- source ---\n%s", err, src)
	}
	if file.Name.Name != "theme" {
		t.Errorf("package name = %q, want %q", file.Name.Name, "theme")
	}
	// The emitted file must call AutoFillNames so the user's file boots
	// without naming every token manually.
	if !strings.Contains(string(src), "style.AutoFillNames(&App)") {
		t.Error("emitted file missing the AutoFillNames init guard")
	}
}

// Write-back after an edit reflects the edit: the value lands in the
// generated file as a %q literal, round-tripping through the emitter.
func TestThemeEditWritebackReflectsEditedValue(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.applyToken("color-primary", "#00FF00"); err != nil {
		t.Fatalf("applyToken: %v", err)
	}
	if err := srv.writeBack(); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	src, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// %q renders #00FF00 as "#00FF00", a double-quoted literal. The raw
	// value must appear; a backtick literal would have been an injection
	// risk.
	if !strings.Contains(string(src), `"#00FF00"`) {
		t.Errorf("edited value not in written file:\n%s", truncate(string(src), 400))
	}
}

// goLiteralBreakers are the byte sequences that end a Go string literal,
// the same set blueprint_emitter_injection_test.go uses. A raw backtick
// literal has no escape mechanism, so one backtick closes it; an
// interpreted literal ends at an unescaped quote and never spans a newline.
// %q must neutralise every one.
var themeEditLiteralBreakers = []string{
	"x`+PWN()+`y",
	`x"+PWN()+"y`,
	"x\nfunc PWN() {}\nvar y = \"",
	"x\r\nPWN()",
}

// The injection test the brief requires: no theme-derived string may
// terminate the Go literal it is emitted into. Theme values are free-form
// CSS (oklch expressions, font stacks with embedded quotes), exactly the
// risky shape. %q escapes every breaking byte, so even a value crafted to
// break out stays data. We bypass ApplyTokens (which would reject these)
// and drive the payloads through the EMITTER directly, because the
// emitter's safety is the property under test.
func TestThemeEditWritebackInjectionSafety(t *testing.T) {
	for _, payload := range themeEditLiteralBreakers {
		label := strings.NewReplacer("\n", "N", "\r", "R", "`", "BT", `"`, "Q").Replace(payload)
		t.Run(label, func(t *testing.T) {
			// Drive the payload through EVERY string the emitter writes.
			//
			// This used to set nine fields by hand and claim to cover "every
			// string-valued token field", of roughly fifty-five. Reflection
			// makes the claim true and keeps it true: a field added to
			// style.Theme later is covered without anyone remembering to add a
			// line here. The map KEYS and the theme Name go in too; both are
			// emitted, and neither was exercised.
			th := uitheme.Default()
			injectEveryThemeString(reflect.ValueOf(&th).Elem(), payload)
			th.Name = payload
			th.DarkColors = map[string]string{"primary": payload, payload: payload}
			th.DarkCode = map[string]string{"kw": payload, payload: payload}
			src, err := emitThemeGoSource(th, "theme")
			if err != nil {
				t.Fatalf("emitter rejected a payload (it must emit it inertly): %v", err)
			}
			assertThemePayloadStayedData(t, src)
		})
	}
}

// assertThemePayloadStayedData parses the emitted file and fails if the
// marker became syntax rather than data. Parsing is the decisive check: an
// injected `func PWN()` is valid Go, so "does it compile" passes it. The
// question is whether "PWN" appears as an IDENTIFIER: inside a string
// literal it is just the value the theme asked for. Modeled on
// blueprint_emitter_injection_test.go's assertPayloadStayedData.
func assertThemePayloadStayedData(t *testing.T, src []byte) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "theme.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("SECURITY: [injection] emitted theme.go does not parse: %v", err)
	}
	var escaped bool
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "PWN") {
			escaped = true
			return false
		}
		return true
	})
	if escaped {
		t.Fatalf("SECURITY: [injection] the payload left its literal and became an identifier in theme.go")
	}
}

// tokenControlType classifies a ThemeToTokens key into the input type the
// controls page renders. The classification is by key prefix: adding a
// new token type to style.Theme still gets a usable text input (never
// hidden).
func TestTokenControlType(t *testing.T) {
	cases := []struct {
		key, want string
	}{
		{"color-primary", "color"},
		{"dark.color-primary", "color"},
		{"color-text-muted", "color"},
		{"z-modal", "number"},
		{"spacing-md", "number-px"},
		{"radii-lg", "number-px"},
		{"breakpoint-md", "number-px"},
		{"font-body", "text"},
		{"shadow-md", "text"},
		{"duration-fast", "text"},
		{"easing-spring", "text"},
		{"text-base", "text"},
		{"tk-kw", "text"},
		{"dark.tk-kw", "text"},
		{"unknown-future-token", "text"},
	}
	for _, c := range cases {
		if got := tokenControlType(c.key); got != c.want {
			t.Errorf("tokenControlType(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// The controls page renders without error and contains a control for at
// least one well-known token. This is the server-side smoke test: the
// generated HTML must mention the token key so the JS can find the input.
func TestThemeEditControlsPageContainsTokenControls(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:0"
	srv.serveControlsPage(rec, req)

	if rec.Code != 200 {
		t.Fatalf("controls page status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-token="color-primary"`) { // not-a-secret: a DOM attribute selector, not a credential
		t.Errorf("controls page missing the color-primary control:\n%s", truncate(body, 400))
	}
	if !strings.Contains(body, `name="theme-edit-token"`) {
		t.Errorf("controls page missing the bearer-token meta tag (name=\"theme-edit-token\")")
	}
}

// The HTTP apply endpoint requires the bearer token: without it, a
// cross-origin page cannot drive edits even if it passes the Host guard.
func TestThemeEditApplyRequiresBearerToken(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/apply", strings.NewReader(`{"key":"color-primary","value":"#000"}`))
	req.Host = "127.0.0.1:0"
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("apply without token: status %d, want 401", rec.Code)
	}
}

// The writeback endpoint writes the file when the bearer token is correct.
func TestThemeEditWritebackHTTP(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/writeback", strings.NewReader(`{}`))
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("writeback: status %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(srv.outPath); err != nil {
		t.Errorf("writeback did not create %s: %v", srv.outPath, err)
	}
}

// The Host guard rejects a request whose Host header is not one of the
// pinned loopback authorities, the DNS-rebinding defence.
func TestThemeEditHostGuard(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com:8080"
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Host guard: status %d for foreign Host, want 403", rec.Code)
	}
}

// packageNameForPath derives the Go package name from the output path's
// directory.
func TestPackageNameForPath(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		path, want string
	}{
		{"theme/theme.go", "theme"},
		{"design-system/tokens.go", "design_system"},
	}
	for _, c := range cases {
		path := filepath.Join(root, c.path)
		if got := packageNameForPath(path); got != c.want {
			t.Errorf("packageNameForPath(%q) = %q, want %q", path, got, c.want)
		}
	}
}

// durationLiteral renders a time.Duration as a readable Go literal that
// round-trips exactly.
func TestDurationLiteral(t *testing.T) {
	cases := []struct {
		d    string // parsed by time.ParseDuration
		want string
	}{
		{"150ms", "150 * time.Millisecond"},
		{"1s", "1000 * time.Millisecond"},
		{"2.5ms", "2500 * time.Microsecond"},
	}
	for _, c := range cases {
		d, err := time.ParseDuration(c.d)
		if err != nil {
			t.Fatalf("parse %s: %v", c.d, err)
		}
		if got := durationLiteral(d); got != c.want {
			t.Errorf("durationLiteral(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// colorSwatchValue normalises a CSS colour value into #rrggbb for the
// <input type="color"> picker, falling back to #000000 for values the
// picker cannot represent (oklch, color-mix, var()).
func TestColorSwatchValue(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"#4F46E5", "#4F46E5"},
		{"#FFF", "#FFFFFF"},
		{"#FFFFFFFF", "#FFFFFF"},
		{"oklch(0.5 0.2 240)", "#000000"},
		{"var(--color-primary)", "#000000"},
	}
	for _, c := range cases {
		if got := colorSwatchValue(c.in); got != c.want {
			t.Errorf("colorSwatchValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// frameFriendlyWriter strips X-Frame-Options and widens frame-ancestors so
// the preview iframe can load a UIHost-served page.
func TestFrameFriendlyWriterStripsHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		_, _ = w.Write([]byte("ok"))
	})
	rec := httptest.NewRecorder()
	fw := &frameFriendlyWriter{rw: rec}
	inner.ServeHTTP(fw, httptest.NewRequest(http.MethodGet, "/preview", nil))
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Errorf("X-Frame-Options not stripped: %q", rec.Header().Get("X-Frame-Options"))
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("frame-ancestors not widened to 'self': %q", csp)
	}
}

// The gallery preview screen renders gallery Demo closures + the contrast
// probes. This exercises the same render path the UIHost serves at /preview.
func TestThemeEditGalleryPreviewRenders(t *testing.T) {
	screen := &galleryPreviewScreen{}
	html := string(screen.Render())
	rendered := html + previewChromeCSS

	// The preview's own structure is design-system output. A page whose job is
	// to show what the design system looks like must not lay itself out with
	// anything else.
	if !strings.Contains(html, `data-fui-comp="ui-section"`) {
		t.Errorf("preview does not use ui.Section for its groups:\n%s", truncate(html, 400))
	}
	if !strings.Contains(html, `data-fui-comp="ui-layout"`) {
		t.Errorf("preview does not use ui.Stack for its rhythm:\n%s", truncate(html, 400))
	}
	for _, gone := range []string{"tp-category", "tp-demo", "tp-gallery", "tp-preview"} {
		if strings.Contains(rendered, gone) {
			t.Errorf("preview still carries the hand-rolled %q hook", gone)
		}
	}

	// The contrast probes must be present so the JS can measure them.
	if !strings.Contains(html, "data-cp=") {
		t.Error("preview missing contrast-probe elements")
	}
	if !strings.Contains(html, "text-subtle|surface") {
		t.Error("preview missing the text-subtle/surface contrast probe")
	}
	if !strings.Contains(html, "danger|danger-tint") {
		t.Error("preview missing the danger tint contrast probe")
	}
}

// TestThemeEditChromeHasNoBespokeClasses is hard rule 7's gate for the editor
// chrome: it must compose framework/ui + core-ui/app + core-ui/style, not ship
// its own stylesheet. The previous chrome carried ~25 hand-written .te-*
// classes (~85 occurrences) plus ~21 hardcoded hex values in a BaseCSS string.
// This test pins the cutover: no inline <style> block, no inline style=
// attribute, and the design-system primitives (ui-workbench shell, ui-form-field
// inputs, ui-callout status surface, ui-button) are present.
//
// It does NOT police the JS string for ".te-*" mentions or the inputs'
// value="..." attributes: the JS legitimately mentions the old class name in a
// comment explaining what the new code does NOT do, and the inputs legitimately
// carry the theme's hex colour values (those are data the operator edits, not
// styling the chrome owns).
func TestThemeEditChromeHasNoBespokeClasses(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.serveControlsPage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// ZERO bespoke CSS: no inline <style> block ships in the chrome page. The
	// previous themeEditChromeCSS const fed a <style> tag with ~25 .te-*
	// rules + ~21 hardcoded hexes; the cutover moves every visual treatment
	// into /__gofastr/app.css.
	if strings.Contains(body, "<style") {
		t.Errorf("chrome ships an inline <style> block; all styling must come from /__gofastr/app.css")
	}
	// No inline style="..." attribute either: the design system's variants
	// drive every visual state through class/data-fui-comp markers.
	if loc := regexp.MustCompile(`style="[^"]*"`).FindStringIndex(body); loc != nil {
		t.Errorf("chrome has an inline style=\"…\" attribute at byte %d — bespoke CSS by another name:\n...%s...",
			loc[0], truncate(body[loc[0]:], 200))
	}

	// The chrome must be pinned to the framework's default theme, linking
	// /__gofastr/app.css with no ?t= query, so the controls stay usable even
	// when the operator sets --color-text: transparent in the working theme.
	// (The working theme only lives in the preview iframe, which swaps to
	// /__gofastr/app.css?t=<hash> via swapPreviewCSS.)
	if !strings.Contains(body, `href="/__gofastr/app.css"`) {
		t.Errorf("chrome does not link /__gofastr/app.css (default theme); cannot be immune to the working theme:\n%s",
			truncate(body, 400))
	}
	if strings.Contains(body, `href="/__gofastr/app.css?t=`) {
		t.Errorf("chrome links a variant app.css (?t=…); it must use the framework default theme, not the working one")
	}

	// Positive presence checks: the chrome composes these design-system
	// primitives. Cutover is incomplete without them.
	mustHave := []string{
		`data-fui-comp="ui-workbench"`,    // framework/ui two-pane inspector shell
		`data-fui-comp="ui-form-field"`,   // design-system input rows
		`data-fui-comp="ui-callout"`,      // status + contrast panels
		`data-fui-comp="ui-button"`,       // scheme toggle + Write button
		`data-fui-comp="fui-collapsible"`, // token groups
		`data-fui-comp="ui-color-field"`,  // swatch + text input row
	}
	for _, want := range mustHave {
		if !strings.Contains(body, want) {
			t.Errorf("chrome missing design-system marker %q", want)
		}
	}
}

// The contrast checker's probes must carry their colours in a STYLESHEET.
//
// They used to use an inline style attribute, which the framework's default CSP
// (default-src 'self', no 'unsafe-inline') discards. Every probe then measured
// the inherited text colour against a transparent background, every ratio came
// out around 20:1, and the tool reported "no issues" for every theme it was
// ever pointed at. A checker that cannot fail is an assurance, not a check.
func TestThemeEditContrastProbesAreNotInlineStyled(t *testing.T) {
	html := string(contrastProbeHTML())
	if strings.Contains(html, "style=") {
		t.Fatalf("contrast probes carry an inline style attribute, which CSP drops — every measurement would read transparent:\n%s", html)
	}

	css := contrastProbeCSS()
	if len(contrastPairs) == 0 {
		t.Fatal("no contrast pairs declared")
	}
	for _, p := range contrastPairs {
		if !strings.Contains(html, "tp-probe--"+p.slug) {
			t.Errorf("no probe element for pair %q", p.name)
		}
		if !strings.Contains(css, ".tp-probe--"+p.slug+" {") {
			t.Errorf("no CSS rule for pair %q — the probe would measure nothing", p.name)
		}
		if !strings.Contains(css, p.fg) || !strings.Contains(css, p.bg) {
			t.Errorf("pair %q rule does not carry both colours", p.name)
		}
	}
	// The pair the theme docs call the harder target has to be in there.
	if !strings.Contains(css, "color-mix(in srgb, var(--color-danger) 15%") {
		t.Error("the status-tone-on-its-own-tint pair is missing from the probe CSS")
	}
}

// The /__theme/tokens endpoint returns the flat token map as JSON.
func TestThemeEditTokensEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__theme/tokens", nil)
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("tokens: status %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "color-primary") {
		t.Errorf("tokens response missing color-primary:\n%s", truncate(body, 400))
	}
	if !strings.Contains(body, `"type":"color"`) {
		t.Errorf("tokens response missing type classification:\n%s", truncate(body, 400))
	}
}

// The happy path: POST /__theme/apply with a valid token returns a hash.
func TestThemeEditApplyHappyPath(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/apply",
		strings.NewReader(`{"key":"color-primary","value":"#00CC66"}`))
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	req.Header.Set("Origin", "http://127.0.0.1:0")
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("apply: status %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"hash"`) {
		t.Errorf("apply response missing hash:\n%s", rec.Body.String())
	}
}

// A cross-origin POST is rejected even with a valid bearer token.
func TestThemeEditCheckOriginRejectsForeignOrigin(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/apply",
		strings.NewReader(`{"key":"color-primary","value":"#000"}`))
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	req.Header.Set("Origin", "http://evil.example.com")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin apply: status %d, want 403", rec.Code)
	}
}

// An apply with a malformed JSON body is a 400, not a 500.
func TestThemeEditApplyMalformedBody(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/apply", strings.NewReader(`not json`))
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed apply: status %d, want 400", rec.Code)
	}
}

// An apply with an invalid value surfaces the ApplyTokens error as JSON.
func TestThemeEditApplyInvalidValueJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__theme/apply",
		strings.NewReader(`{"key":"color-primary","value":"red;}"}`))
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid apply: status %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("invalid apply response missing error field:\n%s", rec.Body.String())
	}
}

// An unknown /__theme/* path returns 404.
func TestThemeEditUnknownAPIPath(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__theme/nope", nil)
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown API path: status %d, want 404", rec.Code)
	}
}

// A GET on the writeback endpoint is method-not-allowed.
func TestThemeEditWritebackMethodGuard(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/__theme/writeback", nil)
	req.Host = "127.0.0.1:0"
	req.Header.Set("Authorization", "Bearer "+srv.token)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET writeback: status %d, want 405", rec.Code)
	}
}

// The full ServeHTTP routing delegates /preview to the UIHost, which
// renders the gallery screen with full chrome (app.css link present).
func TestThemeEditServeHTTPDelegatesPreviewToHost(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview", nil)
	req.Host = "127.0.0.1:0"
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("preview: status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// The UIHost renders the page with the app.css link.
	if !strings.Contains(body, "/__gofastr/app.css") {
		t.Errorf("preview page missing app.css link — the live-swap path depends on it:\n%s", truncate(body, 400))
	}
	// X-Frame-Options must be stripped so the iframe can embed it.
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Errorf("preview page still carries X-Frame-Options: %q", rec.Header().Get("X-Frame-Options"))
	}
}

// sortStrings is a tiny insertion sort; verify it orders correctly.
func TestSortStrings(t *testing.T) {
	in := []string{"c", "a", "b"}
	sortStrings(in)
	want := []string{"a", "b", "c"}
	for i, v := range want {
		if in[i] != v {
			t.Errorf("sortStrings: got %v, want %v", in, want)
			break
		}
	}
}

// A wildcard bind must still accept the Host a browser actually sends.
//
// net.Listen(":8090") reports "[::]:8090", and pinning the guard to that
// literal made every request 403, including from the URL the tool prints, and
// from the `--addr=:8090` invocation the theming docs give as an example. The
// tool was unusable in its own documented form.
func TestLoopbackGuardsAcceptAWildcardBind(t *testing.T) {
	for _, addr := range []string{"[::]:8090", "0.0.0.0:8090", ":8090"} {
		hosts, origins := loopbackGuards(addr)
		joined := strings.Join(hosts, " ")
		for _, want := range []string{"localhost:8090", "127.0.0.1:8090"} {
			if !strings.Contains(joined, want) {
				t.Errorf("loopbackGuards(%q) hosts = %v, missing %q", addr, hosts, want)
			}
		}
		if !hostAllowed("localhost:8090", hosts) {
			t.Errorf("loopbackGuards(%q): a browser's Host would be refused", addr)
		}
		if len(origins) == 0 {
			t.Errorf("loopbackGuards(%q) produced no origins", addr)
		}
	}

	// An explicit non-loopback bind stays pinned to exactly what was asked for.
	hosts, _ := loopbackGuards("192.168.1.10:8090")
	if len(hosts) != 1 || hosts[0] != "192.168.1.10:8090" {
		t.Errorf("an explicit host must stay pinned, got %v", hosts)
	}
}

// A refresh mid-session must show what the server holds, not what it started
// with. The JSON endpoint always read the working theme; only the HTML render
// disagreed, so the swatches and the preview both lied and Write emitted values
// the operator could not see.
func TestThemeEditControlsPageShowsTheWorkingTheme(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.applyToken("color-primary", "#FF0000"); err != nil {
		t.Fatalf("applyToken: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = srv.hosts[0]
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#FF0000") {
		t.Errorf("the controls page does not show the edited value:\n%s", truncate(body, 400))
	}
	if !strings.Contains(body, `name="theme-edit-variant"`) {
		t.Errorf("the page does not carry the working theme's variant for the preview to adopt:\n%s", truncate(body, 400))
	}

	// And the page that carries the bearer token must not be framable.
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY — otherwise any page the developer has open can clickjack Write", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q, want frame-ancestors 'none'", csp)
	}
}

// The package clause is the one string the emitter writes as an IDENTIFIER
// rather than through %q, so a directory name that is not a Go identifier used
// to make Write fail every time, with a 500 carrying the entire generated file
// and nothing written to disk.
func TestPackageNameForPathIsAlwaysALegalIdentifier(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		"design-system/theme.go": "design_system",
		"my.theme/theme.go":      "my_theme",
		"2tokens/theme.go":       "_2tokens",
		"type/theme.go":          "type_",
		"theme/theme.go":         "theme",
		"---/theme.go":           "theme",
		"my theme/theme.go":      "my_theme",
	}
	for path, want := range cases {
		path = filepath.Join(root, path)
		got := packageNameForPath(path)
		if got != want {
			t.Errorf("packageNameForPath(%q) = %q, want %q", path, got, want)
		}
		// Whatever it returns has to actually parse as a package clause.
		if _, err := parser.ParseFile(gotoken.NewFileSet(), "x.go", "package "+got+"\n", 0); err != nil {
			t.Errorf("packageNameForPath(%q) = %q, which does not parse: %v", path, got, err)
		}
	}
}

// Attribute values are HTML, not Go literals. %q renders a double quote as \",
// which HTML does not treat as an escape; the attribute simply ends early. Not
// reachable with the default theme, which is exactly why it is a trap: the
// label two lines above used htmlEscape correctly while the value beside it
// did not.
func TestThemeEditControlsEscapeAttributeValues(t *testing.T) {
	controls := renderTokenControls(map[string]string{
		"font-body": `"Ivy Mode", 'Comic Sans', serif`,
		"shadow-md": `0 1px 2px rgba(0,0,0,.2) /* <script>alert(1)</script> */`,
	})
	if strings.Contains(controls, `\"`) {
		t.Errorf("a value was emitted with a Go-style escape, which HTML ignores:\n%s", truncate(controls, 400))
	}
	if strings.Contains(controls, "<script>") {
		t.Errorf("an unescaped tag reached the markup:\n%s", truncate(controls, 400))
	}
	if !strings.Contains(controls, "&quot;Ivy Mode&quot;") {
		t.Errorf("the quoted font stack was not HTML-escaped:\n%s", truncate(controls, 400))
	}
}

// Every debounced keystroke registers a theme variant, and the registry has no
// eviction of its own. Only the current variant is ever served, so holding a
// full style.Theme per edit for the life of the session is pure accumulation.
func TestThemeEditReleasesSupersededVariants(t *testing.T) {
	srv := newTestServer(t)
	for i, hex := range []string{"#111111", "#222222", "#333333", "#444444", "#555555"} {
		if _, err := srv.applyToken("color-primary", hex); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	if got := srv.host.ThemeVariantCount(); got > 1 {
		t.Fatalf("the theme registry holds %d variants after 5 sequential edits — superseded ones are never released", got)
	}
}

// injectEveryThemeString walks a struct and writes payload into every settable
// string field, however deeply nested. Reflection rather than a hand-written
// list, so the injection test covers fields nobody has written yet.
func injectEveryThemeString(v reflect.Value, payload string) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(payload)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			injectEveryThemeString(v.Field(i), payload)
		}
	case reflect.Ptr:
		if !v.IsNil() {
			injectEveryThemeString(v.Elem(), payload)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			injectEveryThemeString(v.Index(i), payload)
		}
	}
}

// The reflection walk has to actually reach the fields it claims to. A walk
// that silently visited nothing would make the injection test vacuous, the
// exact failure mode the hand-written list had, just harder to see.
func TestInjectEveryThemeStringReachesNestedFields(t *testing.T) {
	th := uitheme.Default()
	injectEveryThemeString(reflect.ValueOf(&th).Elem(), "SENTINEL")
	for name, got := range map[string]string{
		"Colors.Primary.Value":  th.Colors.Primary.Value,
		"Fonts.Body.Value":      th.Fonts.Body.Value,
		"Shadows.MD.Value":      th.Shadows.MD.Value,
		"Easings.Spring.Value":  th.Easings.Spring.Value,
		"Typography.Base.Value": th.Typography.Base.Value,
		"Code.KW.Value":         th.Code.KW.Value,
		"Name":                  th.Name,
	} {
		if got != "SENTINEL" {
			t.Errorf("%s = %q — the walk did not reach it", name, got)
		}
	}
}

// The theme editor serves an unauthenticated page carrying its own bearer token
// and writes a Go file to disk, so it must not leave the machine. A Host pin
// stops a browser rebinding DNS onto the port; it does not stop a direct TCP
// client, which picks its own Host, fetches "/", and reads the token out of the
// markup. Same reasoning as the dev MCP bind guard.
func TestThemeEditAddrIsConfinedToLoopback(t *testing.T) {
	// The friendly "this machine" spellings resolve to loopback rather than
	// binding every interface.
	for _, in := range []string{":8090", "0.0.0.0:8090", "[::]:8090"} {
		got := loopbackifyThemeAddr(in)
		host, _, err := net.SplitHostPort(got)
		if err != nil {
			t.Fatalf("loopbackifyThemeAddr(%q) = %q, which is not host:port", in, got)
		}
		if !isLoopbackHost(host) {
			t.Errorf("loopbackifyThemeAddr(%q) = %q — a wildcard bind must resolve to loopback", in, got)
		}
	}
	// An explicit address is left alone, so the caller can refuse it.
	if got := loopbackifyThemeAddr("192.168.1.10:8090"); got != "192.168.1.10:8090" {
		t.Errorf("an explicit address was rewritten: %q", got)
	}
	if got := loopbackifyThemeAddr("127.0.0.1:0"); got != "127.0.0.1:0" {
		t.Errorf("the default was rewritten: %q", got)
	}
}

func TestWriteBackRefusesLateFile(t *testing.T) {
	srv := newTestServer(t)
	const existing = "package palette\n"
	if err := os.WriteFile(srv.outPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := srv.writeBack(); err == nil {
		t.Fatal("writeBack replaced a file created after startup without --force")
	}
	got, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("late-created file changed to:\n%s", got)
	}
}

func TestWriteBackReplacesAtomically(t *testing.T) {
	srv := newTestServer(t)
	srv.force = true
	if err := os.WriteFile(srv.outPath, []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(srv.outPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := srv.writeBack(); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	got, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "package old\n" {
		t.Fatal("writeBack left the old destination contents in place")
	}
	after, err := os.Stat(srv.outPath)
	if err != nil {
		t.Fatal(err)
	}
	// Windows' replacement rename may preserve the destination file identity
	// while still replacing it atomically. The content assertion below is the
	// portable contract; Unix additionally exposes a distinct inode.
	if runtime.GOOS != "windows" && os.SameFile(before, after) {
		t.Fatal("writeBack truncated the destination in place instead of atomically replacing it")
	}
}

func TestWriteBackUsesSiblingPackage(t *testing.T) {
	srv := newTestServer(t)
	dir := filepath.Dir(srv.outPath)
	if err := os.WriteFile(filepath.Join(dir, "palette.go"), []byte("package palette\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := srv.writeBack(); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	src, err := os.ReadFile(srv.outPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(gotoken.NewFileSet(), srv.outPath, src, parser.PackageClauseOnly)
	if err != nil {
		t.Fatalf("parse generated package: %v", err)
	}
	if file.Name.Name != "palette" {
		t.Fatalf("generated package = %q beside package palette", file.Name.Name)
	}
}

func TestContrastCompositesAlpha(t *testing.T) {
	for _, want := range []string{
		"return [d[0], d[1], d[2], d[3] / 255]",
		"function composite(",
		"var renderedFg = composite(",
	} {
		if !strings.Contains(themeEditChromeJS, want) {
			t.Errorf("contrast script does not composite RGBA values; missing %q", want)
		}
	}
}

func TestApplySameValueReleasesVariant(t *testing.T) {
	srv := newTestServer(t)
	for _, value := range []string{"#111111", "#111111", "#222222"} {
		if _, err := srv.applyToken("color-primary", value); err != nil {
			t.Fatalf("apply %s: %v", value, err)
		}
	}
	if got := srv.host.ThemeVariantCount(); got != 1 {
		t.Fatalf("theme registry holds %d variants after a duplicate edit, want 1", got)
	}
}

func TestThemeAppliesStayOrdered(t *testing.T) {
	for _, want := range []string{
		"var applyQueue = Promise.resolve();",
		"applyQueue = applyQueue.then(function()",
	} {
		if !strings.Contains(themeEditChromeJS, want) {
			t.Errorf("apply script does not serialize requests; missing %q", want)
		}
	}
}

func TestControlKeyEscapesEveryAttribute(t *testing.T) {
	controls := string(renderOneControl(tokenControl{
		Key:   `color-a"b<c`,
		Value: "#000000",
		Type:  "color",
	}))
	if strings.Contains(controls, `data-err-for="color-a"b<c"`) {
		t.Fatalf("error target contains an unescaped token key:\n%s", controls)
	}
	if !strings.Contains(controls, `data-err-for="color-a&quot;b&lt;c"`) {
		t.Fatalf("error target does not contain the escaped token key:\n%s", controls)
	}
}

func TestThemeSelectorsHandleQuotes(t *testing.T) {
	if strings.Contains(themeEditChromeJS, "cssEsc(") {
		t.Fatal("theme script still interpolates token keys into CSS selectors")
	}
	if !strings.Contains(themeEditChromeJS, "getAttribute(attr) === key") {
		t.Fatal("theme script does not match author-supplied token keys as attribute values")
	}
}

func TestThemeEditCSPExemptionIsLocal(t *testing.T) {
	page, err := os.ReadFile("theme_edit_page.go")
	if err != nil {
		t.Fatal(err)
	}
	server, err := os.ReadFile("theme_edit.go")
	if err != nil {
		t.Fatal(err)
	}
	pageText := strings.ReplaceAll(string(page), "\r\n", "\n")
	serverText := strings.ReplaceAll(string(server), "\r\n", "\n")
	if !strings.HasPrefix(pageText, "// check-csp:ignore-file\n") {
		t.Error("theme_edit_page.go lacks the exemption for its inline script and style")
	}
	if strings.HasPrefix(serverText, "// check-csp:ignore-file\n") {
		t.Error("theme_edit.go carries a CSP exemption despite emitting no inline script or style")
	}
}

func TestColorProbeUsesTwoSentinels(t *testing.T) {
	for _, want := range []string{"#010203", "#040506"} {
		if !strings.Contains(themeEditChromeJS, want) {
			t.Errorf("color parser lacks sentinel %s", want)
		}
	}
	if strings.Contains(themeEditChromeJS, "v !== '#000000'") {
		t.Fatal("color parser still rejects valid black spellings through a denylist")
	}
}

func TestGalleryCSSKeepsSpacingLive(t *testing.T) {
	css := gallery.BaseCSS(uitheme.Default())
	for _, variable := range []string{"var(--spacing-md)", "var(--spacing-xl)"} {
		if !strings.Contains(css, variable) {
			t.Errorf("gallery CSS freezes spacing instead of using %s:\n%s", variable, css)
		}
	}
}

func TestThemeHelpRoutesLocally(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"theme", "--help"}, "Usage: gofastr theme <subcommand>"},
		{[]string{"theme", "edit", "--help"}, "Usage: gofastr theme edit"},
		{[]string{"semantic", "--help"}, "semantic search for the project at cwd"},
	}
	for _, tc := range cases {
		out := covT_capStdout(t, func() { dispatch(tc.args) })
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v printed the wrong help:\n%s", tc.args, out)
		}
		if strings.Contains(out, "Start dev server with auto-restart") {
			t.Errorf("%v fell back to global help:\n%s", tc.args, out)
		}
	}

	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { dispatch([]string{"theme", "edit", "help"}) })
	})
	if code != -1 {
		t.Errorf("theme edit help exited %d, want a normal return", code)
	}
	if !strings.Contains(out, "Usage: gofastr theme edit") || strings.Contains(out, "Unknown flag") {
		t.Errorf("theme edit help printed the wrong output:\n%s", out)
	}
}

// The written file has to survive the validation the app performs at boot.
// ApplyTokens is not that boundary: its spacing/radius/z-index setters accept
// 0 while Theme.Validate rejects it, so one keystroke in a number field
// produced a green "updated", a written theme.go, and a panic on next run.
func TestApplyRefusesAThemeThatWouldPanicAtBoot(t *testing.T) {
	srv := newTestServer(t)

	for _, bad := range []struct{ key, value string }{
		{"spacing-md", "0px"},
		{"radii-sm", "0px"},
	} {
		if _, err := srv.applyToken(bad.key, bad.value); err == nil {
			t.Errorf("%s=%s was accepted; app.WithTheme calls MustValidate, so this "+
				"writes a theme.go that panics the operator's app at boot", bad.key, bad.value)
		}
	}
	// A legitimate edit still applies, or the guard has simply broken the tool.
	if _, err := srv.applyToken("color-primary", "#0d9488"); err != nil {
		t.Errorf("a valid edit was refused: %v", err)
	}
}

// The bearer token must not be the harness's HMAC signing key. The controls
// page is served with no authentication, and it is where the token comes
// from, so publishing a machine-persistent signing key there hands it to any
// local process, any other user on the box, and any screenshot in a bug report.
//
// This pins the property by reading the token the SERVER ACTUALLY SERVES in
// its <meta name="theme-edit-token"> tag, the same bytes any unauthenticated
// localhost reader can lift off the page, and asserting those bytes are not
// the harness signing key. The previous test only compared two values it
// generated itself and so passed even when runThemeEdit published the signing
// key verbatim (proven by mutation: replacing newThemeEditToken with
// hex.EncodeToString(deriveListenerSecret()) left the suite green).
func TestThemeEditTokenIsNotTheHarnessSigningKey(t *testing.T) {
	// Pin the signing key to a known deterministic value: with the machine
	// key set, deriveListenerSecret returns sha256("harness-http:" || key),
	// which is the HMAC key signing every harness control-plane token.
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", strings.Repeat("ab", 32))
	signingKey := hex.EncodeToString(deriveListenerSecret())

	srv := newTestServer(t)
	tok, err := newThemeEditToken()
	if err != nil {
		t.Fatalf("newThemeEditToken: %v", err)
	}
	srv.token = tok

	rec := httptest.NewRecorder()
	srv.serveControlsPage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	// The page must serve the server's own freshly generated token. Attribute
	// order in the meta tag is not guaranteed (render.writeAttrs sorts keys
	// alphabetically), so assert the name= and content= pair separately.
	if !strings.Contains(body, `name="theme-edit-token"`) {
		t.Fatalf("server did not emit <meta name=\"theme-edit-token\">; served:\n%s", truncate(body, 400))
	}
	if !strings.Contains(body, `content="`+tok+`"`) {
		t.Fatalf("server did not serve its own generated token %q; served:\n%s",
			tok, truncate(body, 400))
	}

	// The load-bearing assertion: the bytes the page serves must not be the
	// harness signing key. The exact regression the comment at the call site
	// describes is runThemeEdit setting srv.token = hex(deriveListenerSecret()),
	// publishing the HMAC key on an unauthenticated page.
	if tok == signingKey {
		t.Fatalf("the theme editor's served bearer token equals the harness signing key.\n" +
			"runThemeEdit must use freshly random bytes (newThemeEditToken), " +
			"not deriveListenerSecret().")
	}

	// And the token must be per-session random, not a constant or a derived
	// value the way the signing key is.
	tok2, err := newThemeEditToken()
	if err != nil {
		t.Fatalf("second newThemeEditToken: %v", err)
	}
	if tok == tok2 {
		t.Errorf("theme edit token is not per-session random: two calls returned the same value %q", tok)
	}
}
