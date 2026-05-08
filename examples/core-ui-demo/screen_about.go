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
	return elements.Div(nil,
		elements.Heading(1, nil, render.Text("About GoFastr")),
		elements.Section(
			elements.Aria("label", "Our mission"),
			elements.Heading(2, nil, render.Text("Our Mission")),
			elements.Paragraph(nil, render.Text("GoFastr makes it easy to build fast, accessible web applications in pure Go.")),
		),
		elements.Section(
			elements.Aria("label", "Our team"),
			elements.Heading(2, nil, render.Text("Our Team")),
			elements.UnorderedList(nil,
				elements.ListItem(nil, render.Text("Alice — Founder & CEO")),
				elements.ListItem(nil, render.Text("Bob — Lead Engineer")),
				elements.ListItem(nil, render.Text("Carol — Design Lead")),
			),
		),
		elements.Section(
			elements.Aria("label", "Contact"),
			elements.Heading(2, nil, render.Text("Contact")),
			elements.Paragraph(nil, render.Text("Reach us at hello@gofastr.dev")),
		),
	)
}
