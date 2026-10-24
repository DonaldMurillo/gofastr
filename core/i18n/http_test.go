package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPErrorTranslates(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "error.not_found", Message{Text: "introuvable"})
	r := httptest.NewRequest("GET", "/x", nil)
	ctx := WithTranslator(WithContext(context.Background(), Locale{Tag: "fr"}), NewTranslator(cat, "en"))
	w := httptest.NewRecorder()
	HTTPError(w, r.WithContext(ctx), http.StatusNotFound, StatusPhrase(http.StatusNotFound))
	if w.Code != 404 || w.Body.String() != "introuvable\n" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

func TestHTTPErrorUntranslatedIsStatusText(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPError(w, httptest.NewRequest("GET", "/", nil), http.StatusMethodNotAllowed, StatusPhrase(http.StatusMethodNotAllowed))
	if w.Body.String() != "Method Not Allowed\n" {
		t.Fatalf("body %q", w.Body.String())
	}
}

func TestStatusPhraseCoversRefusals(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 405, 406, 409, 410, 411, 412, 413, 414, 415, 416, 422, 428, 429, 431, 500, 501, 502, 503, 504} {
		p := StatusPhrase(code)
		if p.IsZero() {
			t.Errorf("StatusPhrase(%d) is zero", code)
		}
	}
	if !StatusPhrase(299).IsZero() {
		t.Error("StatusPhrase(299) should be zero")
	}
}

func TestHTTPErrorZeroPhraseUsesStatusText(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPError(w, httptest.NewRequest("GET", "/", nil), http.StatusTeapot, Phrase{})
	if w.Body.String() != "I'm a teapot\n" {
		t.Fatalf("body %q", w.Body.String())
	}
}

func TestHTTPErrorNilRequest(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPError(w, nil, http.StatusForbidden, StatusPhrase(http.StatusForbidden))
	if w.Body.String() != "Forbidden\n" {
		t.Fatalf("body %q", w.Body.String())
	}
}
