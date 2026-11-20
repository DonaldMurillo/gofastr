package app

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ArticleMeta describes a screen's content as an article for a browser's built-in Reader Mode
// (Safari Reader, Firefox Reader View).
type ArticleMeta struct {
	// Headline is the article title.
	Headline string
	// Author is the byline.
	Author string
	// DatePublished is the publication time (RFC 3339).
	DatePublished string
	// DateModified is the last-updated time (RFC 3339).
	DateModified string
	// Description is a one-line summary.
	Description string
	// Image is the cover/thumbnail URL.
	Image string
}

// ScreenArticle optionally enriches an article screen with metadata the screen's own
// title/description can't carry (byline, date, cover image).
type ScreenArticle interface {
	ScreenArticle() ArticleMeta
}

// AsArticle is a ScreenOption that marks a screen's content as an article so a browser offers its
// built-in Reader Mode.
func AsArticle() ScreenOption {
	return func(s *Screen) { s.Article = true }
}

func isArticle(screen *Screen, comp component.Component) bool {
	if screen != nil && screen.Article {
		return true
	}
	_, ok := comp.(ScreenArticle)
	return ok
}

func wrapArticle(screen *Screen, comp component.Component, content render.HTML) render.HTML {
	var attrs html.Attrs
	if screen != nil && screen.ownStyle != nil {
		attrs = html.Attrs{"data-cui-scope": screen.ownStyle.Name()}
	}
	if isArticle(screen, comp) {
		return html.Article(html.ArticleConfig{ExtraAttrs: attrs}, content)
	}
	if attrs != nil {
		return html.Div(html.DivConfig{ExtraAttrs: attrs}, content)
	}
	return content
}
