package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A locale negotiated from the headers declares them in Vary, so a shared
// cache stores one copy per language; a locale the URL names needs none.
func TestMiddlewareVariesOnLocaleHeaders(t *testing.T) {
	tr := NewTranslator(NewMapCatalog(), "en")
	serve := func(ctx context.Context) []string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		Middleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
		return rec.Header().Values("Vary")
	}
	if got := strings.Join(serve(context.Background()), ","); got != "Accept-Language,X-Locale" {
		t.Errorf("header-negotiated Vary = %q, want Accept-Language,X-Locale", got)
	}
	set := PathLocales{Default: "en", Prefixed: []string{"es"}}
	if got := serve(WithPathLocale(context.Background(), set, "es")); len(got) != 0 {
		t.Errorf("path locale Vary = %q, want none", got)
	}
}
