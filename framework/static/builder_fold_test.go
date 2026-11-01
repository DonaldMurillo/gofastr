package static

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A static-source or ExtraDirs file that differs from a page's file only
// in case is the page's file on a case-insensitive filesystem: it is
// refused like an exact match, so the rendered page stays.
func TestCopyRefusesCaseFoldedPage(t *testing.T) {
	src := fstest.MapFS{"about/index.html": {Data: []byte("STATIC")}}
	for name, setup := range map[string]func(*Builder){
		"static source": func(b *Builder) { b.Host.SetStaticFS(src) },
		"extra dir": func(b *Builder) {
			b.ExtraDirs = map[string]fs.FS{"/about": fstest.MapFS{"index.html": {Data: []byte("STATIC")}}}
		},
	} {
		a := coreapp.NewApp("t")
		a.Register("/About", &homeScreen{}, nil)
		out := t.TempDir()
		b := &Builder{Host: uihost.New(a), OutDir: out}
		setup(b)
		res, err := b.Build(context.Background())
		if err != nil {
			t.Fatalf("%s: Build: %v", name, err)
		}
		if slices.Contains(res.Assets, "/about/index.html") {
			t.Errorf("%s: copied about/index.html over the page About/index.html", name)
		}
		if got, _ := os.ReadFile(filepath.Join(out, "About", "index.html")); strings.Contains(string(got), "STATIC") {
			t.Errorf("%s: the page holds the static copy", name)
		}
	}
}

// A static source's _HEADERS is the generated _headers on a
// case-insensitive filesystem; copying it dropped the export's CSP. No
// host reads _HEADERS, so it is refused.
func TestCopyRefusesCaseFoldedHeaders(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	host := uihost.New(a)
	host.SetStaticFS(fstest.MapFS{"_HEADERS": {Data: []byte("/*\n  X-Mine: 1\n")}})
	res, err := (&Builder{Host: host, OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(res.Assets, "/_HEADERS") {
		t.Error("copied _HEADERS over the generated _headers")
	}
	if got, _ := os.ReadFile(filepath.Join(out, "_headers")); !strings.Contains(string(got), "Content-Security-Policy") {
		t.Errorf("_headers lost the generated policy:\n%s", got)
	}
}

// fileContributor returns one fixed file.
type fileContributor struct{ path string }

func (fileContributor) ExportStart(context.Context) error { return nil }
func (fileContributor) ExportPage(string, string) error   { return nil }
func (c fileContributor) ExportFiles(context.Context) (map[string][]byte, error) {
	return map[string][]byte{c.path: []byte("ASSET")}, nil
}

// A contributor file on a page's file, in any case, is refused: it
// replaced the page.
func TestContributorRefusesPageFile(t *testing.T) {
	for _, p := range []string{"/about/index.html", "/ABOUT/index.html", "/about/./index.html", "/x/../about/index.html"} {
		a := coreapp.NewApp("t")
		a.Register("/about", &homeScreen{}, nil)
		b := &Builder{Host: uihost.New(a), OutDir: t.TempDir(), Contributors: []Contributor{fileContributor{p}}}
		if _, err := b.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "both export to") {
			t.Errorf("%s: Build error = %v, want a refusal", p, err)
		}
	}
}

// Renaming a route by case only rewrites the same file on a
// case-insensitive filesystem; pruning the old spelling deleted the new
// page.
func TestRebuildKeepsCaseRenamedPage(t *testing.T) {
	out := t.TempDir()
	for _, route := range []string{"/About", "/about"} {
		a := coreapp.NewApp("t")
		a.Register(route, &homeScreen{}, nil)
		if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
			t.Fatalf("Build %s: %v", route, err)
		}
	}
	for _, f := range []string{"index.html", "llm.md"} {
		if _, err := os.Stat(filepath.Join(out, "about", f)); err != nil {
			t.Errorf("the rebuilt page's %s is gone: %v", f, err)
		}
	}
}

// nfcProducts exports "café" precomposed and with a combining accent.
type nfcProducts struct{ productScreen }

func (*nfcProducts) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"slug": "café"}, {"slug": "café"}}
}

// APFS treats the two spellings of "café" as one name, so the second
// page replaced the first: the export refuses them.
func TestExportRefusesNormalizedTwin(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/products/:slug", &nfcProducts{}, nil)
	_, err := (&Builder{Host: uihost.New(a), OutDir: t.TempDir()}).Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "both export to") {
		t.Fatalf("Build error = %v, want a refusal of the two spellings", err)
	}
}

// An ExtraDirs file never replaces a framework asset the build wrote.
func TestExtraDirKeepsFrameworkAsset(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out,
		ExtraDirs: map[string]fs.FS{"/__gofastr": fstest.MapFS{"runtime.js": {Data: []byte("BAD")}}}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "__gofastr", "runtime.js")); string(got) == "BAD" {
		t.Error("an extra dir replaced the generated runtime.js")
	}
}

// A rail script on a page's file, in any case, or on a redirect's page,
// fails the build instead of replacing the page.
func TestRailScriptRefusesPageFile(t *testing.T) {
	for name, c := range map[string]struct {
		src   string
		setup func(*coreapp.App)
	}{
		"page in another case": {"/INDEX.HTML", func(*coreapp.App) {}},
		"redirect page":        {"/old/index.html", func(a *coreapp.App) { a.Redirect("/old", "/") }},
	} {
		a := coreapp.NewApp("t")
		a.Register("/", &homeScreen{}, nil)
		c.setup(a)
		host := uihost.New(a, uihost.WithExtraScripts(c.src))
		mux := http.NewServeMux()
		mux.HandleFunc("GET "+c.src, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("JS")) })
		_, err := (&Builder{Host: host, OutDir: t.TempDir(), Handler: mux}).Build(context.Background())
		if err == nil || !strings.Contains(err.Error(), "would overwrite") {
			t.Errorf("%s: Build error = %v, want a refusal", name, err)
		}
	}
}

// A contributor file is recorded under the path it is written to, so a
// rebuild that no longer renders the page keeps the contributed file.
func TestContributorRecordsCleanPath(t *testing.T) {
	out := t.TempDir()
	a := coreapp.NewApp("t")
	a.Register("/about", &homeScreen{}, nil)
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	a = coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	res, err := (&Builder{Host: uihost.New(a), OutDir: out, Contributors: []Contributor{fileContributor{"/about/./index.html"}}}).Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Assets, "/about/index.html") {
		t.Errorf("assets %v do not name /about/index.html", res.Assets)
	}
	if got, err := os.ReadFile(filepath.Join(out, "about", "index.html")); err != nil || string(got) != "ASSET" {
		t.Errorf("contributed file = %q, %v; want ASSET", got, err)
	}
}

// A backslash is a separator on Windows, so a contributor path holding
// one could land on a page there: it is refused everywhere.
func TestContributorRefusesBackslash(t *testing.T) {
	a := coreapp.NewApp("t")
	a.Register("/about", &homeScreen{}, nil)
	b := &Builder{Host: uihost.New(a), OutDir: t.TempDir(), Contributors: []Contributor{fileContributor{`/about\index.html`}}}
	if _, err := b.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "backslash") {
		t.Fatalf("Build error = %v, want a backslash refusal", err)
	}
}
