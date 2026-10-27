package i18n

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func varyOf(tr *Translator, opts ...NegotiateOption) []string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Middleware(tr, opts...)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	return rec.Header().Values("Vary")
}

// A locale chosen by a cookie made the response depend on the Cookie
// header, but Vary named only the locale headers, so a shared cache
// replayed one reader's cookie-chosen language to every other reader.
func TestCookieResolverVariesOnCookie(t *testing.T) {
	tr := NewTranslator(NewMapCatalog(), "en")
	if got := varyOf(tr, WithLocaleResolver(CookieLocale("locale"))); !slices.Contains(got, "Cookie") {
		t.Errorf("Vary with a cookie resolver = %q, want Cookie in it", got)
	}
	if got := varyOf(tr); slices.Contains(got, "Cookie") {
		t.Errorf("Vary with no resolver = %q, want no Cookie", got)
	}
	own := LocaleResolver{Resolve: func(*http.Request) (string, bool) { return "", false }, Vary: []string{"x-user-lang"}}
	if got := varyOf(tr, WithLocaleResolver(own)); !slices.Contains(got, "X-User-Lang") {
		t.Errorf("Vary with a header resolver = %q, want X-User-Lang in it", got)
	}
}
