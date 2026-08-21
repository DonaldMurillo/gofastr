package main

// =============================================================================
// /reader: a reader-ready article, the simple way. This is a NORMAL
// screen: it has a title, a description, and renders prose, nothing
// article-specific. The one difference from any other screen is the
// app.AsArticle() option on its registration (see main.go):
//
//	site.Register("/reader", &ArticleScreen{}, nil, app.AsArticle())
//
// That single option makes the framework wrap the content in <article> and
// emit Article JSON-LD + og:type=article, deriving the headline from the
// screen's own ScreenTitle. Load /reader in Safari or Firefox and the
// browser's Reader icon lights up in the address bar.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ArticleScreen is an ordinary content screen, the same shape as /about or
// /philosophy. app.AsArticle() on its registration is the entire reader-mode
// switch.
type ArticleScreen struct{}

func (s *ArticleScreen) ScreenTitle() string { return "Reader-ready pages" }
func (s *ArticleScreen) ScreenDescription() string {
	return "How GoFastr turns a normal screen into an article a browser's Reader Mode detects."
}

func (s *ArticleScreen) Render() render.HTML {
	// The article body. A single <h1> matching the title, then prose. The
	// framework supplies the surrounding <article> wrapper; this method
	// returns only what goes inside it, exactly what any screen returns.
	return render.Join(
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Reader-ready pages")),
		html.Paragraph(html.TextConfig{Class: "article-byline"},
			render.Text("By Donald Murillo · "),
			html.Time(html.TimeConfig{Datetime: "2026-08-01"}, render.Text("August 1, 2026")),
		),
		html.Paragraph(html.TextConfig{},
			render.Text("Safari, Firefox, and Edge each ship a built-in Reader Mode: a button in the address bar that strips a page to its article and re-renders it in a clean, distraction-free view. The button only appears when the browser is confident the page "),
			html.Em(html.TextConfig{}, render.Text("is")),
			render.Text(" an article. In GoFastr that confidence comes from a single option on the screen's registration. No article-specific code."),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("One option, no metadata")),
		html.Paragraph(html.TextConfig{},
			render.Text("This page is a normal screen: it has a title and a description and renders prose, exactly like any other. The only addition is "),
			codeText("app.AsArticle()"),
			render.Text(" on its registration. That makes the framework wrap the content in an "),
			codeText("<article>"),
			render.Text(" element (the tag Safari Reader and Firefox's Readability engine scan for), synthesize an "),
			codeText("Article"),
			render.Text(" JSON-LD block (which Safari Reader reads for its title and date), and set "),
			codeText("og:type=article"),
			render.Text(". The headline and description come from the screen's own title and description. Nothing is duplicated."),
		),
		ui.Markdown(ui.MarkdownConfig{Source: articleDemoSource}),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("When you want a byline or a date in JSON-LD")),
		html.Paragraph(html.TextConfig{},
			render.Text("This page shows its byline in the visible prose, which Reader Mode picks up on its own. If you'd rather give the browser structured metadata, so every reader view shows a consistent byline and publication date whatever the prose, implement the optional "),
			codeText("ScreenArticle"),
			render.Text(" interface alongside "),
			codeText("AsArticle"),
			render.Text(". Its fields (author, date, cover image) fill what the screen's title can't carry. But for the common case you just read, the one option is the whole feature."),
		),
	)
}

// articleDemoSource is the code sample shown on the page. Built with string
// concatenation so the fenced Go block can span multiple lines cleanly.
var articleDemoSource = "```go\n" +
	"// Any normal screen + one option = reader-ready.\n" +
	"site.Register(\"/posts/:slug\", &PostScreen{}, layout, app.AsArticle())\n" +
	"\n" +
	"// PostScreen is an ordinary screen: title + prose, nothing more.\n" +
	"type PostScreen struct{ post Post }\n" +
	"func (s *PostScreen) ScreenTitle() string { return s.post.Title }\n" +
	"func (s *PostScreen) Render() render.HTML { /* the post body */ }\n" +
	"\n```"
