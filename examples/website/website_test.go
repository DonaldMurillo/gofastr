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
// registry covers every page promised in the README.
func TestSetupServerWiresAllExpectedRoutes(t *testing.T) {
	_, host := setupServer()
	want := map[string]bool{
		"/":            false,
		"/docs/":       false,
		"/docs/:slug":  false,
		"/examples/":   false,
		"/about":       false,
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
