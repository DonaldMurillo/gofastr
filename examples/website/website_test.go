package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/framework/static"
)

// TestSetupServerWiresAllExpectedRoutes ensures the website's screen
// registry covers every page promised in the README, including the
// component demos and the framework/ui dogfood page.
func TestSetupServerWiresAllExpectedRoutes(t *testing.T) {
	_, host := setupServer()
	want := map[string]bool{
		"/":                       false,
		"/docs/":                  false,
		"/docs/:slug":             false,
		"/examples/":              false,
		"/about":                  false,
		"/components/":            false,
		"/components/accordion":   false,
		"/components/tabs":        false,
		"/components/progress":    false,
		"/components/skeleton":    false,
		"/components/breadcrumbs": false,
		"/components/pagination":  false,
		"/framework-ui/":          false,
	}
	for _, route := range host.App.Routes() {
		if _, ok := want[route.Path]; ok {
			want[route.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("route %q is not registered", path)
		}
	}
}

// TestComponentDemosRenderWithoutPanic walks every component demo
// page and asserts the live demo HTML contains the expected
// component-class signature. Catches regressions where a screen
// compiles but emits empty or wrong markup.
func TestComponentDemosRenderWithoutPanic(t *testing.T) {
	cases := []struct {
		path     string
		mustHave []string
	}{
		{"/components/accordion", []string{`accordion-group`, `accordion-stack`, `accordion-summary`}},
		{"/components/tabs", []string{`class="tabs"`, `tabs-summary`, `tabs-panel`}},
		{"/components/progress", []string{`<progress`, `class="progress-bar"`, `value="73"`}},
		{"/components/skeleton", []string{`skeleton-line`, `skeleton-block`, `skeleton-circle`}},
		{"/components/breadcrumbs", []string{`<nav aria-label="Breadcrumb">`, `breadcrumbs`, `aria-current="page"`}},
		{"/components/pagination", []string{`<nav aria-label="Pagination">`, `pagination`, `aria-current="page"`}},
		{"/framework-ui/", []string{
			`ui-page-header`, `ui-stat-card`, `ui-badge--success`,
			`ui-callout--danger`, `ui-form-section`, `ui-empty-state`,
			`ui-button--danger`, `ui-avatar`,
		}},
	}

	app, _ := setupServer()
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, rec.Code)
			continue
		}
		body := rec.Body.String()
		for _, want := range tc.mustHave {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q in body", tc.path, want)
			}
		}
	}
}

// TestLivereloadEndpointsServe confirms the dev-mode livereload pair
// is wired correctly when GOFASTR_DEV=1: the JS asset serves a script
// that fetches the long-poll endpoint, and every rendered page links
// to that asset. With GOFASTR_DEV unset, the endpoints are absent.
func TestLivereloadEndpointsServe(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	app, _ := setupServer()

	req := httptest.NewRequest("GET", "/__livereload.js", nil)
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/__livereload.js status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "fetch('/__livereload')") {
		t.Errorf("expected livereload script to fetch /__livereload")
	}

	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	app.Router.ServeHTTP(pageRec, pageReq)
	if !strings.Contains(pageRec.Body.String(), `src="/__livereload.js"`) {
		t.Errorf("page missing livereload script tag")
	}
}

// TestLivereloadGatedByDevMode confirms the absence of /__livereload.js
// when GOFASTR_DEV is unset — production-mode behavior.
func TestLivereloadGatedByDevMode(t *testing.T) {
	// Make sure we run with the env var unset.
	t.Setenv("GOFASTR_DEV", "")
	app, _ := setupServer()

	req := httptest.NewRequest("GET", "/__livereload.js", nil)
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/__livereload.js without GOFASTR_DEV expected 404, got %d", rec.Code)
	}

	pageReq := httptest.NewRequest("GET", "/", nil)
	pageRec := httptest.NewRecorder()
	app.Router.ServeHTTP(pageRec, pageReq)
	if strings.Contains(pageRec.Body.String(), `src="/__livereload.js"`) {
		t.Errorf("page should NOT include livereload script when GOFASTR_DEV is unset")
	}
}

// TestSSGProducesExpectedFilesAndContent runs the SSG end-to-end against the
// real website setup and asserts on directory layout plus selected page
// content. This is the closest thing to a deploy-ready check we have without
// shipping to GitHub Pages.
func TestSSGProducesExpectedFilesAndContent(t *testing.T) {
	_, host := setupServer()
	out := t.TempDir()

	res, err := (&static.Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Static pages.
	for _, rel := range []string{
		"index.html",
		"docs/index.html",
		"examples/index.html",
		"about/index.html",
	} {
		assertFileNonEmpty(t, filepath.Join(out, rel))
	}

	// At least one Markdown-driven doc page exists. We don't pin the slug
	// list here — that's the point of StaticPaths: it follows the docs/
	// directory.
	docsRoot := filepath.Join(out, "docs")
	matches, err := filepath.Glob(filepath.Join(docsRoot, "*", "index.html"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no /docs/<slug>/index.html files were produced")
	}

	// Pick one well-known doc and assert markdown actually rendered.
	migrations := filepath.Join(out, "docs", "migrations", "index.html")
	data, err := os.ReadFile(migrations)
	if err != nil {
		t.Fatalf("missing migrations doc: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		`<title>Migrations — GoFastr</title>`,
		`<h1 id="migrations">`,
		`class="language-sql"`, // fenced code block survived rendering
		`gofastr migrate`,       // body content survived
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/docs/migrations missing %q in output", want)
		}
	}
	// SSG output must NOT include the SSE meta — sessions are a server-only concern.
	if strings.Contains(body, "gofastr-sse") {
		t.Error("/docs/migrations should not include gofastr-sse meta in static output")
	}
	// Runtime script tag must be present so client hydration works after first paint.
	if !strings.Contains(body, `<script src="/__gofastr/runtime.js">`) {
		t.Error("/docs/migrations missing runtime.js script tag")
	}

	// Runtime asset.
	rt := filepath.Join(out, "__gofastr", "runtime.js")
	assertFileNonEmpty(t, rt)

	// Result counts agree with what's on disk.
	if len(res.Pages) < 4+len(matches) {
		t.Errorf("Result.Pages count looks wrong: %d (static + %d docs)", len(res.Pages), len(matches))
	}
}

// TestLiveServerRendersAndAppliesMiddleware boots the framework.App in a
// real HTTP server (httptest), hits a Markdown doc page through the full
// router stack, and verifies both the rendered body and the default
// middleware chain (security headers + request id).
func TestLiveServerRendersAndAppliesMiddleware(t *testing.T) {
	fwApp, _ := setupServer()
	srv := httptest.NewServer(fwApp.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/docs/migrations")
	if err != nil {
		t.Fatalf("GET /docs/migrations: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("X-Request-Id is missing — request-id middleware should attach one")
	}

	body := readAll(t, resp.Body)
	if !strings.Contains(body, "<h1") || !strings.Contains(body, "Migrations") {
		t.Errorf("body did not contain expected markdown rendering:\n%s", trim(body, 200))
	}
}

// TestStrictCSPWithExternalResources pins the architectural fix that came
// out of the "no styles" report: the framework injects no inline styles
// or scripts. Theme CSS, custom CSS, route graph, runtime, and compiled
// actions are all served as separate /__gofastr/* endpoints and the page
// references them via <link>/<script src>. The default CSP can therefore
// stay strict (default-src 'self') without breaking UI rendering.
func TestStrictCSPWithExternalResources(t *testing.T) {
	fwApp, _ := setupServer()
	srv := httptest.NewServer(fwApp.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Errorf("default CSP must not require 'unsafe-inline'; got %q", csp)
	}

	body := readAll(t, resp.Body)

	// The page must NOT have inline <style>...content...</style> or inline
	// <script>...content...</script> blocks (only external src/href is OK).
	if strings.Contains(body, "<style>") {
		t.Error("page contains an inline <style> block — should be a <link rel=\"stylesheet\">")
	}
	// Inline scripts (with bodies) are forbidden, but <script src="..."> is fine.
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "<script") {
			continue
		}
		if !strings.Contains(line, "src=") && !strings.Contains(line, "</script>") {
			continue
		}
		// Form: <script>...body...</script> means a body between the tags.
		if strings.Contains(line, "<script>") {
			t.Errorf("inline <script> body found: %q", line)
		}
	}

	// And the external endpoints all must exist + 200.
	for _, endpoint := range []string{
		"/__gofastr/theme.css",
		"/__gofastr/styles.css",
		"/__gofastr/runtime.js",
		"/__gofastr/routes.js",
	} {
		r, err := http.Get(srv.URL + endpoint)
		if err != nil {
			t.Errorf("%s: %v", endpoint, err)
			continue
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", endpoint, r.StatusCode)
		}
	}
}

// TestDocCatalogResolvesPaths exercises the doc-catalog directly to pin the
// behavior screens depend on: load() must succeed, find() must round-trip
// at least one slug, and missing slugs must error.
func TestDocCatalogResolvesPaths(t *testing.T) {
	cat := &docCatalog{}
	items, err := cat.all()
	if err != nil {
		t.Fatalf("docCatalog.all: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("docCatalog scanned zero docs — is docs/ on disk?")
	}
	first := items[0]
	got, err := cat.find(first.Slug)
	if err != nil {
		t.Fatalf("find(%q): %v", first.Slug, err)
	}
	if got.Slug != first.Slug {
		t.Errorf("round-trip mismatch: %q vs %q", got.Slug, first.Slug)
	}
	if _, err := cat.find("definitely-not-a-real-slug-xyzzy"); err == nil {
		t.Error("find() should error on an unknown slug")
	}
}

func assertFileNonEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("%s is empty", path)
	}
}

func readAll(t *testing.T, r interface{ Read(p []byte) (int, error) }) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
