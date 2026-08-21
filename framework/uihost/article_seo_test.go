package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// screenArticleComp declares itself an article via ScreenArticle only; the
// framework supplies the <article> wrapper + Article JSON-LD + og:type.
type screenArticleComp struct{}

func (*screenArticleComp) Render() render.HTML {
	return render.Raw("<h1>Hello</h1><p>The body.</p>")
}
func (*screenArticleComp) ScreenArticle() app.ArticleMeta {
	return app.ArticleMeta{
		Headline:      "Hello World",
		Author:        "Ada Lovelace",
		DatePublished: "2026-08-01T00:00:00Z",
		Description:   "A post.",
	}
}

func TestScreenArticleSynthesizesMetadata(t *testing.T) {
	a := app.NewApp("art")
	a.Register("/post", &screenArticleComp{}, nil)
	ds := New(a)

	req := httptest.NewRequest("GET", "/post", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	for name, want := range map[string]string{
		"<article> wrapper":      "<article>",
		"Article JSON-LD @type":  `"@type":"Article"`,
		"headline":               `"headline":"Hello World"`,
		"author as Person":       `"@type":"Person"`,
		"author name":            `"name":"Ada Lovelace"`,
		"datePublished":          `"datePublished":"2026-08-01T00:00:00Z"`,
		"description":            `"description":"A post."`,
		"og:type article":        `<meta property="og:type" content="article">`,
		"og:title from headline": `<meta property="og:title" content="Hello World">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing %q in body", name, want)
		}
	}
}

// An explicit ScreenSchema Article wins; the synthesized one is NOT added
// on top (no duplicate JSON-LD block).
type articleWithSchemaComp struct{}

func (*articleWithSchemaComp) Render() render.HTML { return render.Raw("<p>x</p>") }
func (*articleWithSchemaComp) ScreenArticle() app.ArticleMeta {
	return app.ArticleMeta{Headline: "From Meta"}
}
func (*articleWithSchemaComp) ScreenSchema() []seo.Thing {
	a := seo.NewArticle()
	a.Headline = "From Schema"
	return []seo.Thing{a}
}

func TestScreenArticleDoesNotDuplicateSchema(t *testing.T) {
	a := app.NewApp("dup")
	a.Register("/p", &articleWithSchemaComp{}, nil)
	ds := New(a)

	req := httptest.NewRequest("GET", "/p", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	if c := strings.Count(body, `"@type":"Article"`); c != 1 {
		t.Errorf("expected exactly 1 Article JSON-LD (explicit ScreenSchema wins), got %d", c)
	}
	if !strings.Contains(body, `"headline":"From Schema"`) {
		t.Errorf("expected the explicit Schema headline to win")
	}
	if strings.Contains(body, `"headline":"From Meta"`) {
		t.Errorf("synthesized Article should NOT duplicate the explicit one")
	}
}

// Explicit ScreenSEO OG values are preserved. The article synthesis only
// fills empty fields (Type stays "website", Title stays custom).
type articleWithOGComp struct{}

func (*articleWithOGComp) Render() render.HTML { return render.Raw("<p>x</p>") }
func (*articleWithOGComp) ScreenArticle() app.ArticleMeta {
	return app.ArticleMeta{Headline: "From Meta", Description: "From Meta Desc"}
}
func (*articleWithOGComp) ScreenSEO() SEO {
	return SEO{OG: &OG{Type: "website", Title: "Custom Title"}}
}

func TestScreenArticlePreservesExplicitOG(t *testing.T) {
	a := app.NewApp("og")
	a.Register("/p", &articleWithOGComp{}, nil)
	ds := New(a)

	req := httptest.NewRequest("GET", "/p", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	if !strings.Contains(body, `<meta property="og:type" content="website">`) {
		t.Errorf("explicit og:type=website must be preserved, got:\n%s", body)
	}
	if strings.Contains(body, `og:type" content="article"`) {
		t.Errorf("og:type must NOT be overridden to article")
	}
	if !strings.Contains(body, `<meta property="og:title" content="Custom Title">`) {
		t.Errorf("explicit og:title must be preserved, got:\n%s", body)
	}
}

// asArticlePlainComp is a NORMAL screen, just a title and a description,
// no article interface. Registered with app.AsArticle(), it becomes an
// article whose JSON-LD headline + og:title come from ScreenTitle and whose
// description comes from ScreenDescription. This path needs no
// article-specific data anywhere.
type asArticlePlainComp struct{}

func (*asArticlePlainComp) Render() render.HTML {
	return render.Raw("<h1>My Post</h1><p>The prose.</p>")
}
func (*asArticlePlainComp) ScreenTitle() string       { return "My Post" }
func (*asArticlePlainComp) ScreenDescription() string { return "A normal screen turned article." }

func TestAsArticleDerivesFromScreenTitle(t *testing.T) {
	a := app.NewApp("plain")
	a.Register("/p", &asArticlePlainComp{}, nil, app.AsArticle())
	ds := New(a)

	req := httptest.NewRequest("GET", "/p", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	body := w.Body.String()

	for name, want := range map[string]string{
		"<article> wrapper":       "<article>",
		"headline from title":     `"headline":"My Post"`,
		"description from screen": `"description":"A normal screen turned article."`,
		"og:type article":         `<meta property="og:type" content="article">`,
		"og:title from title":     `<meta property="og:title" content="My Post">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing %q in body", name, want)
		}
	}
}
