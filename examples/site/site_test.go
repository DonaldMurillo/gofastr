package main

// Regression tests for the product site. Two styles:
//   - bare-function tests over the catalogs/renderers (fast, deterministic)
//   - HTTP tests that drive setupServer().Router() like a real client
//
// These lock in the audit fixes: the /__gofastr 401 (session cookie over
// http://localhost), the embedded-markdown docs system, single-suffix
// titles, per-example snippets + source links, the real-path copy, the
// honest component labels, the 404 path echo, and the code-block changes.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// serve runs one request through the site's router and returns the recorder.
func serve(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	app := setupServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.Host = "localhost:8083" // loopback origin → dev session cookie
	app.Router().ServeHTTP(rec, req)
	return rec
}

func body(t *testing.T, target string) string {
	t.Helper()
	rec := serve(t, http.MethodGet, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: got %d, want 200", target, rec.Code)
	}
	return rec.Body.String()
}

// ── Keystone: the /__gofastr session cookie must round-trip over http
// ── localhost, or every island RPC + the SSE stream 401s. ──────────────

func TestSessionCookieIsBrowserUsableOverLocalhost(t *testing.T) {
	// We assert the cookie ATTRIBUTES, not a Go-client round-trip: Go's
	// net/http cookiejar (unlike a browser) will happily send a Secure
	// cookie over http://127.0.0.1, so a round-trip test passes even with
	// the bug. A real browser drops the Secure/__Host- cookie over plain
	// http://localhost — which was the 401-storm cause — so the guard is
	// "loopback http must mint a non-Secure, non-__Host- cookie".
	rec := serve(t, http.MethodGet, "/")
	set := rec.Header().Get("Set-Cookie")
	if !strings.Contains(set, "gofastr-session=") {
		t.Fatalf("loopback page should mint a session cookie; got %q", set)
	}
	if strings.Contains(set, "__Host-") || strings.Contains(set, "Secure") {
		t.Fatalf("loopback http cookie must not be Secure/__Host- (a browser won't return it → 401 storm): %q", set)
	}

	// The gate still holds: no cookie → 401 (not wide open).
	g := httptest.NewRecorder()
	greq := httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page=/", nil)
	greq.Host = "localhost:8083"
	setupServer().Router().ServeHTTP(g, greq)
	if g.Code != http.StatusUnauthorized {
		t.Fatalf("widgets without cookie: got %d, want 401", g.Code)
	}
}

// ── Docs: catalog ↔ embedded markdown integrity (no dead doc pages). ────

func TestEveryDocSlugMapsToEmbeddedDoc(t *testing.T) {
	for _, it := range docIntents {
		for _, d := range it.Docs {
			if _, err := docs.Get(d.Slug); err != nil {
				t.Errorf("doc card %q (%s) has no embedded doc %q.md: %v", d.Title, it.Title, d.Slug, err)
			}
		}
	}
}

func TestDocCountMatchesCatalog(t *testing.T) {
	n := 0
	for _, it := range docIntents {
		n += len(it.Docs)
	}
	if n != docCount() {
		t.Fatalf("docCount()=%d but catalog has %d", docCount(), n)
	}
	html := body(t, "/docs/")
	want := strings.Replace("X docs · 6 intents", "X", itoa(docCount()), 1)
	if !strings.Contains(html, want) {
		t.Fatalf("/docs/ header should contain %q", want)
	}
}

func TestDocIndexCardsAreLinks(t *testing.T) {
	html := body(t, "/docs/")
	for _, it := range docIntents {
		for _, d := range it.Docs {
			if !strings.Contains(html, `href="/docs/`+d.Slug+`"`) {
				t.Errorf("/docs/ missing link to /docs/%s", d.Slug)
			}
		}
	}
}

func TestDocPageRendersEmbeddedMarkdown(t *testing.T) {
	rec := serve(t, http.MethodGet, "/docs/entity-declarations")
	if rec.Code != http.StatusOK {
		t.Fatalf("/docs/entity-declarations: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ui-markdown") {
		t.Fatal("doc page should render markdown via ui.Markdown")
	}
}

// ── Titles: exactly one " — GoFastr" suffix (no doubling). ──────────────

func TestPageTitlesSingleSuffix(t *testing.T) {
	for _, path := range []string{"/", "/get-started", "/docs/", "/examples", "/kiln", "/philosophy", "/components/", "/components/button"} {
		html := body(t, path)
		title := between(html, "<title>", "</title>")
		if strings.Count(title, "— GoFastr") != 1 {
			t.Errorf("%s title %q should have exactly one '— GoFastr'", path, title)
		}
	}
}

// ── Examples: distinct snippets, source links, no static-site Serve. ────

func TestExamplesHaveSourceLinksAndAnchors(t *testing.T) {
	html := body(t, "/examples")
	for _, slug := range []string{"blog", "website", "api-tour", "embed-demo", "spa", "static-site"} {
		if !strings.Contains(html, `id="`+slug+`"`) {
			t.Errorf("/examples missing anchor id=%q", slug)
		}
		if !strings.Contains(html, "tree/main/examples/"+slug) {
			t.Errorf("/examples missing source link for %q", slug)
		}
	}
	// Static-site is "no server" — its snippet must NOT show app.Serve.
	if !strings.Contains(html, "gofastr build") {
		t.Error("/examples static-site snippet should show 'gofastr build'")
	}
}

func TestHomeExampleCardsDeepLink(t *testing.T) {
	html := body(t, "/")
	for _, slug := range []string{"blog", "website", "api-tour"} {
		if !strings.Contains(html, `href="/examples#`+slug+`"`) {
			t.Errorf("home example card should deep-link to /examples#%s", slug)
		}
	}
}

// ── 404: echoes the real path, no placeholder, no false search box. ─────

func TestNotFoundEchoesRequestedPath(t *testing.T) {
	rec := serve(t, http.MethodGet, "/no-such-page-xyz")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
	b := rec.Body.String()
	if !strings.Contains(b, "/no-such-page-xyz") {
		t.Error("404 should echo the requested path")
	}
	if strings.Contains(b, "requested-but-missing") {
		t.Error("404 still shows the hardcoded placeholder path")
	}
	if strings.Contains(b, "a search box") {
		t.Error("404 still promises a search box that doesn't exist")
	}
}

// ── Route convention: copy uses real paths, never the fictional /_/ . ───

func TestNoFictionalUnderscorePaths(t *testing.T) {
	for _, path := range []string{"/", "/get-started", "/kiln", "/components/datatable"} {
		if strings.Contains(body(t, path), "/_/") {
			t.Errorf("%s still references a fictional /_/ path", path)
		}
	}
}

func TestKilnPanelPortIsConsistent(t *testing.T) {
	for _, path := range []string{"/", "/kiln"} {
		b := body(t, path)
		if strings.Contains(b, "localhost:8080/_/kiln") || strings.Contains(b, "8080/_/kiln") {
			t.Errorf("%s references the wrong kiln port", path)
		}
	}
}

// ── Components: honest Live/Note labels + real package links. ───────────

func TestComponentDemoLabels(t *testing.T) {
	if !strings.Contains(body(t, "/components/datatable"), `demo-stage__label">Note`) {
		t.Error("note-only component (datatable) should be labeled 'Note'")
	}
	if !strings.Contains(body(t, "/components/button"), `demo-stage__label">Live`) {
		t.Error("live component (button) should be labeled 'Live'")
	}
}

func TestComponentPackageLinks(t *testing.T) {
	if got := componentPkg("button"); got != "framework/ui" {
		t.Errorf("componentPkg(button)=%q", got)
	}
	if got := componentPkg("accordion"); got != "core-ui/patterns/accordion" {
		t.Errorf("componentPkg(accordion)=%q", got)
	}
	if got := componentPkg("pipelineimage"); got != "framework/image" {
		t.Errorf("componentPkg(pipelineimage)=%q", got)
	}
	if !strings.Contains(body(t, "/components/accordion"), "pkg.go.dev/github.com/DonaldMurillo/gofastr/core-ui/patterns/accordion") {
		t.Error("accordion page should link to its real package docs")
	}
}

func TestWizardsCategoryHoldsOnlyWizards(t *testing.T) {
	for _, c := range componentCatalog {
		if c.Category == "Wizards" && c.Slug != "stepwizard" && c.Slug != "progresssteps" {
			t.Errorf("%q should not be in Wizards", c.Slug)
		}
		if c.Slug == "pipelineimage" && c.Category != "Media" {
			t.Errorf("pipelineimage should be in Media, got %q", c.Category)
		}
	}
}

// ── Code block: real copy button + contiguous gutter (#13/#14). ─────────

func TestCodeBlockHasFunctionalCopyButton(t *testing.T) {
	out := string(codeBlock("x.go", []render.HTML{ln(kw("package"), render.Text(" main"))}))
	if !strings.Contains(out, `data-fui-copy-text-from="#codeblk`) {
		t.Error("code block copy button should target its pre via data-fui-copy-text-from")
	}
	if !strings.Contains(out, `<pre class="code__body" id="codeblk`) {
		t.Error("code block pre should carry the id the copy button targets")
	}
	if !strings.Contains(out, `<button`) {
		t.Error("copy affordance should be a real <button>, not a span")
	}
}

func TestBlankCodeLineGetsLineBox(t *testing.T) {
	out := string(ln()) // blank source line
	if !strings.Contains(out, "\u200b") {
		t.Error("blank ln() should emit a zero-width space so its gutter number shows")
	}
}

func TestCodeBlockLineCountMatchesLines(t *testing.T) {
	lines := []render.HTML{ln(kw("a")), ln(), ln(kw("b"))}
	out := string(codeBlock("x.go", lines))
	if !strings.Contains(out, "3 lines") {
		t.Errorf("line count should equal len(lines)=3; got %q", firstN(out, 200))
	}
}

// ── Command palette search RPC filters server-side. ─────────────────────

func TestPaletteSearchFilters(t *testing.T) {
	hit := paletteSearch(t, "kiln")
	if !strings.Contains(hit, "/kiln") || strings.Contains(hit, "/philosophy") {
		t.Errorf("q=kiln should match only Kiln; got %q", hit)
	}
	if miss := paletteSearch(t, "zzzzz"); !strings.Contains(miss, "No matches") {
		t.Errorf("q=zzzzz should report no matches; got %q", miss)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func paletteSearch(t *testing.T, q string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__site/palette", strings.NewReader("q="+q))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	servePaletteSearch(rec, req)
	return rec.Body.String()
}

func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	j := strings.Index(s, end)
	if j < 0 {
		return s
	}
	return s[:j]
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
