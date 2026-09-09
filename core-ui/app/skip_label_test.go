package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The skip link is the first thing a keyboard user tabs to, and it was
// hardcoded in English (#411): a Spanish or Arabic reader met English as
// their first string. The label now resolves like the document language:
// a per-route func, else an app-wide label, else the English default.

func TestSkipLabelForPath(t *testing.T) {
	cases := []struct {
		name string
		app  *App
		path string
		want string
	}{
		{"default", NewApp("t"), "/x", "Skip to main content"},
		{"app label", NewApp("t").WithSkipLabel("Zum Inhalt springen"), "/x", "Zum Inhalt springen"},
		{"func wins", NewApp("t").
			WithSkipLabel("Skip").
			WithSkipLabelFunc(func(string) string { return "Saltar al contenido principal" }), "/x",
			"Saltar al contenido principal"},
		{"func empty falls back to label", NewApp("t").
			WithSkipLabel("Skip").
			WithSkipLabelFunc(func(string) string { return "  " }), "/x", "Skip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.app.SkipLabelForPath(tc.path); got != tc.want {
				t.Errorf("SkipLabelForPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestSkipLinkUsesSkipLabelForPath(t *testing.T) {
	a := NewApp("t").WithSkipLabelFunc(func(path string) string {
		if strings.HasPrefix(path, "/es/") {
			return "Saltar al contenido principal"
		}
		return ""
	})
	a.RegisterScreen(NewScreen("/es/x", &stubComponent{html: render.Raw("<p>hola</p>")}), nil)

	html, err := a.RenderPage(context.Background(), "/es/x")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	s := string(html)
	if !strings.Contains(s, `data-skip-link`) {
		t.Fatalf("skip link missing:\n%s", s)
	}
	if !strings.Contains(s, `>Saltar al contenido principal</a>`) {
		t.Errorf("skip link must speak the route's language:\n%s", s)
	}
}
