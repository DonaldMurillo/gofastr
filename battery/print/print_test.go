package print

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// ----- helpers --------------------------------------------------------------

// stubDoc is a trivial component returning fixed HTML.
type stubDoc struct{ html render.HTML }

func (s stubDoc) Render() render.HTML { return s.html }

// docBuild returns a Build func that always renders the given HTML.
func docBuild(html string) func(*http.Request) (component.Component, error) {
	return func(*http.Request) (component.Component, error) {
		return stubDoc{html: render.HTML(html)}, nil
	}
}

// mount mounts the battery on a fresh router and returns it as a handler.
func mount(t *testing.T, b *Battery) http.Handler {
	t.Helper()
	r := router.New()
	if err := b.RegisterRoutes(r); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	return r
}

// authed wraps a handler so every request carries a stand-in user.
func authed(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(handler.SetUser(r.Context(), struct{ ID string }{ID: "u1"}))
		h.ServeHTTP(w, r)
	})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// ----- tests ----------------------------------------------------------------

func TestMountsHTMLRoute(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("<h1>Hi</h1>"),
	})
	rec := get(t, mount(t, b), "/print/doc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("body does not start with doctype: %q", body[:min(40, len(body))])
	}
	if !strings.Contains(body, "<h1>Hi</h1>") {
		t.Errorf("body missing component output")
	}
}

func TestMountsPDFRoute(t *testing.T) {
	b := New(Config{DefaultAccess: Public, PDFRenderer: &fakeRenderer{}}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("<p>x</p>"),
	})
	rec := get(t, mount(t, b), "/print/doc/pdf")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
}

func TestParamReachesBuild(t *testing.T) {
	var gotID string
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "invoice", Path: "/invoice/{id}",
		Build: func(r *http.Request) (component.Component, error) {
			gotID = router.Param(r, "id")
			return stubDoc{html: "ok"}, nil
		},
	})
	get(t, mount(t, b), "/print/invoice/42")
	if gotID != "42" {
		t.Fatalf("Build saw id = %q, want 42", gotID)
	}
}

func TestBuildErrorIsCleanStatus(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) { return nil, ErrNotFound },
	})
	rec := get(t, mount(t, b), "/print/doc")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "runtime") || strings.Contains(rec.Body.String(), "goroutine") {
		t.Errorf("error body leaked internals: %q", rec.Body.String())
	}
}

func TestDocumentAfterInitPanics(t *testing.T) {
	b := New(Config{}).Document(Document{Name: "a", Path: "/a", Build: docBuild("x")})
	mount(t, b) // sets mounted=true
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on Document after Init")
		}
	}()
	b.Document(Document{Name: "b", Path: "/b", Build: docBuild("y")})
}

func TestTitleEscaped(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("x"),
		TitleFunc: func(*http.Request) string { return "<script>alert(1)</script>" },
	})
	body := get(t, mount(t, b), "/print/doc").Body.String()
	if strings.Contains(body, "<title><script>") {
		t.Errorf("title not escaped: %q", body)
	}
	if !strings.Contains(body, "<title>&lt;script&gt;") {
		t.Errorf("escaped title missing: %q", body)
	}
}
