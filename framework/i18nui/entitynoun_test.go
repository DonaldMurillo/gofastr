package i18nui

import (
	"context"
	"testing"
)

// EntityNoun is the name inside a sentence ("11 customers"): the Display
// name or the slug lowercases each word capitalized only at its start,
// and a word with any other capital (an acronym) keeps its casing.
func TestEntityNounLowersPlainWords(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, display string
		plural        bool
		want          string
	}{
		{"customers", "", true, "customers"},
		{"customers", "", false, "customer"},
		{"line_items", "", true, "line items"},
		{"customers", "Customers", true, "customers"},
		{"line_items", "Line Items", true, "line items"},
		{"api_keys", "API Keys", true, "API keys"},
		{"oauth_apps", "OAuth Apps", true, "OAuth apps"},
		{"invoices", "Invoice", false, "invoice"},
	} {
		if got := EntityNoun(ctx, nil, tc.name, tc.display, tc.plural); got != tc.want {
			t.Errorf("EntityNoun(%q, %q, %v) = %q, want %q", tc.name, tc.display, tc.plural, got, tc.want)
		}
	}
}

// A catalog entry is the translator's casing: German capitalizes nouns,
// so the catalog's text is used as written.
func TestEntityNounKeepsCatalogCasing(t *testing.T) {
	ctx, tr := frCatalog("entity.invoices.plural", "entity.invoices.singular")
	if got := EntityNoun(ctx, tr, "invoices", "Invoices", true); got != "fr·entity.invoices.plural" {
		t.Fatalf("plural catalog entry rewritten: %q", got)
	}
	if got := EntityNoun(ctx, tr, "invoices", "Invoice", false); got != "fr·entity.invoices.singular" {
		t.Fatalf("singular catalog entry rewritten: %q", got)
	}
}
