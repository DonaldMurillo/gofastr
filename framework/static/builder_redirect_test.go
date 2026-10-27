package static

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestExportWritesRedirectPages: a static host has no Redirect route to
// answer /old, so an inbound link to it would land on 404.html. Each
// exact redirect gets a page at its path that sends the browser on with
// a meta refresh and a plain link, under the export's own CSP (no
// script, no style), and a _redirects line for the hosts that read one
// (Netlify, Cloudflare Pages: the ones _headers already targets) to
// answer with a real 308. A pattern redirect has no single path to
// write; it is logged as skipped.
func TestExportWritesRedirectPages(t *testing.T) {
	buf := captureSlog(t)
	a := coreapp.NewApp("Redirects")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	a.Redirect("/old", "/about")
	a.Redirect("/older", "/old")              // a chain resolves to its end
	a.Redirect("/api-doc", "/api/v1?x=1&y=2") // not an exported page
	a.Redirect("/products/alpha", "/about")   // shadows one StaticPaths page, as live
	a.RedirectPattern("/legacy/:id", "/products/:id")

	out := t.TempDir()
	res, err := (&Builder{Host: uihost.New(a), OutDir: out, BasePath: "/site"}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, from := range []string{"old", "older", "products/alpha"} {
		page := readOut(t, out, from+"/index.html")
		for _, want := range []string{
			`<meta http-equiv="refresh" content="0; url=/site/about/">`,
			`href="/site/about/"`,
			`http-equiv="Content-Security-Policy"`,
			`<meta name="robots" content="noindex">`,
			`<html lang="`,
			"<title>",
		} {
			if !strings.Contains(page, want) {
				t.Errorf("%s/index.html lacks %s:\n%s", from, want, page)
			}
		}
		for _, banned := range []string{"<script", "<style", "style="} {
			if strings.Contains(page, banned) {
				t.Errorf("%s/index.html carries %s, which the export's CSP refuses:\n%s", from, banned, page)
			}
		}
	}
	if page := readOut(t, out, "api-doc/index.html"); !strings.Contains(page, `url=/site/api/v1?x=1&amp;y=2"`) {
		t.Errorf("the redirect to a non-page target lost its path or query:\n%s", page)
	}
	if slices.Contains(res.Pages, "/products/alpha") || slices.Contains(res.Pages, "/old") {
		t.Errorf("Result.Pages lists a redirected path: %v", res.Pages)
	}
	if _, err := os.Stat(filepath.Join(out, "products", "alpha", "llm.md")); !os.IsNotExist(err) {
		t.Error("llm.md written for a path a redirect takes")
	}
	if page := readOut(t, out, "products/beta/index.html"); strings.Contains(page, `http-equiv="refresh"`) {
		t.Error("the unshadowed StaticPaths page became a redirect")
	}
	rules := readOut(t, out, "_redirects")
	for _, want := range []string{
		"/site/old /site/about/ 308\n",
		"/site/older /site/about/ 308\n",
		"/site/products/alpha /site/about/ 308\n",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("_redirects lacks %q:\n%s", want, rules)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "legacy")); !os.IsNotExist(err) {
		t.Error("a pattern redirect wrote a file")
	}
	if logged := buf.String(); !strings.Contains(logged, "/legacy/:id") {
		t.Errorf("the skipped pattern redirect was not logged:\n%s", logged)
	}
}

// TestExportNoRedirectsNoRulesFile: an app without redirects leaves no
// _redirects, not even an earlier build's in a reused OutDir, whose
// rules would keep redirecting on the host.
func TestExportNoRedirectsNoRulesFile(t *testing.T) {
	a := coreapp.NewApp("NoRedirects")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "_redirects"), []byte("/ /gone 308\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "_redirects")); !os.IsNotExist(err) {
		t.Error("_redirects written for an app with no redirects")
	}
}

// TestStaticSourceIndexHTMLCopied: an index.html in the static source
// is a file like any other unless the build rendered a page at that
// path; only that one is refused, and the refusal is logged.
func TestStaticSourceIndexHTMLCopied(t *testing.T) {
	for _, viaFS := range []bool{false, true} {
		name := "dir"
		if viaFS {
			name = "fs"
		}
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			for rel, body := range map[string]string{
				"index.html":            "user root",
				"guide/index.html":      "user guide",
				"about/index.html":      "user about",
				"guide/deep/index.html": "user deep",
			} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(src, rel)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			a := coreapp.NewApp("StaticIndex")
			a.Register("/", &homeScreen{}, nil)
			a.Register("/about", &homeScreen{}, nil)
			var host *uihost.UIHost
			if viaFS {
				host = uihost.New(a)
				host.SetStaticFS(os.DirFS(src))
			} else {
				host = uihost.New(a, uihost.WithStaticDir(src))
			}
			var lines strings.Builder
			out := t.TempDir()
			b := &Builder{Host: host, OutDir: out, Logger: func(f string, args ...any) {
				fmt.Fprintf(&lines, f+"\n", args...)
			}}
			if _, err := b.Build(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := readOut(t, out, "guide/index.html"); got != "user guide" {
				t.Errorf("guide/index.html = %q, want the static source's copy", got)
			}
			if got := readOut(t, out, "guide/deep/index.html"); got != "user deep" {
				t.Errorf("guide/deep/index.html = %q, want the static source's copy", got)
			}
			for _, rel := range []string{"index.html", "about/index.html"} {
				if got := readOut(t, out, rel); strings.HasPrefix(got, "user ") {
					t.Errorf("the static source's %s replaced the rendered page", rel)
				}
			}
			if !strings.Contains(lines.String(), "/about/index.html (static source copy refused)") {
				t.Errorf("the refused index.html was not logged:\n%s", lines.String())
			}
		})
	}
}

// TestExtraDirsKeepGeneratedHostRules: an extra dir's _headers or
// _redirects at the root does not replace the files the build wrote.
// _headers carries frame-ancestors, which the page meta cannot.
func TestExtraDirsKeepGeneratedHostRules(t *testing.T) {
	a := coreapp.NewApp("HostRules")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	a.Redirect("/old", "/about")
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out, ExtraDirs: map[string]fs.FS{
		"/": fstest.MapFS{"_headers": {Data: []byte("/*\n  X-Extra: 1\n")}, "_redirects": {Data: []byte("/x /y 301\n")}},
	}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := readOut(t, out, "_headers"); !strings.Contains(got, "Content-Security-Policy:") {
		t.Errorf("an extra dir replaced the generated _headers:\n%s", got)
	}
	if got := readOut(t, out, "_redirects"); !strings.Contains(got, "/old /about/ 308") {
		t.Errorf("an extra dir replaced the generated _redirects:\n%s", got)
	}
}
