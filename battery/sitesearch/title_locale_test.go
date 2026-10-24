package sitesearch

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The search page's tab title is the same word as its heading, in the
// reader's language.
func TestSearchTitleSpeaksLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", i18nui.SearchLabel.Key(), i18n.Message{Text: "Recherche"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
	s := &searchScreen{}
	if got := s.ScreenTitle(ctx); got != "Recherche" {
		t.Errorf("French title = %q, want Recherche", got)
	}
	if got := s.ScreenTitle(context.Background()); got != "Search" {
		t.Errorf("bare-ctx title = %q, want Search", got)
	}
}
