package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/markdown"
	"github.com/gofastr/gofastr/core/render"
)

// DocsPageScreen is the dynamic /docs/:slug screen. It pulls slug out of
// route params, reads the matching .md file in Load(ctx), parses with
// core/markdown, and renders the resulting HTML inline. StaticPaths
// enumerates every doc so SSG generates one HTML file per .md.
type DocsPageScreen struct {
	slug    string
	title   string
	body    render.HTML
	err     error
}

func (s *DocsPageScreen) SetParams(params map[string]string) {
	s.slug = params["slug"]
	s.title = ""
	s.body = ""
	s.err = nil
}

func (s *DocsPageScreen) ScreenTitle() string {
	if s.title != "" {
		return s.title
	}
	if s.slug != "" {
		return humanise(s.slug)
	}
	return "Doc"
}
func (s *DocsPageScreen) ScreenDescription() string  { return "" }
func (s *DocsPageScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DocsPageScreen) Load(ctx context.Context) error {
	if s.slug == "" {
		s.err = fmt.Errorf("docs: slug is empty")
		return s.err
	}
	item, err := docs.find(s.slug)
	if err != nil {
		s.err = err
		return err
	}
	data, err := os.ReadFile(item.Path)
	if err != nil {
		s.err = err
		return err
	}
	doc := markdown.Render(string(data))
	s.title = doc.Title
	if s.title == "" {
		s.title = item.Title
	}
	s.body = doc.HTML
	return nil
}

// StaticPaths enumerates every doc so SSG produces one HTML file per .md.
func (s *DocsPageScreen) StaticPaths(ctx context.Context) []map[string]string {
	items, err := docs.all()
	if err != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]string{"slug": item.Slug})
	}
	return out
}

func (s *DocsPageScreen) Render() render.HTML {
	if s.err != nil {
		return render.Tag("div", nil,
			html.Heading(html.HeadingConfig{Level: 1}, render.Text("Doc not found")),
			render.Tag("p", nil, render.Text(s.err.Error())),
			html.Link(html.LinkConfig{Href: "/docs/", Text: "← Back to docs", Class: "doc-back"}),
		)
	}
	return render.Tag("div", map[string]string{"class": "doc-body"},
		html.Link(html.LinkConfig{Href: "/docs/", Text: "← Back to docs", Class: "doc-back"}),
		s.body,
	)
}
