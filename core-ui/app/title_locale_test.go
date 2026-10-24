package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
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

type retiredTitleComp struct{}

func (retiredTitleComp) Render() render.HTML { return "body" }
func (retiredTitleComp) ScreenTitle() string { return "Old title" }

type retiredDescComp struct{}

func (retiredDescComp) Render() render.HTML       { return "body" }
func (retiredDescComp) ScreenDescription() string { return "Old description" }

// The SEO bundle and the JSON-LD hook live in framework/uihost; the old
// spellings are refused by name, whatever they return.
type retiredSEOComp struct{}

func (retiredSEOComp) Render() render.HTML { return "body" }
func (retiredSEOComp) ScreenSEO() struct{} { return struct{}{} }

type retiredSchemaComp struct{}

func (*retiredSchemaComp) Render() render.HTML { return "body" }
func (*retiredSchemaComp) ScreenSchema() []any { return nil }

// A screen still declaring the retired no-argument methods compiles but
// satisfies neither interface, so its title would vanish without a word.
// Registration refuses it instead.
func TestRetiredTitleMethodPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp interface{ Render() render.HTML }
		want string
	}{
		{"title", retiredTitleComp{}, "ScreenTitle(ctx context.Context) now"},
		{"description", retiredDescComp{}, "ScreenDescription(ctx context.Context) now"},
		{"seo", retiredSEOComp{}, "ScreenSEO(ctx context.Context) now"},
		{"schema", &retiredSchemaComp{}, "ScreenSchema(ctx context.Context) now"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "/old") {
					t.Fatalf("panic = %v, want one naming /old and %q", r, tc.want)
				}
			}()
			app.NewApp("Luna").Register("/old", tc.comp, nil)
		})
	}
}

// Components that reach a render without passing registration: a
// policy's RenderAlt result and the synthetic 404/500 bodies.
func TestRetiredTitleAtRenderPanics(t *testing.T) {
	alt := app.PolicyFunc(func(context.Context) app.Decision {
		return decide.RenderAlt(func() component.Component { return retiredTitleComp{} })
	})
	newApp := func() *app.App {
		a := app.NewApp("Luna")
		a.RegisterScreen(app.NewScreen("/dash", phrasedTitleComp{}).WithPolicy(alt), nil)
		return a
	}
	ctx := context.Background()
	for name, render := range map[string]func() error{
		"alt page":    func() error { _, err := newApp().RenderPageResult(ctx, "/dash"); return err },
		"alt partial": func() error { _, err := newApp().RenderPartialResult(ctx, "/dash"); return err },
		"not-found": func() error {
			_, err := newApp().RenderNotFoundPageResult(ctx, "/x", retiredTitleComp{})
			return err
		},
		"not-found partial": func() error {
			_, err := newApp().RenderNotFoundFromResult(ctx, "/x", "/dash", retiredTitleComp{})
			return err
		},
		"error": func() error {
			_, err := newApp().RenderErrorPageResult(ctx, "/x", retiredTitleComp{})
			return err
		},
		"error partial": func() error {
			_, err := newApp().RenderErrorFromResult(ctx, "/x", "/dash", retiredTitleComp{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if msg, _ := recover().(string); !strings.Contains(msg, "ScreenTitle(ctx context.Context) now") {
					t.Fatalf("panic = %q, want the retired-title refusal", msg)
				}
			}()
			_ = render()
		})
	}
}

func TestRetiredTitleOnNotFoundPanics(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "ScreenTitle(ctx context.Context) now") {
			t.Fatalf("panic = %q, want the retired-title refusal", msg)
		}
	}()
	app.NewApp("Luna").WithNotFoundBody(retiredTitleComp{})
}
