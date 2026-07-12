package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestApp_TWithoutTranslatorReturnsKey(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware())
	if got := a.T(nil, "x"); got != "x" {
		t.Fatalf("no translator: got %q", got)
	}
}

func TestApp_WithI18nWiresMiddlewareAndExposesT(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "hi", i18n.Message{Text: "Hello"})
	cat.Set("fr", "hi", i18n.Message{Text: "Bonjour"})
	tr := i18n.NewTranslator(cat, "en")

	a := NewApp(WithI18n(tr))
	a.Router().Get("/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(a.T(r.Context(), "hi")))
	}))

	cases := []struct {
		accept string
		want   string
	}{
		{"fr;q=1.0,en;q=0.5", "Bonjour"},
		{"en", "Hello"},
		{"", "Hello"}, // fallback to en
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/hello", nil)
		if tc.accept != "" {
			req.Header.Set("Accept-Language", tc.accept)
		}
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
		if rr.Body.String() != tc.want {
			t.Errorf("accept=%q: got %q want %q", tc.accept, rr.Body.String(), tc.want)
		}
	}
}

func TestApp_WithI18nPanicsWithWithoutDefaultMiddleware(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic when WithI18n is paired with WithoutDefaultMiddleware")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "WithoutDefaultMiddleware") {
			t.Fatalf("panic should mention the conflict; got %v", r)
		}
	}()
	tr := i18n.NewTranslator(i18n.NewMapCatalog(), "en")
	_ = NewApp(WithoutDefaultMiddleware(), WithI18n(tr))
}

// TestApp_I18nBridgeWiresTranslatorIntoCtx proves the DefaultMiddleware
// bridge: an App built with WithI18n stashes the translator on the
// request ctx so framework/ui components resolve via i18nui.T(r.Context())
// using the caller's locale. Without the bridge the component renders
// English regardless of catalog.
func TestApp_I18nBridgeWiresTranslatorIntoCtx(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "ui.pagination.next", i18n.Message{Text: "Next"})
	cat.Set("fr", "ui.pagination.next", i18n.Message{Text: "Suivant"})
	tr := i18n.NewTranslator(cat, "en")

	a := NewApp(WithI18n(tr))
	a.Router().Get("/lbl", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(i18nui.T(r.Context(), i18nui.KeyPaginationNext)))
	}))

	req := httptest.NewRequest(http.MethodGet, "/lbl", nil)
	req.Header.Set("Accept-Language", "fr")
	rr := httptest.NewRecorder()
	a.Router().ServeHTTP(rr, req)
	if got := rr.Body.String(); got != "Suivant" {
		t.Fatalf("bridge: got %q, want Suivant (translator not wired into ctx)", got)
	}
}

// TestApp_LocaleResolverCookieWins: WithLocaleResolver(CookieLocale)
// makes a stored per-user locale win over the browser's
// Accept-Language, end-to-end through the wired middleware.
func TestApp_LocaleResolverCookieWins(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "greeting", i18n.Message{Text: "Hello"})
	cat.Set("fr", "greeting", i18n.Message{Text: "Bonjour"})
	cat.Set("de", "greeting", i18n.Message{Text: "Hallo"})
	tr := i18n.NewTranslator(cat, "en")

	a := NewApp(
		WithI18n(tr),
		WithLocaleResolver(i18n.CookieLocale("locale")),
	)
	a.Router().Get("/g", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(a.T(r.Context(), "greeting")))
	}))

	req := httptest.NewRequest(http.MethodGet, "/g", nil)
	req.Header.Set("Accept-Language", "de")
	req.AddCookie(&http.Cookie{Name: "locale", Value: "fr"})
	rr := httptest.NewRecorder()
	a.Router().ServeHTTP(rr, req)
	if got := rr.Body.String(); got != "Bonjour" {
		t.Fatalf("cookie locale should win over Accept-Language: got %q want Bonjour", got)
	}
}

// TestApp_WithLocaleResolverWithoutI18nPanics: using the option
// without a translator is a programmer error — fail loudly.
func TestApp_WithLocaleResolverWithoutI18nPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when WithLocaleResolver used without WithI18n")
		}
	}()
	_ = NewApp(WithLocaleResolver(i18n.CookieLocale("locale")))
}
