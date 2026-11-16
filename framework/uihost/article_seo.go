package uihost

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/seo"
)

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
