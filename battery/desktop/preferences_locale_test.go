package desktop

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A number preference's range hint is the screen's own words, so it
// speaks the reader's language: French gets "Entre 1 et 180.".
func TestIntRangeHintSpeaksLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "desktop.pref_between", i18n.Message{Text: "Entre {{min}} et {{max}}."})
	cat.Set("fr", "desktop.pref_at_least", i18n.Message{Text: "Au moins {{min}}."})
	cat.Set("fr", "desktop.pref_at_most", i18n.Message{Text: "Au plus {{max}}."})
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en")), i18n.Locale{Tag: "fr"})
	one, top := 1, 180
	for _, c := range []struct {
		d    Preference
		want string
	}{
		{Preference{Key: "a", Label: "Minutes", Kind: PreferenceInt, Min: &one, Max: &top}, "Entre 1 et 180."},
		{Preference{Key: "b", Label: "Minutes", Kind: PreferenceInt, Min: &one}, "Au moins 1."},
		{Preference{Key: "c", Label: "Minutes", Kind: PreferenceInt, Max: &top, Help: "Durée."}, "Durée. Au plus 180."},
	} {
		if got := string(intField(ctx, c.d, 5)); !strings.Contains(got, c.want) {
			t.Errorf("%s: hint missing %q:\n%s", c.d.Key, c.want, got)
		}
	}
}
