package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SelectScreen struct{}

func (s *SelectScreen) ScreenTitle() string        { return "Select" }
func (s *SelectScreen) ScreenDescription() string  { return "Labelled native <select> dropdown with FormField integration." }
func (s *SelectScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SelectScreen) Render() render.HTML {
	basicDemo := ui.Select(ui.SelectConfig{
		Name:  "country",
		Label: "Country",
		Options: []ui.SelectOption{
			{Value: "us", Text: "United States"},
			{Value: "ca", Text: "Canada"},
			{Value: "uk", Text: "United Kingdom"},
			{Value: "de", Text: "Germany"},
			{Value: "fr", Text: "France"},
		},
	})
	basicSrc := `ui.Select(ui.SelectConfig{
    Name:  "country",
    Label: "Country",
    Options: []ui.SelectOption{
        {Value: "us", Text: "United States"},
        {Value: "ca", Text: "Canada"},
        // ...
    },
})`

	placeholderDemo := ui.Select(ui.SelectConfig{
		Name:        "plan",
		Label:       "Plan",
		Placeholder: "Choose a plan…",
		Required:    true,
		Help:        "Select the plan that fits your needs.",
		Options: []ui.SelectOption{
			{Value: "free", Text: "Free"},
			{Value: "pro", Text: "Pro — $9/mo"},
			{Value: "enterprise", Text: "Enterprise — Contact us"},
		},
	})
	placeholderSrc := `ui.Select(ui.SelectConfig{
    Name:        "plan",
    Label:       "Plan",
    Placeholder: "Choose a plan…",
    Required:    true,
    Help:        "Select the plan that fits your needs.",
    Options:     []ui.SelectOption{...},
})`

	errorDemo := ui.Select(ui.SelectConfig{
		Name:  "status",
		Label: "Status",
		Error: "Please select a status.",
		Options: []ui.SelectOption{
			{Value: "active", Text: "Active"},
			{Value: "inactive", Text: "Inactive"},
			{Value: "archived", Text: "Archived"},
		},
	})
	errorSrc := `ui.Select(ui.SelectConfig{
    Name:  "status",
    Label: "Status",
    Error: "Please select a status.",
    Options: []ui.SelectOption{...},
})`

	disabledDemo := ui.Select(ui.SelectConfig{
		Name:     "readonly-field",
		Label:    "Read-only",
		Disabled: true,
		Options: []ui.SelectOption{
			{Value: "locked", Text: "Cannot change this", Selected: true},
		},
	})
	disabledSrc := `ui.Select(ui.SelectConfig{
    Name:     "readonly-field",
    Label:    "Read-only",
    Disabled: true,
    Options:  []ui.SelectOption{...},
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Select")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Labelled native <select> dropdown with FormField-style label, help text, error state, required marker, and placeholder support. Wraps a native <select> so keyboard, screen reader, and form submission work without JavaScript.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic")),
		demoFrame(basicDemo, basicSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With placeholder & required")),
		demoFrame(placeholderDemo, placeholderSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Error state")),
		demoFrame(errorDemo, errorSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Disabled")),
		demoFrame(disabledDemo, disabledSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Features")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Custom chevron arrow via CSS background-image (no external icon dependency).")),
			render.Tag("li", nil, render.Text("Placeholder option (disabled, selected by default) for empty-state hint.")),
			render.Tag("li", nil, render.Text("Error state with aria-invalid + role=\"alert\" message.")),
			render.Tag("li", nil, render.Text("Required marker with aria-hidden asterisk.")),
			render.Tag("li", nil, render.Text("Focus-visible ring using theme primary color.")),
			render.Tag("li", nil, render.Text("Pure SSR — no JavaScript runtime module.")),
		),
	)
}
