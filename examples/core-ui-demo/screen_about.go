package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// AboutScreen is a static about page.
type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About" }
func (s *AboutScreen) ScreenDescription() string  { return "About GoFastr" }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return elements.Div(elements.DivConfig{},
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("About GoFastr")),
		elements.Section(
			elements.SectionConfig{Label: "Our mission"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Our Mission")),
			elements.Paragraph(elements.TextConfig{}, render.Text("GoFastr makes it easy to build fast, accessible web applications in pure Go.")),
		),
		elements.Section(
			elements.SectionConfig{Label: "Our team"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Our Team")),
			elements.UnorderedList(elements.ListConfig{},
				elements.ListItem(elements.ListItemConfig{}, render.Text("Alice — Founder & CEO")),
				elements.ListItem(elements.ListItemConfig{}, render.Text("Bob — Lead Engineer")),
				elements.ListItem(elements.ListItemConfig{}, render.Text("Carol — Design Lead")),
			),
		),
		elements.Section(
			elements.SectionConfig{Label: "Contact"},
			elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Contact")),
			elements.Paragraph(elements.TextConfig{}, render.Text("Reach us at hello@gofastr.dev")),
		),
	)
}
