package auditcmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

const contributeSrc = "package %s\n\nimport \"github.com/DonaldMurillo/gofastr/core-ui/style\"\n\nfunc init() { style.Contribute(nil) }\n"

func TestAuditDepsSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/a\n\ngo 1.22\n")
	mustWrite(t, dir, "p/p_test.go", fmt.Sprintf(contributeSrc, "p"))
	findings, err := auditDeps(dir)
	if err != nil || len(findings) != 0 {
		t.Fatalf("auditDeps = %v, %v; want no finding from a _test.go file", findings, err)
	}
}

func TestAuditDepsSortsByPackageKindFileLine(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module example.com/a\n\ngo 1.22\n")
	mustWrite(t, dir, "zeta/z.go", fmt.Sprintf(contributeSrc, "zeta"))
	mustWrite(t, dir, "alpha/c.go", fmt.Sprintf(contributeSrc, "alpha"))
	mustWrite(t, dir, "alpha/b.go", fmt.Sprintf(contributeSrc, "alpha")+"func init() { style.Contribute(nil) }\n")
	mustWrite(t, dir, "alpha/a.go", "package alpha\n\nimport \"github.com/DonaldMurillo/gofastr/core/render\"\n\nfunc init() { render.RegisterFunc(\"x\", nil) }\n"+
		"func init() { render.RegisterComponent(nil) }\n")
	findings, err := auditDeps(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, fmt.Sprintf("%s %s %s:%d", f.Pkg, f.Kind, f.File, f.Line))
	}
	want := []string{
		"example.com/a/alpha render.RegisterComponent alpha/a.go:6",
		"example.com/a/alpha render.RegisterFunc alpha/a.go:5",
		"example.com/a/alpha style.Contribute alpha/b.go:5",
		"example.com/a/alpha style.Contribute alpha/b.go:6",
		"example.com/a/alpha style.Contribute alpha/c.go:5",
		"example.com/a/zeta style.Contribute zeta/z.go:5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("findings out of order:\n got %q\nwant %q", got, want)
	}
}

func TestRunAuditDepsListsUnmountedCopies(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module audittest\n\ngo 1.21\n")
	mustWrite(t, dir, "gofastr.settings.yml", "unmounted:\n  - source: github.com/DonaldMurillo/gofastr/core/yaml\n    version: v1.2.3\n    path: internal/yaml\n    date: 2026-11-01\n")
	out := clitest.CovT_capStdout(t, func() { RunAudit([]string{"deps", dir}) })
	if !strings.Contains(out, "github.com/DonaldMurillo/gofastr/core/yaml v1.2.3 at internal/yaml (unmounted 2026-11-01)") {
		t.Fatalf("deps report does not list the unmounted copy:\n%s", out)
	}
}

func TestA11yEmailAndPasswordComeTogether(t *testing.T) {
	for _, args := range [][]string{
		{"--url=http://127.0.0.1:1", "--email=a@example.test"},
		{"--url=http://127.0.0.1:1", "--password=-"},
	} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capOutput(t, func() { runAuditA11y(args) })
		})
		if code != 2 {
			t.Errorf("runAuditA11y(%q) exit = %d, want 2", args, code)
		}
	}
}

func TestA11yLiteralPasswordWarns(t *testing.T) {
	var got string
	stderr := clitest.CovT_capStderr(t, func() { got = resolveAuditPassword("hunter2") })
	if got != "hunter2" || !strings.Contains(stderr, "visible to every local process") {
		t.Fatalf("resolveAuditPassword(literal) = %q, stderr %q; want the literal and a warning", got, stderr)
	}
}

func TestA11ySitemapIndexReadsEachPartOnce(t *testing.T) {
	reads := map[string]int{}
	inner := sitemapFixture(map[string]string{
		"/sitemap.xml":   `<sitemapindex><sitemap><loc>/s/1.xml</loc></sitemap><sitemap><loc>https://example.com/s/1.xml</loc></sitemap></sitemapindex>`,
		"/s/1.xml":       `<urlset><url><loc>/a</loc></url></urlset>`,
		"/sitemap/x.xml": `<urlset/>`,
	})
	pages, err := discoverA11yPages(func(path string) (a11ySitemap, error) {
		reads[path]++
		return inner(path)
	})
	if err != nil || !slices.Equal(pages, []string{"/a"}) || reads["/s/1.xml"] != 1 {
		t.Fatalf("pages %v, err %v, reads %v; want /s/1.xml read once", pages, err, reads)
	}
}

func TestA11ySitemapPartMustBeAURLSet(t *testing.T) {
	read := sitemapFixture(map[string]string{
		"/sitemap.xml": `<sitemapindex><sitemap><loc>/s/1.xml</loc></sitemap></sitemapindex>`,
		"/s/1.xml":     `<sitemapindex><sitemap><loc>/s/2.xml</loc></sitemap></sitemapindex>`,
	})
	if pages, err := discoverA11yPages(read); err == nil || !strings.Contains(err.Error(), "expected a URL set") {
		t.Fatalf("discoverA11yPages = %v, %v; want a nested index refused", pages, err)
	}
}

func TestAuditA11yURLCountsRedirectsAsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("headless-Chrome axe scan skipped in -short")
	}
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", t.TempDir())
	page := `<!DOCTYPE html><html lang="en"><head><title>T</title></head><body><main><h1>T</h1><p>one</p><p>two</p><p>three</p><p>four</p></main></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<urlset><url><loc>/private</loc></url><url><loc>/login</loc></url></urlset>`)
		case "/private":
			http.Redirect(w, r, "/login", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page)
		}
	}))
	defer srv.Close()
	var code int
	clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { runAuditA11y([]string{"--url", srv.URL}) })
	})
	if code != 1 {
		t.Errorf("a run with a redirected page exited %d, want 1", code)
	}
	run, err := auditA11yURL(srv.URL, nil, a11yCredentials{})
	if err != nil || len(run.Unreachable) != 1 || run.Unreachable[0].Page != "/private" {
		t.Fatalf("run %+v, err %v; want /private unreachable", run, err)
	}

	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			http.Redirect(w, r, "/elsewhere.xml", http.StatusFound)
			return
		case "/elsewhere.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<urlset><url><loc>/</loc></url></urlset>`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer moved.Close()
	if _, err := auditA11yURL(moved.URL, nil, a11yCredentials{}); err == nil || errors.Is(err, errA11ySitemapMissing) {
		t.Fatalf("a redirected sitemap = %v, want discovery to fail", err)
	}
}
