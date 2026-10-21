package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
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
	if got := strings.Join(res.Pages, " "); !strings.Contains(got, "/es/about") {
		t.Errorf("Result.Pages = %s, want the /es/ pages listed", got)
	}
}
