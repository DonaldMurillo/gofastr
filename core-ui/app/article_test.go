package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// articleTestComp renders a body and declares itself an article. The
// framework must wrap its content in <article> (inside the layout's
// <main>) so browsers detect it for Reader Mode.
type articleTestComp struct {
	body string
}

func (a *articleTestComp) Render() render.HTML { return render.Raw(a.body) }
func (a *articleTestComp) ScreenArticle() ArticleMeta {
	return ArticleMeta{Headline: "Hello", Author: "Ada", DatePublished: "2026-08-01T00:00:00Z"}
}

// A ScreenArticle screen renders its content inside an <article> element,
// nested within the layout's <main> landmark — the structure Safari Reader
// and Firefox Reader View detect as an article.
func TestScreenArticleWrapsContentInArticle(t *testing.T) {
	a := NewApp("ArticleApp")
	a.SetDefaultLayout(NewLayout("main"))
	a.RegisterScreen(NewScreen("/post", &articleTestComp{body: "<p>Body</p>"}), nil)

	out, err := a.RenderPage(context.Background(), "/post")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	s := string(out)

	mainIdx := strings.Index(s, "<main")
	artIdx := strings.Index(s, "<article>")
	if mainIdx == -1 {
		t.Fatalf("expected <main> from layout, got:\n%s", s)
	}
	if artIdx == -1 {
		t.Fatalf("expected <article> wrapper for ScreenArticle screen, got:\n%s", s)
	}
	if artIdx < mainIdx {
		t.Fatalf("expected <article> nested inside <main>, got:\n%s", s)
	}
	// The content sits inside the article.
	bodyIdx := strings.Index(s, "<p>Body</p>")
	if bodyIdx == -1 || bodyIdx < artIdx {
		t.Fatalf("expected body inside <article>, got:\n%s", s)
	}
}

// A plain screen (no ScreenArticle) is NOT wrapped in <article> — the
// feature is opt-in.
func TestNonArticleScreenHasNoArticleWrapper(t *testing.T) {
	a := NewApp("PlainApp")
	a.SetDefaultLayout(NewLayout("main"))
	a.RegisterScreen(NewScreen("/page", &stubComponent{html: render.Raw("<p>Plain</p>")}), nil)

	out, err := a.RenderPage(context.Background(), "/page")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if strings.Contains(string(out), "<article>") {
		t.Fatalf("non-article screen must NOT be wrapped in <article>, got:\n%s", out)
	}
}

// A ScreenArticle screen with no layout still gets <article> inside the
// screen's <main> (renderComponentAs path), not wrapping it from outside.
func TestScreenArticleNoLayoutStillWrapsInsideMain(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/bare", &articleTestComp{body: "<p>Bare</p>"}), nil)
	out, err := r.RenderRaw("/bare")
	if err != nil {
		t.Fatalf("RenderRaw: %v", err)
	}
	s := string(out)
	mainIdx := strings.Index(s, "<main")
	artIdx := strings.Index(s, "<article>")
	if mainIdx == -1 || artIdx == -1 {
		t.Fatalf("expected <main> and <article>, got:\n%s", s)
	}
	// <article> must follow <main> (be inside it), not the reverse.
	if artIdx < mainIdx {
		t.Fatalf("expected <article> inside <main> on the no-layout path, got:\n%s", s)
	}
}

// AsArticle() turns a plain screen into an article at registration — no
// interface, no metadata. The framework wraps its content in <article>.
func TestAsArticleOptionWrapsContent(t *testing.T) {
	a := NewApp("AsArticleApp")
	a.SetDefaultLayout(NewLayout("main"))
	a.Register("/post", &stubComponent{html: render.Raw("<p>Body</p>")}, nil, AsArticle())

	out, err := a.RenderPage(context.Background(), "/post")
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	s := string(out)
	mainIdx := strings.Index(s, "<main")
	artIdx := strings.Index(s, "<article>")
	if artIdx == -1 {
		t.Fatalf("AsArticle screen must be wrapped in <article>, got:\n%s", s)
	}
	if mainIdx == -1 || artIdx < mainIdx {
		t.Fatalf("expected <article> nested inside <main>, got:\n%s", s)
	}
}
