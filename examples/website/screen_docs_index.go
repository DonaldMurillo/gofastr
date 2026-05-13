package main

import (
	"context"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// DocsIndexScreen lists every Markdown doc as a card. Implements
// ScreenLoader so the catalog scan runs as part of SSG / SSR rather than
// at template-render time.
type DocsIndexScreen struct {
	items []docItem
	err   error
}

func (s *DocsIndexScreen) ScreenTitle() string        { return "Docs" }
func (s *DocsIndexScreen) ScreenDescription() string  { return "Documentation index for the GoFastr framework." }
func (s *DocsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DocsIndexScreen) Load(ctx context.Context) error {
	s.items, s.err = docs.all()
	return s.err
}

func (s *DocsIndexScreen) Render() render.HTML {
	if s.err != nil {
		return render.Tag("div", nil,
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("Docs")),
			render.Tag("p", nil, render.Text("Failed to load docs: "+s.err.Error())),
		)
	}

	cards := make([]render.HTML, 0, len(s.items))
	for _, item := range s.items {
		cards = append(cards, render.Tag("a", map[string]string{"href": "/docs/" + item.Slug + "/"},
			render.Tag("strong", nil, render.Text(item.Title)),
			render.Tag("span", nil, render.Text(item.Description)),
		))
	}

	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Documentation")),
		render.Tag("p", nil, render.Text(
			"Long-form docs covering each part of the framework. Generated from "+
				"the Markdown files in the project's docs/ directory.")),
		render.Tag("div", map[string]string{"class": "doc-list"}, cards...),
	)
}
