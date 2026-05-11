package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// AboutScreen is a static about page.
type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About" }
func (s *AboutScreen) ScreenDescription() string  { return "About GoFastr" }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("About GoFastr")),
		html.Section(
			html.SectionConfig{Label: "Our mission"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Our Mission")),
			html.Paragraph(html.TextConfig{}, render.Text("GoFastr makes it easy to build fast, accessible web applications in pure Go.")),
		),
		html.Section(
			html.SectionConfig{Label: "Our team"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Our Team")),
			html.UnorderedList(html.ListConfig{},
				html.ListItem(html.ListItemConfig{}, render.Text("Alice — Founder & CEO")),
				html.ListItem(html.ListItemConfig{}, render.Text("Bob — Lead Engineer")),
				html.ListItem(html.ListItemConfig{}, render.Text("Carol — Design Lead")),
			),
		),
		html.Section(
			html.SectionConfig{Label: "Contact"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text("Contact")),
			html.Paragraph(html.TextConfig{}, render.Text("Reach us at hello@gofastr.dev")),
		),
	)
}
