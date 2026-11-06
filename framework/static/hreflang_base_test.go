package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestExportAlternatesIncludeBase(t *testing.T) {
	for _, origin := range []string{"https://example.com", "https://example.com/site"} {
		t.Run(origin, func(t *testing.T) {
			application := coreapp.NewApp("Alternates")
			application.Register("/about", &helloScreen{}, nil)
			host := uihost.New(application, uihost.WithSitemap(uihost.SitemapConfig{BaseURL: origin}))
			catalog := i18n.NewMapCatalog()
			catalog.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
			translator := i18n.NewTranslator(catalog, "en")
			locales, err := i18n.NewPathLocales(translator)
			if err != nil {
				t.Fatal(err)
			}
			host.SetPathLocales(locales)
			output := t.TempDir()
			if _, err := (&Builder{Host: host, OutDir: output, BasePath: "/site"}).Build(i18n.WithTranslator(context.Background(), translator)); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"about/index.html", "es/about/index.html"} {
				page := readOut(t, output, path)
				for _, alternate := range []string{
					`hreflang="en" href="https://example.com/site/about"`,
					`hreflang="es" href="https://example.com/site/es/about"`,
					`hreflang="x-default" href="https://example.com/site/about"`,
				} {
					if !strings.Contains(page, alternate) {
						t.Errorf("%s missing %s", path, alternate)
					}
				}
			}
		})
	}
}
