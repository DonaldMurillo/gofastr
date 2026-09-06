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

// A host serving two languages needs its shells to follow the route, not the
// site: a 404 under /es/ that says lang="en" is the same WCAG 3.1.1 failure the
// pages had. WithLangFunc resolves it per path, and the host also honours the
// app's own LangFunc so a site declares its languages in one place.

func TestLangForPathPrecedence(t *testing.T) {
	esPrefix := func(path string) string {
		if strings.HasPrefix(path, "/es/") {
			return "es"
		}
		return ""
	}

	ds := New(app.NewApp("x").WithLang("en"), WithLangFunc(esPrefix))
	if got := ds.LangForPath("/es/docs"); got != "es" {
		t.Errorf("host WithLangFunc = %q, want es", got)
	}
	if got := ds.LangForPath("/docs"); got != "en" {
		t.Errorf("an empty answer must fall back to EffectiveLang = %q, want en", got)
	}
	if got := ds.LangForPath(""); got != "en" {
		t.Errorf("a pathless shell must use EffectiveLang = %q, want en", got)
	}

	fromApp := New(app.NewApp("x").WithLang("en").WithLangFunc(esPrefix))
	if got := fromApp.LangForPath("/es/docs"); got != "es" {
		t.Errorf("the app's LangFunc must reach host shells = %q, want es", got)
	}

	hostWins := New(app.NewApp("x").WithLangFunc(func(string) string { return "de" }),
		WithLangFunc(func(string) string { return "ja" }))
	if got := hostWins.LangForPath("/x"); got != "ja" {
		t.Errorf("host WithLangFunc must override the app's = %q, want ja", got)
	}

	unset := New(app.NewApp("x").WithLang("de"))
	if got := unset.LangForPath("/anything"); got != "de" {
		t.Errorf("no LangFunc anywhere must match EffectiveLang = %q, want de", got)
	}
}

func TestServeNotFoundUsesPerRouteLang(t *testing.T) {
	ds := New(app.NewApp("x"), WithLangFunc(func(path string) string {
		if strings.HasPrefix(path, "/es/") {
			return "es"
		}
		return ""
	}))
	for path, want := range map[string]string{
		"/es/nope": `<html lang="es">`,
		"/nope":    `<html lang="en">`,
	} {
		rec := httptest.NewRecorder()
		ds.serveNotFound(rec, httptest.NewRequest(http.MethodGet, path, nil), path)
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("404 for %q must contain %s; got:\n%s", path, want, rec.Body.String())
		}
	}
}

// The 404 shell's lang is now derived from the requested path, so a LangFunc
// that echoes part of it puts caller-controlled text in an attribute.
func TestShellLangIsAttributeEscaped(t *testing.T) {
	ds := New(app.NewApp("x"), WithLangFunc(func(string) string {
		return `en"><script>alert(1)</script>`
	}))
	rec := httptest.NewRecorder()
	ds.serveNotFound(rec, httptest.NewRequest(http.MethodGet, "/nope", nil), "/nope")
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("SECURITY: [uihost] a lang value broke out of the attribute:\n%s", rec.Body.String())
	}
}

func TestPWAOfflineHTMLUsesConfiguredLang(t *testing.T) {
	ds := New(app.NewApp("x"), WithLang("pt"), WithPWA(PWAConfig{Name: "PWA"}))
	html := ds.PWAOfflineHTML()
	if !strings.Contains(html, `<html lang="pt">`) {
		t.Errorf("PWA offline shell must use the configured lang; got:\n%s", html)
	}
}
