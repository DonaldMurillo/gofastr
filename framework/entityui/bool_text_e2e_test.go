package entityui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestBoolCardsUseLocale(t *testing.T) {
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", i18nui.EntityYes.Key(), i18n.Message{Text: "Oui"})
	catalog.Set("fr", i18nui.EntityNo.Key(), i18n.Message{Text: "Non"})
	translator := i18n.NewTranslator(catalog, "en")
	config := entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}, {Name: "active", Type: schema.Bool}},
		Exposure: &entity.ExposureConfig{Public: true},
		Display:  &entity.DisplayConfig{Card: &entity.CardFields{Title: "name", Subtitle: "active", Meta: []string{"active"}}},
	}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"flags": config}, map[string][]map[string]any{"flags": {
		{"id": "unset", "name": "Unset"}, {"id": "off", "name": "Off", "active": false}, {"id": "on", "name": "On", "active": true},
	}}, withTranslator(translator))
	ctx := i18n.WithTranslator(i18n.WithContext(fixture.ctx("/flags", ""), i18n.Locale{Tag: "fr"}), translator)
	body := listHTML(t, fixture.ui.List("flags").Base("/flags").As("cards"), ctx)
	metadata, err := fixture.ui.meta("flags")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		value any
		want  string
	}{{nil, ""}, {false, "Non"}, {true, "Oui"}, {"0", "Non"}, {"1", "Oui"}} {
		got := fixture.ui.plainText(ctx, &listState{m: metadata}, labelResolver{}, schema.Field{Type: schema.Bool, Name: "active"}, map[string]any{"active": scenario.value}, "active")
		if got != scenario.want {
			t.Errorf("text for %v = %q, want %q", scenario.value, got, scenario.want)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<!doctype html><body>"+body)
	}))
	t.Cleanup(server.Close)
	var text string
	if err := chromedp.Run(chromedptest.Context(t), chromedp.Navigate(server.URL), chromedp.Text("body", &text)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Active: Oui") || !strings.Contains(text, "Active: Non") || strings.Contains(text, "true") || strings.Contains(text, "false") {
		t.Fatalf("card text is not localized: %s", text)
	}
}
