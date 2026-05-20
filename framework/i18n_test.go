package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
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
	a.Router.Get("/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		a.Router.ServeHTTP(rr, req)
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
