package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var helpTitle = i18n.Define("app_test.help_title", "Help center")

type phrasedTitleComp struct{}

func (phrasedTitleComp) Render() render.HTML { return "body" }
func (phrasedTitleComp) ScreenTitle(ctx context.Context) string {
	return helpTitle.Text(ctx, nil)
}

// The page <title> reads ScreenTitle under the request's locale; the
// registration-time title (no reader yet) is the phrase's English.
func TestScreenTitleSpeaksLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "app_test.help_title", i18n.Message{Text: "Centre d'aide"})
	ctx := i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en"))
	ctx = i18n.WithContext(ctx, i18n.Locale{Tag: "fr"})

	a := app.NewApp("Luna")
	a.Register("/help", phrasedTitleComp{}, nil)
	res, err := a.RenderPageResult(ctx, "/help")
	if err != nil {
		t.Fatal(err)
	}
	if want := "<title>Centre d&#39;aide — Luna</title>"; !strings.Contains(string(res.HTML), want) {
		t.Fatalf("page title missing %q", want)
	}
	screen, _, _ := a.Router.Resolve("/help")
	if screen.Title != "Help center" {
		t.Fatalf("registered title = %q, want the English", screen.Title)
	}
}
