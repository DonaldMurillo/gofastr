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

type docLinkScreen struct{ component.ContextOnly }

func (docLinkScreen) RenderCtx(ctx context.Context) render.HTML {
	return "<p>" + render.Text(exportHello.Text(ctx)) +
		`</p><a href="/about">about</a><a href="/brochure.pdf">pdf</a>`
}

// The live /es/about/llm.md is the Spanish doc with /es/ links; the
// export wrote one English llm.md per page, so the /es/ tree had none
// and its page index linked docs the static host 404s.
func TestExportWritesLocaleLLMMD(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "brochure.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := coreapp.NewApp("SSGLocaleDoc")
	a.Register("/", docLinkScreen{}, nil)
	a.Register("/about", docLinkScreen{}, nil)
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
	for file, wants := range map[string][]string{
		"es/about/llm.md": {"Hola", "(/es/about)", "(/brochure.pdf)"},
		"es/llm.md":       {"Hola", "(/es/about)"},
		"es/llm-pages.md": {"/es/about/llm.md"},
		"about/llm.md":    {"Hello", "(/about)", "(/brochure.pdf)"},
		"llm-pages.md":    {"/about/llm.md"},
	} {
		b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(file)))
		if err != nil {
			t.Errorf("missing %s: %v", file, err)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s lacks %s:\n%s", file, want, b)
			}
		}
	}
}
