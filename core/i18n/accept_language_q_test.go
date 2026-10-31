package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func negotiateHeader(tr *Translator, header string) string {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", header)
	return Negotiate(tr, r).Tag
}

// A wildcard matches any catalog locale the header did not refuse with
// q=0; it used to match nothing, so the refused fallback was served.
func TestWildcardSkipsRefusedLocale(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	if got := negotiateHeader(tr, "en;q=0, *;q=1"); got != "fr" {
		t.Errorf("en;q=0, * negotiated %q, want fr", got)
	}
	if got := negotiateHeader(tr, "de, *;q=0.5"); got != "en" {
		t.Errorf("de, * negotiated %q, want the fallback en", got)
	}
}

// A locale refused with q=0 stays refused when a longer accepted tag
// truncates to it: en-GB falling back to the refused en served it ahead
// of the accepted fr (2026-10-27 C11).
func TestTruncationSkipsRefusedLocale(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	tr := NewTranslator(c, "en")

	if got := negotiateHeader(tr, "en;q=0, en-GB;q=0.9, fr;q=0.8"); got != "fr" {
		t.Errorf("en;q=0, en-GB, fr negotiated %q, want fr", got)
	}
	if got := negotiateHeader(tr, "en-GB;q=0, en-GB-oxendict, fr;q=0.8"); got != "en" {
		t.Errorf("refusing en-GB negotiated %q, want en (only en-GB was refused)", got)
	}
}

// A q the grammar does not allow drops its entry; it used to keep the
// default q=1 and outrank every well-formed entry.
func TestMalformedQDropsEntry(t *testing.T) {
	c := NewMapCatalog()
	c.Set("en", "k", Message{Text: "en"})
	c.Set("fr", "k", Message{Text: "fr"})
	c.Set("de", "k", Message{Text: "de"})
	tr := NewTranslator(c, "de")

	for _, h := range []string{"fr;q=garbage, en;q=0.8", "fr;q=NaN, en;q=0.8", "fr;q=2, en;q=0.8"} {
		if got := negotiateHeader(tr, h); got != "en" {
			t.Errorf("%q negotiated %q, want en", h, got)
		}
	}
}
