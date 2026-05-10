package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// ComponentsIndexScreen lists every core-ui component package the website
// dogfoods. Each entry links to a live demo + explainer page.
type ComponentsIndexScreen struct{}

func (s *ComponentsIndexScreen) ScreenTitle() string        { return "Components" }
func (s *ComponentsIndexScreen) ScreenDescription() string  { return "Live, dogfooded core-ui components." }
func (s *ComponentsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

type componentEntry struct {
	Slug  string
	Name  string
	Tag   string
	Intro string
}

var componentEntries = []componentEntry{
	{
		Slug:  "accordion",
		Name:  "Accordion",
		Tag:   "Group · Stack",
		Intro: "Disclosure widgets built on native <details>/<summary>. Two variants: an exclusive group (single-open via the name= attribute) and an independent stack. Pure server-rendered, modern-CSS animation, zero JavaScript.",
	},
}

func (s *ComponentsIndexScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(componentEntries))
	for _, c := range componentEntries {
		cards = append(cards, elements.LinkHTML(elements.LinkHTMLConfig{
			Href: "/components/" + c.Slug,
			Content: render.Join(
				render.Tag("strong", nil, render.Text(c.Name)),
				render.Tag("em", map[string]string{"class": "component-tag"}, render.Text(c.Tag)),
				render.Tag("span", nil, render.Text(c.Intro)),
			),
		}))
	}
	return render.Tag("main", nil,
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Components")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Building blocks shipped in core-ui/. Every component on this site is rendered with itself — what you see is the dogfood.")),
		render.Tag("div", map[string]string{"class": "doc-list"}, cards...),
	)
}
