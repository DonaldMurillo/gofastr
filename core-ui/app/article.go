package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ArticleMeta describes a screen's content as an article for a browser's
// built-in Reader Mode (Safari Reader, Firefox Reader View). It is optional
// enrichment — the headline and description fall back to the screen's own
// ScreenTitle / ScreenDescription, so a screen needs none of these fields to
// be reader-ready. Implement ScreenArticle only to add what the screen's
// title/description can't carry: a byline, a publication date, a cover image.
type ArticleMeta struct {
	// Headline is the article title. Overrides the screen's ScreenTitle
	// for the Article JSON-LD headline + og:title when set.
	Headline string
	// Author is the byline. Maps to the JSON-LD author (a Person), which
	// Safari Reader reads to show "By …" in its reader view.
	Author string
	// DatePublished is the publication time, RFC 3339 / ISO 8601
	// (e.g. "2026-08-01T09:00:00Z"). Maps to JSON-LD datePublished, which
	// Reader Mode reads for the article date.
	DatePublished string
	// DateModified is the last-updated time (RFC 3339). Optional.
	DateModified string
	// Description is a one-line summary. Overrides the screen's
	// ScreenDescription for JSON-LD description + og:description when set.
	Description string
	// Image is the cover/thumbnail URL. Maps to JSON-LD image + og:image.
	Image string
}

// ScreenArticle optionally enriches an article screen with metadata the
// screen's own title/description can't carry (byline, date, cover image). On
// its own it also marks the screen as an article. For the common case —
// turning a normal screen into a reader-ready article with no extra data —
// prefer the AsArticle registration option; implement this interface only to
// add the richer fields.
type ScreenArticle interface {
	ScreenArticle() ArticleMeta
}

// AsArticle is a ScreenOption that marks a screen's content as an article so
// a browser offers its built-in Reader Mode. It needs no article-specific
// data: the screen's existing ScreenTitle becomes the Article headline (and
// og:title), ScreenDescription becomes the description, and the content is
// wrapped in an <article> element. Add it to a screen's registration:
//
//	site.Register("/post", &PostScreen{}, layout, app.AsArticle())
//
// For a richer reader view (byline, date, cover image), also implement the
// ScreenArticle interface — its fields fill what AsArticle can't infer.
func AsArticle() ScreenOption {
	return func(s *Screen) { s.Article = true }
}

// isArticle reports whether the screen should be rendered as an article:
// the AsArticle option set the flag, or the component implements ScreenArticle.
func isArticle(screen *Screen, comp component.Component) bool {
	if screen != nil && screen.Article {
		return true
	}
	_, ok := comp.(ScreenArticle)
	return ok
}

// wrapArticle wraps content in an <article> element when the screen is an
// article — the semantic tag Safari Reader and Firefox Reader View key on to
// detect article content. Non-article screens pass through unchanged.
func wrapArticle(screen *Screen, comp component.Component, content render.HTML) render.HTML {
	if isArticle(screen, comp) {
		return html.Article(html.ArticleConfig{}, content)
	}
	return content
}
