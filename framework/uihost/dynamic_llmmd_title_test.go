package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// loadedTitleComp is a dynamic screen whose title and content only exist
// after SetParams + Load — the shape every dynamic detail screen (and the
// site's /docs/{path...} catch-all) has.
type loadedTitleComp struct {
	id    string
	title string
}

func (c *loadedTitleComp) SetParams(m map[string]string) { c.id = m["id"] }
func (c *loadedTitleComp) Load(ctx context.Context) error {
	c.title = "Thing " + c.id
	return nil
}
func (c *loadedTitleComp) ScreenTitle() string { return c.title }
func (c *loadedTitleComp) ScreenDescription() string {
	if c.id == "" {
		return ""
	}
	return "All about thing " + c.id
}
func (c *loadedTitleComp) Render() render.HTML {
	return render.HTML("<h1>Thing " + c.id + "</h1><p>Loaded detail body.</p>")
}

func newDynamicTitleHost(t *testing.T) *UIHost {
	t.Helper()
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	// Per-screen llm.md is public only via the explicit opt-in — the
	// dynamic-route fallback must honor the same gate.
	return New(a, WithPublicLLMMD())
}

// SPA partial responses must carry the post-Load title, not the empty
// registration-time title of a dynamic route.
func TestPartialTitleLoadedForDynamic(t *testing.T) {
	ds := newDynamicTitleHost(t)
	req := httptest.NewRequest(http.MethodGet, "/things/42", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial GET /things/42: got %d, want 200", rec.Code)
	}
	got, err := url.PathUnescape(rec.Header().Get("X-Gofastr-Title"))
	if err != nil {
		t.Fatalf("unescape title: %v", err)
	}
	if !strings.Contains(got, "Thing 42") {
		t.Errorf("X-Gofastr-Title = %q, want it to carry the loaded title", got)
	}
}

// A concrete URL of a dynamic route serves a per-instance llm.md with the
// loaded title and content (static routes get explicit handlers at mount;
// dynamic ones land in the serveOrRender fallback).
func TestDynamicRouteLLMMDServed(t *testing.T) {
	ds := newDynamicTitleHost(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/42/llm.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /things/42/llm.md: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Thing 42") {
		t.Errorf("llm.md missing loaded title/content:\n%s", body)
	}
	if !strings.Contains(body, "Loaded detail body.") {
		t.Errorf("llm.md missing rendered content:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "markdown") {
		t.Errorf("Content-Type = %q, want markdown", ct)
	}
}

// An llm.md URL whose base resolves to nothing keeps 404ing.
func TestDynamicLLMMDUnknownBase404s(t *testing.T) {
	ds := newDynamicTitleHost(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nowhere/llm.md", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nowhere/llm.md: got %d, want 404", rec.Code)
	}
}

// Without WithPublicLLMMD the dynamic-route fallback must stay closed —
// same default as the static per-screen handlers.
func TestDynamicLLMMDGatedByDefault(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	ds := New(a)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/42/llm.md", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("without WithPublicLLMMD: got %d, want 404", rec.Code)
	}
}

// Full-page renders of dynamic routes must emit the LOADED instance's
// meta description — the registration-time description is empty for
// every dynamic route (the docs catch-all regression).
func TestDynamicMetaDescriptionLoaded(t *testing.T) {
	ds := newDynamicTitleHost(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /things/42: got %d, want 200", rec.Code)
	}
	want := `<meta name="description" content="All about thing 42">`
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("page missing loaded meta description %q", want)
	}
}

// Markdown content negotiation must evaluate the policy chain — a gated
// screen's negotiated markdown degrades to the withheld doc instead of
// serving its rendered content.
func TestNegotiatedMarkdownHonorsPolicy(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	deny := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionBlock, Status: 403}
	})
	a.RegisterScreen(app.NewScreen("/secret", &secretRenderComp{}).WithPolicy(deny), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	req := httptest.NewRequest(http.MethodGet, "/secret", nil)
	req.Header.Set("Accept", "text/markdown")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "internal instructions") {
		t.Errorf("gated screen leaked content via markdown negotiation:\n%s", rec.Body.String())
	}
}

// Negotiated markdown for a dynamic route serves the loaded instance's
// content, not the pattern placeholder.
func TestNegotiatedMarkdownLoadsInstance(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	a.Register("/things/{id}", &loadedTitleComp{}, nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

	req := httptest.NewRequest(http.MethodGet, "/things/42", nil)
	req.Header.Set("Accept", "text/markdown")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Loaded detail body.") {
		t.Errorf("negotiated markdown missing loaded content:\n%s", body)
	}
}

type secretRenderComp struct{}

func (c *secretRenderComp) Render() render.HTML {
	return render.HTML("<p>internal instructions</p>")
}

// The dynamic llm.md's SEO front matter resolves against the LOADED
// instance — the HTML head and the markdown must stay in lockstep.
func TestDynamicLLMMDFrontMatterLoaded(t *testing.T) {
	ds := newDynamicTitleHost(t)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/things/42/llm.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /things/42/llm.md: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "All about thing 42") {
		t.Errorf("front matter missing the loaded description:\n%s", rec.Body.String())
	}
}

// A gated screen's llm.md carries NO front matter and no title metadata.
func TestGatedLLMMDHasNoFrontMatter(t *testing.T) {
	a := app.NewApp("dyn")
	a.Register("/", &testHomeComp{}, nil)
	deny := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionBlock, Status: 403}
	})
	scr := app.NewScreen("/secret/{id}", &loadedTitleComp{}).WithPolicy(deny)
	scr.Title = "Project NIGHTFALL"
	a.RegisterScreen(scr, nil)
	ds := New(a, WithPublicLLMMD())

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/secret/42/llm.md", nil))
	body := rec.Body.String()
	if strings.Contains(body, "NIGHTFALL") || strings.Contains(body, "Thing 42") {
		t.Errorf("gated llm.md leaked metadata:\n%s", body)
	}
	if !strings.Contains(body, "withheld") {
		t.Errorf("expected withheld doc:\n%s", body)
	}
}
