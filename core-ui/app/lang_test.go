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

// One Lang for the whole app is wrong as soon as the app serves two languages:
// every translated page claimed the site language, so a screen reader read
// Spanish with English rules and any indexer reading <html lang> filed the page
// under the wrong language. LangFunc resolves the tag per route; ScreenLanger
// lets a page declare its own, after Load.

// langScreen declares its own document language.
type langScreen struct {
	lang string
}

func (s *langScreen) Render() render.HTML { return render.Raw("<p>hola</p>") }
func (s *langScreen) ScreenLang() string  { return s.lang }

// loadLangScreen picks its language up in Load, the way a dynamic route reads
// it off the content it fetched.
type loadLangScreen struct {
	lang string
}

func (s *loadLangScreen) Render() render.HTML { return render.Raw("<p>olá</p>") }
func (s *loadLangScreen) ScreenLang() string  { return s.lang }
func (s *loadLangScreen) Load(context.Context) error {
	s.lang = "pt-BR"
	return nil
}

func TestLangFuncResolvesPerRoute(t *testing.T) {
	a := NewApp("LangApp").WithLang("en").WithLangFunc(func(path string) string {
		if strings.HasPrefix(path, "/es/") {
			return "es"
		}
		return ""
	})
	a.RegisterScreen(NewScreen("/docs", &stubComponent{html: render.Raw("<p>hi</p>")}), nil)
	a.RegisterScreen(NewScreen("/es/docs", &stubComponent{html: render.Raw("<p>hola</p>")}), nil)

	for path, want := range map[string]string{
		"/docs":    `<html lang="en">`,
		"/es/docs": `<html lang="es">`,
	} {
		html, err := a.RenderPage(context.Background(), path)
		if err != nil {
			t.Fatalf("RenderPage(%q): %v", path, err)
		}
		if !strings.Contains(string(html), want) {
			t.Errorf("RenderPage(%q) must contain %s; got:\n%s", path, want, html)
		}
	}
}

func TestLangFuncEmptyResultFallsBackToLang(t *testing.T) {
	a := NewApp("LangApp").WithLang("fr").WithLangFunc(func(string) string { return "" })
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>bonjour</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="fr">`) {
		t.Errorf("an empty LangFunc answer must fall back to Lang; got:\n%s", html)
	}
}

func TestScreenLangerOverridesLangFunc(t *testing.T) {
	a := NewApp("LangApp").WithLang("en").WithLangFunc(func(string) string { return "de" })
	a.RegisterScreen(NewScreen("/", &langScreen{lang: "ja"}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="ja">`) {
		t.Errorf("ScreenLanger must win over LangFunc; got:\n%s", html)
	}
}

func TestScreenLangerEmptyFallsThrough(t *testing.T) {
	a := NewApp("LangApp").WithLangFunc(func(string) string { return "de" })
	a.RegisterScreen(NewScreen("/", &langScreen{}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="de">`) {
		t.Errorf("an empty ScreenLang must fall through to LangFunc; got:\n%s", html)
	}
}

func TestScreenLangerReadAfterLoad(t *testing.T) {
	a := NewApp("LangApp")
	a.RegisterScreen(NewScreen("/", &loadLangScreen{}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if !strings.Contains(string(html), `<html lang="pt-BR">`) {
		t.Errorf("ScreenLang must be read after Load; got:\n%s", html)
	}
}

// A LangFunc is usually derived from the request path, so its answer is
// effectively caller-controlled and lands in an attribute. It has to be escaped
// there like any other attribute value.
func TestLangIsAttributeEscaped(t *testing.T) {
	a := NewApp("LangApp").WithLangFunc(func(string) string {
		return `en"><script>alert(1)</script>`
	})
	a.RegisterScreen(NewScreen("/", &stubComponent{html: render.Raw("<p>hi</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if strings.Contains(string(html), "<script>") {
		t.Errorf("SECURITY: [app] a lang value broke out of the attribute:\n%s", html)
	}
}

func TestLangForPath(t *testing.T) {
	plain := NewApp("LangApp")
	if got := plain.LangForPath("/anything"); got != "en" {
		t.Errorf("unset LangFunc = %q, want en", got)
	}

	a := NewApp("LangApp").WithLang("en").WithLangFunc(func(path string) string {
		if path == "/es" {
			return "es"
		}
		return ""
	})
	if got := a.LangForPath("/es"); got != "es" {
		t.Errorf("LangForPath(/es) = %q, want es", got)
	}
	if got := a.LangForPath("/"); got != "en" {
		t.Errorf("LangForPath(/) = %q, want en", got)
	}
}
