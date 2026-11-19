package i18n

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSweepI18nAcceptLanguageSegmentsCapped(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("en", "k", Message{Text: "hi"})
	cat.Set("fr", "k", Message{Text: "salut"})
	tr := NewTranslator(cat, "en")
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", strings.Repeat(",", 600)+"fr")
	if got := Negotiate(tr, r).Tag; got != "en" {
		t.Fatalf("Negotiate read past 512 empty segments to %q", got)
	}
	r.Header.Set("Accept-Language", strings.Repeat(",", 100)+"fr")
	if got := Negotiate(tr, r).Tag; got != "fr" {
		t.Fatalf("Negotiate = %q, want fr within the segment budget", got)
	}
}

var sweepRefusal = Define("sweep.test.refusal", "the upload is too large")

func TestSweepI18nHTTPErrorWritesOwnPhraseUntranslated(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPError(w, httptest.NewRequest("GET", "/", nil), 413, sweepRefusal)
	if got := strings.TrimSpace(w.Body.String()); got != "the upload is too large" {
		t.Fatalf("body = %q, want the phrase's English", got)
	}
}
