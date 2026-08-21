package uihost

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
)

// ensureArticleSchema appends an Article JSON-LD item derived from meta
// when the resolved schema doesn't already include one. An explicit
// ScreenSchema declaration (which may carry richer publisher/author data)
// wins. Only the gap is filled. The Article carries headline/author/date,
// which Safari Reader reads to populate its reader-view title, byline, and
// date.
func ensureArticleSchema(schema []seo.Thing, meta app.ArticleMeta) []seo.Thing {
	for _, s := range schema {
		if _, ok := s.(seo.Article); ok {
			return schema
		}
	}
	a := seo.NewArticle()
	a.Headline = meta.Headline
	a.Description = meta.Description
	a.Image = meta.Image
	a.DatePublished = meta.DatePublished
	a.DateModified = meta.DateModified
	if meta.Author != "" {
		p := seo.NewPerson()
		p.Name = meta.Author
		a.Author = &p
	}
	return append(schema, a)
}

// mergeArticleOG fills og:type=article and any empty OG field from the
// article metadata. A nil og (the screen declared no OG) becomes a minimal
// article OG. Values the screen declared explicitly through ScreenSEO are
// preserved. Only empty fields inherit from the article meta.
func mergeArticleOG(og *OG, meta app.ArticleMeta) *OG {
	if og == nil {
		og = &OG{}
	}
	if og.Type == "" {
		og.Type = "article"
	}
	if og.Title == "" && meta.Headline != "" {
		og.Title = meta.Headline
	}
	if og.Description == "" && meta.Description != "" {
		og.Description = meta.Description
	}
	if og.Image == "" && meta.Image != "" {
		og.Image = meta.Image
	}
	return og
}
