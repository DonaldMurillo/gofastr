package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNegotiateBareTagNeverWidens: Negotiate is RFC 4647 Lookup, so a
// bare tag never reaches a regional catalog. A catalog with only fr-CA
// serves Accept-Language: fr the fallback, until the app aliases fr to it.
func TestNegotiateBareTagNeverWidens(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr-CA", "k", Message{Text: "fr-ca"})
	tr := NewTranslator(c, "en")
	negotiate := func(header string) string {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", header)
		return Negotiate(tr, r).Tag
	}
	if got := negotiate("fr"); got != "en" {
		t.Fatalf("fr with only an fr-CA catalog negotiated %q, want the fallback en", got)
	}
	if got := negotiate("fr-CA"); got != "fr-ca" {
		t.Fatalf("fr-CA negotiated %q, want fr-ca", got)
	}

	c.Entries["fr"] = c.Entries["fr-ca"]
	if got := negotiate("fr"); got != "fr" {
		t.Fatalf("fr with an fr alias negotiated %q, want fr", got)
	}
	if got := tr.T(WithContext(t.Context(), Locale{Tag: "fr"}), "k"); got != "fr-ca" {
		t.Fatalf("the fr alias translated k to %q, want the fr-CA text", got)
	}
}
