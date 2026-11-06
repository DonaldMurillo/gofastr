package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestScreenDocContentUsesLocale(t *testing.T) {
	phrase := i18n.Define("app_test.doc_plans", "Plans")
	catalog := i18n.NewMapCatalog()
	catalog.Set("es", phrase.Key(), i18n.Message{Text: "Planes"})
	catalog.Set("fr", phrase.Key(), i18n.Message{Text: "Forfaits"})
	base := i18n.WithTranslator(context.Background(), i18n.NewTranslator(catalog, "en"))
	locales := i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}}
	screen := NewScreen("/pricing", NewContextComponent(func(ctx context.Context) render.HTML {
		return render.Text(phrase.Text(ctx, nil)) + `<a href="/about">about</a>`
	}))
	application := NewApp("docs")
	application.RegisterScreen(screen, nil)
	handler := ScreenLLMMDHandler(application, screen)
	for _, test := range []struct{ locale, content, link string }{
		{"es", "Planes", "/es/about"},
		{"fr", "Forfaits", "/fr/about"},
	} {
		t.Run(test.locale, func(t *testing.T) {
			ctx := i18n.WithPathLocale(base, locales, test.locale)
			ctx = i18n.WithContext(ctx, i18n.Locale{Tag: test.locale})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/"+test.locale+"/pricing/llm.md", nil).WithContext(ctx)
			handler.ServeHTTP(recorder, request)
			body := recorder.Body.String()
			if !strings.Contains(body, test.content) || !strings.Contains(body, "("+test.link+")") {
				t.Fatalf("localized doc lacks %q or %q:\n%s", test.content, test.link, body)
			}
		})
	}
}

func TestScreenDocUnprefixedLocale(t *testing.T) {
	phrase := i18n.Define("app_test.doc_unprefixed_plans", "Plans")
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", phrase.Key(), i18n.Message{Text: "Forfaits"})
	catalog.Set("de", phrase.Key(), i18n.Message{Text: "Tarife"})
	translator := i18n.NewTranslator(catalog, "fr")
	base := context.Background()
	translated := i18n.WithTranslator(base, translator)
	french := i18n.WithContext(translated, i18n.Locale{Tag: "fr"})
	defaultLocale := i18n.WithPathLocale(french, i18n.PathLocales{Default: "fr", Prefixed: []string{"de"}}, "fr")
	screen := NewScreen("/pricing", NewContextComponent(func(ctx context.Context) render.HTML {
		return render.Text(phrase.Text(ctx)+" ["+i18n.FromContext(ctx).Tag+"]") + `<a href="/about">about</a>`
	}))
	application := NewApp("docs")
	application.RegisterScreen(screen, nil)
	handler := ScreenLLMMDHandler(application, screen)
	for _, test := range []struct {
		name, content string
		ctx           context.Context
	}{
		{"unprefixed default", "Forfaits [fr]", defaultLocale},
		{"translator only", "Forfaits []", translated},
		{"locale only", "Plans [de]", i18n.WithContext(base, i18n.Locale{Tag: "de"})},
		{"negotiated locale", "Tarife [de]", i18n.WithContext(translated, i18n.Locale{Tag: "de"})},
		{"no locale", "Plans []", base},
	} {
		t.Run(test.name, func(t *testing.T) {
			if prefix := i18n.PathPrefix(test.ctx); prefix != "" {
				t.Fatalf("test needs an unprefixed locale, got %q", prefix)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/pricing/llm.md", nil).WithContext(test.ctx)
			handler.ServeHTTP(recorder, request)
			body := recorder.Body.String()
			if !strings.Contains(body, test.content) || !strings.Contains(body, "(/about)") {
				t.Fatalf("unprefixed doc lacks %q or /about link:\n%s", test.content, body)
			}
			if want := ScreenLLMMD(test.ctx, screen); body != want {
				t.Fatalf("handler differs from request-context render:\ngot:\n%s\nwant:\n%s", body, want)
			}
		})
	}
}
