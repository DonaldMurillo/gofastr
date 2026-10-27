package i18n

import (
	"context"
	"testing"
)

var cartItems = Define("plural_miss_test.cart.items", "{{count}} items")

// A plural message with neither the count's form nor "other", and no
// Text, rendered "" and Phrase.Text took it as translated: French
// {"one": …} with count 2 showed nothing instead of the English.
func TestPluralWithNoFormFallsBack(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", cartItems.Key(), Message{Plural: map[string]string{"one": "{{count}} article"}})
	tr := NewTranslator(cat, "en")
	ctx := WithTranslator(WithContext(context.Background(), Locale{Tag: "fr"}), tr)
	if got := cartItems.Text(ctx, map[string]any{"count": 2}); got != "2 items" {
		t.Errorf("count 2 = %q, want the English 2 items", got)
	}
	if got := cartItems.Text(ctx, map[string]any{"count": 1}); got != "1 article" {
		t.Errorf("count 1 = %q, want the French form", got)
	}
	if got := cartItems.Text(ctx); got != "{{count}} items" {
		t.Errorf("no count = %q, want the English", got)
	}
}

// The catalog check reports a plural entry with no "other" form: every
// locale's plural rules end in other, so a count it misses renders the
// fallback.
func TestCheckReportsPluralWithoutOther(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", cartItems.Key(), Message{Plural: map[string]string{"one": "{{count}} article"}})
	got := CheckCatalog(cat, "fr")
	var hit bool
	for _, is := range got {
		if is.Key == cartItems.Key() && is.Problem == ProblemNoOther {
			hit = true
		}
	}
	if !hit {
		t.Errorf("CheckCatalog = %+v, want %s reported as %q", got, cartItems.Key(), ProblemNoOther)
	}
}
