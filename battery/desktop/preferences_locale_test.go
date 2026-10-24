package desktop

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
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
		{Preference{Key: "a", Label: testPhrase("Minutes"), Kind: PreferenceInt, Min: &one, Max: &top}, "Entre 1 et 180."},
		{Preference{Key: "b", Label: testPhrase("Minutes"), Kind: PreferenceInt, Min: &one}, "Au moins 1."},
		{Preference{Key: "c", Label: testPhrase("Minutes"), Kind: PreferenceInt, Max: &top, Help: testPhrase("Durée.")}, "Durée. Au plus 180."},
	} {
		if got := string(intField(ctx, c.d, 5)); !strings.Contains(got, c.want) {
			t.Errorf("%s: hint missing %q:\n%s", c.d.Key, c.want, got)
		}
	}
}

// A preference's label and help are the app's words for the reader:
// every kind renders them in the reader's language.
func TestPreferenceLabelSpeaksLocale(t *testing.T) {
	label, help := testPhrase("Sound"), testPhrase("Played when a session ends.")
	cat := i18n.NewMapCatalog()
	cat.Set("fr", label.Key(), i18n.Message{Text: "Son"})
	cat.Set("fr", help.Key(), i18n.Message{Text: "Joué à la fin d'une séance."})
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en")), i18n.Locale{Tag: "fr"})
	d := Preference{Key: "sound", Label: label, Help: help}
	for name, got := range map[string]render.HTML{
		"bool":   boolField(ctx, d, true),
		"int":    intField(ctx, d, 5),
		"choice": choiceField(ctx, d, ""),
		"string": stringField(ctx, d, ""),
	} {
		for _, want := range []string{">Son<", "Joué à la fin d&#39;une séance."} {
			if !strings.Contains(string(got), want) {
				t.Errorf("%s: missing %q:\n%s", name, want, got)
			}
		}
	}
}
