package static

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

var exportHello = i18n.Define("static_test.export.hello", "Hello")

type helloScreen struct{ component.ContextOnly }

func (helloScreen) RenderCtx(ctx context.Context) render.HTML {
	return "<p>" + render.Text(exportHello.Text(ctx)) + `</p><a href="/about">about</a>`
}

// An app that routes by locale prefix exports one page tree per
// prefixed locale: /es/about/index.html in Spanish, its links under
// /es/, and the runtime told the base the prefix sits behind.
func TestExportWritesLocaleTrees(t *testing.T) {
	def := widget.New("ssg-locale-test").Hidden().Slot("body", helloScreen{}).Build()
	widget.Mount(router.New(), &def)
	grouped := widget.New("ssg-locale-grouped").Hidden().Slot("body", helloScreen{}).Build()
	widget.Mount(router.New().Group("/v2"), &grouped)
	a := coreapp.NewApp("SSGLocale")
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
	res, err := (&Builder{Host: host, OutDir: out, BasePath: "/site"}).Build(i18n.WithTranslator(context.Background(), tr))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	es := read("es/about/index.html")
	for _, want := range []string{`lang="es"`, "Hola", `href="/site/es/about/"`, `"base":"/site"`} {
		if !strings.Contains(es, want) {
			t.Errorf("es/about/index.html lacks %s", want)
		}
	}
	en := read("about/index.html")
	for _, want := range []string{"Hello", `href="/site/about/"`} {
		if !strings.Contains(en, want) {
			t.Errorf("about/index.html lacks %s", want)
		}
	}
	if !strings.Contains(read("es/index.html"), "Hola") {
		t.Error("es/index.html is not Spanish")
	}
	// A hidden widget's chrome is fetched when it opens, so each locale
	// tree carries its own copy, rendered in that locale.
	if got := read("es/v2/core-ui/widget/ssg-locale-grouped/chrome"); !strings.Contains(got, "Hola") {
		t.Errorf("the /es/ chrome of a widget mounted under /v2 is not Spanish:\n%s", got)
	}
	if got := read("es/core-ui/widget/ssg-locale-test/chrome"); !strings.Contains(got, "Hola") {
		t.Errorf("the /es/ widget chrome is not Spanish:\n%s", got)
	}
	if got := read("core-ui/widget/ssg-locale-test/chrome"); !strings.Contains(got, "Hello") {
		t.Errorf("the widget chrome is not English:\n%s", got)
	}
	if got := strings.Join(res.Pages, " "); !strings.Contains(got, "/es/about") {
		t.Errorf("Result.Pages = %s, want the /es/ pages listed", got)
	}
}

// fileLinkScreen links a static file, an extra-dir file, a path no
// route takes, and a page.
type fileLinkScreen struct{}

func (fileLinkScreen) Render() render.HTML {
	return `<a href="/files/report.pdf">report</a><a href="/dl/go.zip">sdk</a>` +
		`<a href="/nowhere">gone</a><a href="/about">about</a>`
}

// TestLocaleTreeKeepsFileLinks: the static source and extra dirs are
// copied once, to the export root, not into each locale tree, so a
// Spanish page's link to /files/report.pdf must stay unprefixed or it
// 404s; a link to a page still takes the /es/ prefix.
func TestLocaleTreeKeepsFileLinks(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "files", "report.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := coreapp.NewApp("SSGLocaleFiles")
	a.Register("/", fileLinkScreen{}, nil)
	a.Register("/about", fileLinkScreen{}, nil)
	// Routes that also match the files' paths: the copied file wins, as
	// the live server serves it first.
	a.Register("/files/:slug", &productScreen{}, nil)
	a.Register("/dl/:slug", &productScreen{}, nil)
	host := uihost.New(a, uihost.WithStaticDir(src))
	cat := i18n.NewMapCatalog()
	cat.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
	tr := i18n.NewTranslator(cat, "en")
	set, err := i18n.NewPathLocales(tr)
	if err != nil {
		t.Fatal(err)
	}
	host.SetPathLocales(set)
	out := t.TempDir()
	b := &Builder{Host: host, OutDir: out, ExtraDirs: map[string]fs.FS{
		"/dl": fstest.MapFS{"go.zip": {Data: []byte("zip")}},
	}}
	if _, err := b.Build(i18n.WithTranslator(context.Background(), tr)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	es := readOut(t, out, "es/about/index.html")
	for _, href := range []string{"/files/report.pdf", "/dl/go.zip"} {
		if !strings.Contains(es, `href="`+href+`"`) {
			t.Errorf("the Spanish page does not link %s unprefixed:\n%s", href, es)
		}
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(href))); err != nil {
			t.Errorf("%s is not in the export: %v", href, err)
		}
	}
	if !strings.Contains(es, `href="/nowhere"`) {
		t.Errorf("a path no route takes was prefixed:\n%s", es)
	}
	if !strings.Contains(es, `href="/es/about/"`) {
		t.Errorf("the page link lost its /es/ prefix:\n%s", es)
	}
}

// Each locale tree's inline route graph carries loading content in that
// locale, the translator reaching the export through ctx alone.
func TestExportLoadingSpeaksLocale(t *testing.T) {
	a := coreapp.NewApp("SSGLoading")
	a.RegisterScreen(coreapp.NewScreen("/", &helloScreen{}).
		WithLoading(&coreapp.Loading{Show: coreapp.NewContextComponent(func(ctx context.Context) render.HTML {
			return "<i>" + render.Text(exportHello.Text(ctx)) + "</i>"
		})}), nil)
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
	if _, err := (&Builder{Host: host, OutDir: out}).Build(i18n.WithTranslator(context.Background(), tr)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for file, want := range map[string]string{"index.html": `\u003ci\u003eHello`, "es/index.html": `\u003ci\u003eHola`} {
		b, err := os.ReadFile(filepath.Join(out, file))
		if err != nil {
			t.Fatal(err)
		}
		_, routes, _ := strings.Cut(string(b), `id="gofastr-routes">`)
		routes, _, _ = strings.Cut(routes, "</script>")
		if !strings.Contains(routes, want) {
			t.Errorf("%s: inline routes lack %s:\n%s", file, want, routes)
		}
	}
}

// cafeLinkScreen links an exact route whose path is not ASCII, escaped
// the way a browser sends it.
type cafeLinkScreen struct{}

func (cafeLinkScreen) Render() render.HTML { return `<a href="/caf%C3%A9">cafe</a>` }

// TestLocaleTreeTargetsFollowLinkRule: in a locale tree a redirect's
// target follows the rule its links follow. A static file the export
// copies once to the root stays unprefixed, a target that already names
// a locale keeps it, and a page takes the tree's prefix. An escaped link
// to an exact route is a page link too.
func TestLocaleTreeTargetsFollowLinkRule(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "files", "report.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := coreapp.NewApp("SSGLocaleTargets")
	a.Register("/", cafeLinkScreen{}, nil)
	a.Register("/about", cafeLinkScreen{}, nil)
	a.Register("/café", cafeLinkScreen{}, nil)
	a.Redirect("/download", "/files/report.pdf")
	a.Redirect("/hola", "/es/about")
	a.Redirect("/team", "/about")
	host := uihost.New(a, uihost.WithStaticDir(src))
	cat := i18n.NewMapCatalog()
	cat.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
	tr := i18n.NewTranslator(cat, "en")
	set, err := i18n.NewPathLocales(tr)
	if err != nil {
		t.Fatal(err)
	}
	host.SetPathLocales(set)
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(i18n.WithTranslator(context.Background(), tr)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for from, want := range map[string]string{
		"es/download/index.html": "url=/files/report.pdf\"",
		"es/hola/index.html":     "url=/es/about/\"",
		"es/team/index.html":     "url=/es/about/\"",
	} {
		if page := readOut(t, out, from); !strings.Contains(page, want) {
			t.Errorf("%s does not redirect with %s:\n%s", from, want, page)
		}
	}
	rules := readOut(t, out, "_redirects")
	for _, want := range []string{"/es/download /files/report.pdf 308\n", "/es/hola /es/about/ 308\n"} {
		if !strings.Contains(rules, want) {
			t.Errorf("_redirects lacks %q:\n%s", want, rules)
		}
	}
	if es := readOut(t, out, "es/about/index.html"); !strings.Contains(es, `href="/es/caf%C3%A9`) {
		t.Errorf("the escaped link to /café lost its /es/ prefix:\n%s", es)
	}
}
