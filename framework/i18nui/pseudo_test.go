package i18nui

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Pseudo accents every letter, grows the text by at least a third and
// brackets it, and leaves {placeholders} and {{placeholders}} alone so
// interpolation still finds them.
func TestPseudo(t *testing.T) {
	got := Pseudo("Delete {count} {entity}?")
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("not bracketed: %q", got)
	}
	if !strings.Contains(got, "{count}") || !strings.Contains(got, "{entity}") {
		t.Fatalf("a placeholder changed: %q", got)
	}
	if strings.ContainsAny(strings.NewReplacer("{count}", "", "{entity}", "").Replace(got), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatalf("a letter kept its ASCII form: %q", got)
	}
	if g := Pseudo("Hi {{name}}"); !strings.Contains(g, "{{name}}") {
		t.Fatalf("a double-brace placeholder changed: %q", g)
	}
	for _, s := range []string{"Save", "Sort by name", "OK", "Rhythm", "CSV", "Rhythms by Lynch"} {
		if n, m := utf8.RuneCountInString(Pseudo(s)), utf8.RuneCountInString(s); n*3 < m*4 {
			t.Errorf("Pseudo(%q) grew %d → %d runes, under a third", s, m, n)
		}
	}
	if Pseudo("") != "" {
		t.Error("an empty string stays empty")
	}
}

// AddPseudo puts every default under the tag, so a page rendered in it
// shows no plain English from the kit.
func TestAddPseudoCoversDefaults(t *testing.T) {
	c := i18n.NewMapCatalog()
	AddPseudo(c, "en-XA")
	for k, v := range Defaults {
		m, ok := c.Get("en-XA", string(k))
		if !ok || m.Text != Pseudo(v) {
			t.Fatalf("%s: got %q, %v", k, m.Text, ok)
		}
	}
	ctx := WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "en-xa"}), i18n.NewTranslator(c, "en"))
	if got := T(ctx, KeyDialogSave); got != Pseudo(Defaults[KeyDialogSave]) {
		t.Fatalf("T in the pseudo locale = %q", got)
	}
}
