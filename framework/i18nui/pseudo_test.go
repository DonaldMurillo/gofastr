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
	for k, v := range i18n.Phrases() {
		m, ok := c.Get("en-XA", k)
		if !ok || m.Text != Pseudo(v) {
			t.Fatalf("%s: got %q, %v", k, m.Text, ok)
		}
	}
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "en-xa"}), i18n.NewTranslator(c, "en"))
	if got := DialogSave.Text(ctx); got != Pseudo(DialogSave.English()) {
		t.Fatalf("T in the pseudo locale = %q", got)
	}
}

// An entity label with no catalog entry falls back to its English, and
// in the pseudo locale that English is pseudo-localized too: Meridian's
// column headers, status badges and view tabs stayed plain English under
// /en-xa/, so the overflow gate never measured them.
func TestPseudoAccentsLabelFallbacks(t *testing.T) {
	c := i18n.NewMapCatalog()
	AddPseudo(c, "en-XA")
	tr := i18n.NewTranslator(c, "en")
	at := func(tag string) context.Context {
		return i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: tag}), tr)
	}
	labels := []struct {
		name string
		of   func(context.Context) string
	}{
		{"field", func(ctx context.Context) string { return FieldLabel(ctx, tr, "invoices", "due_on", "") }},
		{"field display", func(ctx context.Context) string { return FieldLabel(ctx, tr, "invoices", "amount", "Amount due") }},
		{"enum value", func(ctx context.Context) string { return FieldValueLabel(ctx, tr, "invoices", "status", "past_due") }},
		{"view", func(ctx context.Context) string { return ViewLabel(ctx, tr, "invoices", "open", "") }},
		{"plural", func(ctx context.Context) string { return EntityPlural(ctx, tr, "invoices", "") }},
		{"noun", func(ctx context.Context) string { return EntityNoun(ctx, tr, "invoices", "", true) }},
	}
	for _, l := range labels {
		english := l.of(at("en"))
		if got := l.of(at("en-xa")); got != Pseudo(english) {
			t.Errorf("%s: en-XA = %q, want %q", l.name, got, Pseudo(english))
		}
		if got := l.of(at("es")); got != english {
			t.Errorf("%s: es = %q, want the English %q", l.name, got, english)
		}
	}
	if got := EntityDescription(at("en-xa"), tr, "invoices", ""); got != "" {
		t.Errorf("an absent description = %q, want empty", got)
	}
}
