package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// i18n.md said an entry with characters outside lowercase letters,
// digits and '-' is skipped, which reads as "FR is refused". The bound
// runs after normalisation (lower-case, trim), so an upper-case or
// padded tag from any source is honoured, and only a character that
// survives normalisation outside the set is refused.
func TestTagBoundRunsAfterNormalising(t *testing.T) {
	c := NewMapCatalog()
	c.Set("fr", "k", Message{Text: "fr"})
	c.Set("en-us", "k", Message{Text: "en-us"})
	tr := NewTranslator(c, "en")
	for _, tc := range []struct{ header, value, want string }{
		{"X-Locale", "FR", "fr"},
		{"X-Locale", " EN-US ", "en-us"},
		{"Accept-Language", "FR;q=0.9", "fr"},
		{"Accept-Language", "EN-US", "en-us"},
		{"X-Locale", "en_US", "en"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(tc.header, tc.value)
		if got := Negotiate(tr, r).Tag; got != tc.want {
			t.Errorf("%s %q negotiated %q, want %q", tc.header, tc.value, got, tc.want)
		}
	}
	res := LocaleResolver{Resolve: func(*http.Request) (string, bool) { return "Fr", true }}
	if got := Negotiate(tr, httptest.NewRequest(http.MethodGet, "/", nil), WithLocaleResolver(res)).Tag; got != "fr" {
		t.Errorf("resolver %q negotiated %q, want fr", "Fr", got)
	}
}
