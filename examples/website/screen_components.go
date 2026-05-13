package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
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
	{
		Slug:  "tabs",
		Name:  "Tabs",
		Tag:   "Tab strip",
		Intro: "Tabbed-content layout built from native <details> elements arranged with CSS Grid. Zero JavaScript, native keyboard accessibility, full mutual exclusivity via the name= attribute.",
	},
	{
		Slug:  "progress",
		Name:  "Progress",
		Tag:   "Determinate · Indeterminate",
		Intro: "Native <progress> wrapped with theme-aware styling. Determinate when Value is set, animated indeterminate when Value < 0. Drive live updates via signal binding.",
	},
	{
		Slug:  "skeleton",
		Name:  "Skeleton",
		Tag:   "Line · Block · Circle",
		Intro: "Pure-CSS shimmer placeholders for loading states. Three variants cover paragraphs, blocks, and avatars. Aria-hidden so screen readers announce the parent's loading state once, not every shimmer.",
	},
	{
		Slug:  "breadcrumbs",
		Name:  "Breadcrumbs",
		Tag:   "Trail nav",
		Intro: "Ordered-list trail with aria-current=\"page\" on the leaf. CSS-driven slash separators (no DOM noise). One <nav aria-label=\"Breadcrumb\"> wrapper.",
	},
	{
		Slug:  "pagination",
		Name:  "Pagination",
		Tag:   "Numeric nav",
		Intro: "Numeric page navigation with first/last anchors and ellipses for gaps. ARIA-correct (<nav aria-label=\"Pagination\">, aria-current=\"page\"), prev/next disabled at boundaries.",
	},
}

func (s *ComponentsIndexScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(componentEntries))
	for _, c := range componentEntries {
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href: "/components/" + c.Slug,
			Content: render.Join(
				html.Strong(html.TextConfig{}, render.Text(c.Name)),
				html.Span(html.TextConfig{Class: "component-tag"}, render.Text(c.Tag)),
				html.Span(html.TextConfig{}, render.Text(c.Intro)),
			),
		}))
	}
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "core-ui",
			Title:   "Components",
			Subtitle: "Building blocks shipped in core-ui/. Every component on this site is rendered with itself — what you see is the dogfood.",
		}),
		ui.Section(ui.SectionConfig{
			Heading: "Available components",
		}, render.Tag("div", map[string]string{"class": "doc-list"}, cards...)),
	)
}
