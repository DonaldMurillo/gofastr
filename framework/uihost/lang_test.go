package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// The uihost document shells (404, PWA offline, embed) hardcoded
// <html lang="en">. Lang is now a host option (WithLang) that overrides the
// app's own Lang, defaulting to "en", threaded to every shell emit site.

func TestEffectiveLangResolvesConfig(t *testing.T) {
	if got := New(app.NewApp("x")).EffectiveLang(); got != "en" {
		t.Fatalf("default lang = %q, want en", got)
	}
	if got := New(app.NewApp("x").WithLang("de")).EffectiveLang(); got != "de" {
		t.Fatalf("app Lang should propagate = %q, want de", got)
	}
	if got := New(app.NewApp("x").WithLang("de"), WithLang("ja")).EffectiveLang(); got != "ja" {
		t.Fatalf("host WithLang must override app Lang = %q, want ja", got)
	}
}

func TestServeNotFoundUsesConfiguredLang(t *testing.T) {
	ds := New(app.NewApp("x"), WithLang("es"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	ds.serveNotFound(rec, req, "/nope")
	if !strings.Contains(rec.Body.String(), `<html lang="es">`) {
		t.Errorf("404 shell must use the configured lang; got:\n%s", rec.Body.String())
	}
}

func TestPWAOfflineHTMLUsesConfiguredLang(t *testing.T) {
	ds := New(app.NewApp("x"), WithLang("pt"), WithPWA(PWAConfig{Name: "PWA"}))
	html := ds.PWAOfflineHTML()
	if !strings.Contains(html, `<html lang="pt">`) {
		t.Errorf("PWA offline shell must use the configured lang; got:\n%s", html)
	}
}
