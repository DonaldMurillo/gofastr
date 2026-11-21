package static

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestSweepStaticDefaultCSPDirectives(t *testing.T) {
	for _, d := range []string{"default-src 'self'", "object-src 'none'", "form-action 'self'", "frame-ancestors 'none'", "base-uri 'none'", "worker-src 'self'", "require-trusted-types-for 'script'", "trusted-types gofastr"} {
		if !strings.Contains(staticExportCSP, d) {
			t.Errorf("the default export policy lacks %s: %s", d, staticExportCSP)
		}
	}
}

func TestSweepStaticCSPRefusesAngleBrackets(t *testing.T) {
	for _, p := range []string{"default-src 'self' <x", "default-src 'self' x>"} {
		if checkExportCSP(p) == nil {
			t.Errorf("checkExportCSP accepted %q", p)
		}
	}
}

func TestSweepStaticCSPMeta(t *testing.T) {
	own := `<html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'"></head></html>`
	if got := applyStaticCSP(own, staticExportCSP); got != own {
		t.Errorf("a page carrying its own policy meta was changed:\n%s", got)
	}
	got := applyStaticCSP("<html><head></head></html>", "default-src https://a.example/?x&y")
	if !strings.Contains(got, `content="default-src https://a.example/?x&amp;y"`) {
		t.Errorf("the meta does not escape &:\n%s", got)
	}
}

func TestSweepStaticSrcsetCommaEndsCandidate(t *testing.T) {
	if got := prefixSrcset("/a.png, /b.png 2x", "/base"); got != "/base/a.png, /base/b.png 2x" {
		t.Errorf("prefixSrcset = %q", got)
	}
}

func TestSweepStaticSlashLinksStayInBase(t *testing.T) {
	b := &Builder{BasePath: "/base"}
	pages := pageSet([]string{"/about"})
	if got := b.slashPageLinks(` href="/basex/about"`, pages); got != ` href="/basex/about"` {
		t.Errorf("a link outside the base path was slashed: %s", got)
	}
	if got := b.slashPageLinks(` href="/base/about"`, pages); got != ` href="/base/about/"` {
		t.Errorf("a page link under the base was not slashed: %s", got)
	}
	if got := (&Builder{}).slashPageLinks(` href="/items/caf%C3%A9"`, pageSet([]string{"/items/café"})); got != ` href="/items/caf%C3%A9/"` {
		t.Errorf("an escaped page link was not slashed: %s", got)
	}
}

func TestSweepStaticNotFoundPage(t *testing.T) {
	a := coreapp.NewApp("SSG404")
	a.SetDefaultLayout(coreapp.NewLayout("shell", coreapp.LayoutSpec{}, func(ctx context.Context, l *coreapp.LayoutTree) render.HTML {
		return render.Join(render.HTML(`<nav><a href="/about">about</a></nav>`), l.Primary())
	}))
	a.Register("/", &homeScreen{}, nil)
	a.Register("/about", &homeScreen{}, nil)
	out := t.TempDir()
	res, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	page := readOut(t, out, "404.html")
	for _, want := range []string{"data-cui-static", `http-equiv="Content-Security-Policy"`, `href="/about/"`} {
		if !strings.Contains(page, want) {
			t.Errorf("404.html lacks %s:\n%s", want, page)
		}
	}
	found := false
	for _, p := range res.Assets {
		found = found || p == "/404.html"
	}
	if !found {
		t.Errorf("Result.Assets lacks /404.html: %v", res.Assets)
	}
}

func TestSweepStaticKeepUnprefixedRedirect(t *testing.T) {
	a := coreapp.NewApp("Keep")
	a.Register("/about", &homeScreen{}, nil)
	a.Redirect("/old", "/about")
	b := &Builder{Host: uihost.New(a)}
	if b.keepUnprefixed("/old") {
		t.Error("a redirect's path is kept unprefixed")
	}
	if !b.keepUnprefixed("/nothing") {
		t.Error("a path no route or redirect takes is prefixed")
	}
}

func TestSweepStaticRedirectHelpers(t *testing.T) {
	if got := urlBytesEscaper("/a b\x01\x7f"); got != "/a%20b%01%7F" {
		t.Errorf("urlBytesEscaper = %q", got)
	}
	if got := (&Builder{}).redirectHref("/team", map[string]bool{"/team/": true}); got != "/team/" {
		t.Errorf("redirectHref to a page registered with its slash = %q", got)
	}
	if page := redirectPage(context.Background(), "/x", staticExportCSP); !strings.Contains(page, `<html lang="en">`) {
		t.Errorf("a redirect page without a locale is not English:\n%s", page)
	}
}

func TestSweepStaticExcludedRedirectAndDoc(t *testing.T) {
	a := coreapp.NewApp("Exclude")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/secret", &homeScreen{}, nil)
	a.Redirect("/old", "/")
	out := t.TempDir()
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out, ExcludeRoutes: []string{"/old", "/secret/"}}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, rel := range []string{"old/index.html", "secret/index.html", "secret/llm.md"} {
		if _, err := os.Stat(filepath.Join(out, rel)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("an excluded route wrote %s", rel)
		}
	}
	if idx := readOut(t, out, "llm-pages.md"); strings.Contains(idx, "/secret/llm.md") {
		t.Errorf("the doc index links an excluded route's doc:\n%s", idx)
	}
}

func TestSweepStaticRouteExcludedIsSegmentAligned(t *testing.T) {
	b := &Builder{ExcludeRoutes: []string{"/broken/"}}
	if !b.routeExcluded("/broken") || !b.routeExcluded("/broken/load") {
		t.Error("an entry does not cover its own route and subtree")
	}
	if b.routeExcluded("/brokenly") {
		t.Error("an entry covers a route that only shares its prefix")
	}
}

type sweepGatedPaths struct{}

func (sweepGatedPaths) Render() render.HTML         { return "<p>secret admin body</p>" }
func (sweepGatedPaths) SetParams(map[string]string) {}
func (sweepGatedPaths) StaticPaths(context.Context) []map[string]string {
	return []map[string]string{{"id": "abc"}}
}

func TestSweepStaticUnresolvedDocIsWithheld(t *testing.T) {
	a := coreapp.NewApp("t")
	gate := coreapp.PolicyFunc(func(ctx context.Context) coreapp.Decision {
		return coreapp.Decision{Kind: coreapp.DecisionBlock}
	})
	scr := coreapp.NewScreen("/admin/{id:int}", sweepGatedPaths{}).WithPolicy(gate)
	scr.Title = "Project NIGHTFALL"
	a.RegisterScreen(scr, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a), OutDir: out}
	if err := b.writeLLMMD(context.Background(), &Result{}, func(p string) string { return p }, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if doc := readOut(t, out, "admin/abc/llm.md"); strings.Contains(doc, "NIGHTFALL") || strings.Contains(doc, "secret admin body") {
		t.Errorf("an unresolved path's doc carries the gated screen:\n%s", doc)
	}
}

func TestSweepStaticDocStatus(t *testing.T) {
	if docStatus(nil, http.StatusOK) != 0 || docStatus(nil, 0) != 0 || docStatus(nil, http.StatusNotFound) != http.StatusNotFound {
		t.Error("docStatus does not map 200 and unset to zero and keep other statuses")
	}
}

type sweepBrokenFill struct{}

func (sweepBrokenFill) Load(context.Context) error {
	return errors.New("boom \u202eevil\x1b[31m")
}
func (sweepBrokenFill) Render() render.HTML { return "<p>fill</p>" }

func TestSweepStaticFillFailureIsScrubbed(t *testing.T) {
	a := coreapp.NewApp("Fill")
	aside := coreapp.NewOutlet("aside", coreapp.OutletOptions{})
	a.SetDefaultLayout(coreapp.NewLayout("shell", coreapp.LayoutSpec{Outlets: []*coreapp.Outlet{aside}}, func(ctx context.Context, l *coreapp.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(aside))
	}))
	a.RegisterScreen(coreapp.NewScreen("/broken", &homeScreen{}).Fill(aside, sweepBrokenFill{}), nil)
	_, err := (&Builder{Host: uihost.New(a), OutDir: t.TempDir()}).Build(context.Background())
	if err == nil {
		t.Fatal("a contained fill failure did not refuse the export")
	}
	if strings.ContainsAny(err.Error(), "\u202e\x1b") {
		t.Errorf("the refusal carries unscrubbed error text: %q", err)
	}
}

func TestSweepStaticWalkSkipAndNilHost(t *testing.T) {
	a := coreapp.NewApp("Walk")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	var seen []string
	err := Walk(context.Background(), uihost.New(a), func(p string) bool { return p == "/products/beta" }, func(p, _ string, err error) error {
		seen = append(seen, p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range seen {
		if p == "/products/beta" {
			t.Errorf("Walk rendered a skipped path: %v", seen)
		}
	}
	if Walk(context.Background(), nil, nil, nil) == nil {
		t.Error("Walk accepted a nil host")
	}
	if _, err := (&Builder{OutDir: t.TempDir()}).Build(context.Background()); err == nil {
		t.Error("Build accepted a nil Host")
	}
	t.Chdir(t.TempDir())
	if _, err := (&Builder{Host: uihost.New(a), OutDir: " "}).Build(context.Background()); err == nil {
		t.Error("Build accepted a blank OutDir")
	}
}

type sweepContributor struct {
	start  error
	files  map[string][]byte
	pages  []string
	starts int
}

func (c *sweepContributor) ExportStart(context.Context) error { c.starts++; return c.start }
func (c *sweepContributor) ExportPage(_, html string) error {
	c.pages = append(c.pages, html)
	return nil
}
func (c *sweepContributor) ExportFiles(context.Context) (map[string][]byte, error) {
	return c.files, nil
}

func TestSweepStaticContributorContract(t *testing.T) {
	a := coreapp.NewApp("Contrib")
	a.Register("/", &homeScreen{}, nil)
	build := func(c *sweepContributor) error {
		_, err := (&Builder{Host: uihost.New(a), OutDir: t.TempDir(), Contributors: []Contributor{c}}).Build(context.Background())
		return err
	}
	if err := build(&sweepContributor{start: errors.New("no")}); err == nil {
		t.Error("a failed ExportStart did not fail the build")
	}
	if err := build(&sweepContributor{files: map[string][]byte{"index.json": nil}}); err == nil {
		t.Error("a contributor file that is not root-absolute was accepted")
	}
	c := &sweepContributor{}
	if err := build(c); err != nil {
		t.Fatal(err)
	}
	if len(c.pages) != 1 || strings.Contains(c.pages[0], "data-cui-static") {
		t.Errorf("ExportPage did not get the live HTML: %q", c.pages)
	}
}

func TestSweepStaticDiskValueRefusals(t *testing.T) {
	for _, v := range []string{"", ".", "a\\b", "a\x00b", "a//b"} {
		if _, err := applyParams("/p/:slug", map[string]string{"slug": v}); err == nil {
			t.Errorf("applyParams accepted %q", v)
		}
	}
	if got, err := applyParams("/p/:slug", map[string]string{"slug": "a/b"}); err != nil || pathToFile(got) != filepath.Join("p", "a", "b", "index.html") {
		t.Errorf("a slash in a one-segment key = %q, %v", got, err)
	}
}

func TestSweepStaticSourceWinsOverExtraDir(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "docs", "x.txt"), []byte("static"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := coreapp.NewApp("Extra")
	a.Register("/", &homeScreen{}, nil)
	out := t.TempDir()
	b := &Builder{Host: uihost.New(a, uihost.WithStaticDir(src)), OutDir: out,
		ExtraDirs: map[string]fs.FS{"/docs": fstest.MapFS{"x.txt": {Data: []byte("extra")}}}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readOut(t, out, "docs/x.txt"); got != "static" {
		t.Errorf("docs/x.txt = %q, want the static source's copy", got)
	}
}

type foldFS struct{ m fstest.MapFS }

func (f foldFS) Open(name string) (fs.File, error) {
	if _, ok := f.m[name]; ok || name == "." {
		return f.m.Open(name)
	}
	for k := range f.m {
		if strings.EqualFold(k, name) {
			return f.m.Open(k)
		}
	}
	return nil, fs.ErrNotExist
}

func TestSweepStaticSpelledInNeedsExactName(t *testing.T) {
	fsys := foldFS{fstest.MapFS{"MANIFEST.webmanifest": {Data: []byte("{}")}}}
	if spelledIn(fsys, "manifest.webmanifest") {
		t.Error("spelledIn matched another spelling")
	}
	if !spelledIn(fsys, "MANIFEST.webmanifest") {
		t.Error("spelledIn missed the exact spelling")
	}
}

func TestSweepStaticRailScriptTakesBasePath(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/r.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`fetch("/__gofastr/x")`))
	})
	out := t.TempDir()
	host := uihost.New(a, uihost.WithExtraScripts("/__site/r.js"))
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux, BasePath: "/base"}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readOut(t, out, "__site/r.js"); !strings.Contains(got, `"/base/__gofastr/x"`) {
		t.Errorf("the rail script kept its root URLs: %s", got)
	}
}

func TestSweepStaticWidgetChrome(t *testing.T) {
	def := widget.New("ssg-sweep-chrome").Hidden().Slot("body", helloScreen{}).Build()
	widget.Mount(router.New(), &def)
	a := coreapp.NewApp("Chrome")
	a.Register("/", &helloScreen{}, nil)
	a.Register("/about", &helloScreen{}, nil)
	host := uihost.New(a)
	cat := i18n.NewMapCatalog()
	cat.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
	tr := i18n.NewTranslator(cat, "en")
	set, err := i18n.NewPathLocales(tr)
	if err != nil {
		t.Fatal(err)
	}
	host.SetPathLocales(set)
	out := t.TempDir()
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), tr), i18n.Locale{Tag: "es"})
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: "/site"}).Build(ctx); err != nil {
		t.Fatal(err)
	}
	chrome := readOut(t, out, "core-ui/widget/ssg-sweep-chrome/chrome")
	if !strings.Contains(chrome, "Hello") || !strings.Contains(chrome, `href="/site/about/"`) {
		t.Errorf("the root chrome is not in the default locale under the base path:\n%s", chrome)
	}
}

func TestSweepStaticWorkerHash(t *testing.T) {
	build := func(out, name string) string {
		t.Helper()
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, name), []byte("same"), 0o644); err != nil {
			t.Fatal(err)
		}
		a := coreapp.NewApp("Hash")
		a.Register("/", &homeScreen{}, nil)
		host := uihost.New(a, uihost.WithStaticDir(src), uihost.WithPWA(uihost.PWAConfig{}))
		if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
			t.Fatal(err)
		}
		return readOut(t, out, "service-worker.js")
	}
	out := t.TempDir()
	first := build(out, "a.txt")
	if again := build(out, "a.txt"); again != first {
		t.Error("a rebuild over the last build's output changed the worker")
	}
	if renamed := build(t.TempDir(), "b.txt"); renamed == first {
		t.Error("renaming a file left the worker unchanged")
	}
	if worker := build(t.TempDir(), "café.txt"); !strings.Contains(worker, "caf%C3%A9.txt") {
		t.Error("the worker lists a static file unescaped")
	}
	if !strings.Contains(readOut(t, out, "__gofastr/pwa/offline/index.html"), "data-cui-static") {
		t.Error("the offline page lacks the static-mode stamp")
	}
}

func TestSweepStaticPruneKeepsAssetOnOldPage(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "site")
	a := coreapp.NewApp("Prune")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/foo", &homeScreen{}, nil)
	if _, err := (&Builder{Host: uihost.New(a), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "foo", "index.html"), []byte("static"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := coreapp.NewApp("Prune")
	b.Register("/", &homeScreen{}, nil)
	if _, err := (&Builder{Host: uihost.New(b, uihost.WithStaticDir(src)), OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readOut(t, out, "foo/index.html"); got != "static" {
		t.Errorf("foo/index.html = %q, want the static file this build copied", got)
	}
}

func TestSweepStaticRefusesNULName(t *testing.T) {
	if refuseSeparatorName("a\x00b") == nil {
		t.Error("a NUL in a file name was accepted")
	}
}

func TestSweepStaticWatch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newMtimeMap()
	m.scan([]string{dir})
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if !m.changed([]string{dir}) {
		t.Error("a deleted file is not a change")
	}
	var reported atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := (&Builder{}).Watch(ctx, nil, time.Millisecond, func(error) { reported.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if reported.Load() == 0 {
		t.Error("a failed first build was not reported to onError")
	}
}

func TestSweepStaticWorkerHashesUserFileUnderPWADir(t *testing.T) {
	build := func(body string) string {
		t.Helper()
		src := t.TempDir()
		if err := os.MkdirAll(filepath.Join(src, "__gofastr", "pwa"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "__gofastr", "pwa", "extra.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		a := coreapp.NewApp("Hash")
		a.Register("/", &homeScreen{}, nil)
		out := t.TempDir()
		host := uihost.New(a, uihost.WithStaticDir(src), uihost.WithPWA(uihost.PWAConfig{}))
		if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
			t.Fatal(err)
		}
		return readOut(t, out, "service-worker.js")
	}
	if build("one") == build("two") {
		t.Error("a user file under __gofastr/pwa/ changed without changing the worker")
	}
}

func TestSweepStaticSourceHasMatchesCopy(t *testing.T) {
	seo := func(h *uihost.UIHost) string {
		t.Helper()
		out := t.TempDir()
		if _, err := (&Builder{Host: h, OutDir: out}).Build(context.Background()); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(out, "robots.txt"))
		return string(got)
	}
	opts := []uihost.Option{uihost.WithRobots(uihost.RobotsConfig{})}

	src, outside := t.TempDir(), filepath.Join(t.TempDir(), "robots.txt")
	if err := os.WriteFile(outside, []byte("User-agent: *\nDisallow: /outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "robots.txt")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if got := seo(uihost.New(sweepSourceApp(), append(opts, uihost.WithStaticDir(src))...)); strings.Contains(got, "/outside") || got == "" {
		t.Errorf("an escaping symlink kept the generated robots.txt out: %q", got)
	}

	h := uihost.New(sweepSourceApp(), opts...)
	h.SetStaticFS(sweepUnstattableFS{fstest.MapFS{"robots.txt": {Mode: fs.ModeNamedPipe}}})
	if got := seo(h); got == "" {
		t.Error("an entry that cannot be stat'ed and is listed as a pipe kept the generated robots.txt out")
	}
}

func sweepSourceApp() *coreapp.App {
	a := coreapp.NewApp("Source")
	a.Register("/", &homeScreen{}, nil)
	return a
}

type sweepUnstattableFS struct{ fstest.MapFS }

func (f sweepUnstattableFS) Open(name string) (fs.File, error) {
	if name == "robots.txt" {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Open(name)
}

func (f sweepUnstattableFS) Stat(name string) (fs.FileInfo, error) {
	if name == "robots.txt" {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Stat(name)
}
