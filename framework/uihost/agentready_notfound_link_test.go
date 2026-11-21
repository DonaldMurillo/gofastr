package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A not-found answer for a path a screen owns sends the Link header an
// unmatched URL gets, with no markdown alternate for the hidden route
// (#844): middleware calling ServeNotFound, a policy answering
// decide.NotFound, and a Load answering ErrNotFound.
func TestNotFoundLinkHeaderHasNoMarkdownAlternate(t *testing.T) {
	a := app.NewApp("lnf")
	a.SetDefaultLayout(app.NewLayout("root", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	a.RegisterScreen(app.NewScreen("/lnf-open", adminComp{}).WithTitle("Open"), nil)
	a.RegisterScreen(app.NewScreen("/lnf-mw", adminComp{}).WithTitle("Hidden"), nil)
	a.RegisterScreen(app.NewScreen("/lnf-policy", adminComp{}).WithTitle("Policy").
		WithPolicy(app.PolicyFunc(func(context.Context) app.Decision { return decide.NotFound() })), nil)
	a.Register("/lnf-load/:type", &barcodeComp{}, nil)
	ds := New(a, WithPublicLLMMD(),
		WithAgentReady(AgentReadyConfig{AgentCard: &AgentCardConfig{Name: "X", MCPEndpoint: "/mcp"}}))
	rt := router.New()
	rt.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/lnf-mw") {
				ds.ServeNotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ds.Mount(rt)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	if rec := get("/lnf-open"); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Link"), "/lnf-open/llm.md") {
		t.Fatalf("control: /lnf-open answered %d, Link=%q; want 200 with its markdown alternate", rec.Code, rec.Header().Get("Link"))
	}
	if rec := get("/lnf-load/qr"); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Link"), "/lnf-load/qr/llm.md") {
		t.Fatalf("control: /lnf-load/qr answered %d, Link=%q; want 200 with its markdown alternate", rec.Code, rec.Header().Get("Link"))
	}
	unmatched := get("/lnf-nope")
	want := unmatched.Header().Get("Link")
	if unmatched.Code != http.StatusNotFound || want == "" || strings.Contains(want, "llm.md") {
		t.Fatalf("setup: unmatched URL answered %d, Link=%q; want 404 with discovery links and no alternate", unmatched.Code, want)
	}
	for _, path := range []string{"/lnf-mw", "/lnf-policy", "/lnf-load/nope"} {
		rec := get(path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Link"); got != want {
			t.Errorf("%s: Link %q, unmatched URL %q", path, got, want)
		}
	}
}
