package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The <html lang="en"> attribute was hardcoded; an app that renders in another
// language still told screen readers the page was English (WCAG 3.1.1). Lang is
// now a config field defaulting to "en" and threaded into the document shell.

func TestRenderPageUsesConfiguredLang(t *testing.T) {
	a := NewApp("LangApp").WithLang("fr")
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>bonjour</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="fr">`) {
		t.Errorf("configured lang must appear in the <html> tag; got:\n%s", html)
	}
}

func TestRenderPageDefaultsToEnglishLang(t *testing.T) {
	a := NewApp("LangApp")
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>hi</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="en">`) {
		t.Errorf("absent lang must default to en; got:\n%s", html)
	}
}
