package uihost

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// Page markdown is rendered per caller (policy, Load, the session the
// cookie names), yet went out as Cache-Control: no-cache with no Vary:
// Cookie: a shared cache may store no-cache, so it could replay a
// signed-in reader's doc to the next agent. Every page-doc surface now
// answers as the HTML twin does.
func TestPageMarkdownIsNoStore(t *testing.T) {
	a := app.NewApp("md-nostore")
	a.RegisterScreen(app.NewScreen("/", &mdPageScreen{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/about", &mdPageScreen{}).WithTitle("About"), nil)
	a.RegisterScreen(app.NewScreen("/notes/{id}", &paramJSONComp{}).WithTitle("Note"), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	for _, c := range []struct{ name, url, accept string }{
		{"index", "/llm-pages.md", ""},
		{"exact doc", "/about/llm.md", ""},
		{"pattern doc", "/notes/:id/llm.md", ""},
		{"concrete doc", "/notes/42/llm.md", ""},
		{"negotiated", "/about", "text/markdown"},
	} {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		if c.accept != "" {
			req.Header.Set("Accept", c.accept)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", c.name, rec.Code)
			continue
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("%s: Cache-Control = %q, want private, no-store", c.name, cc)
		}
		if !slices.Contains(rec.Header().Values("Vary"), "Cookie") {
			t.Errorf("%s: Vary = %q, want Cookie among them", c.name, rec.Header().Values("Vary"))
		}
	}
}
