package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type SegmentedScreen struct{}

func (s *SegmentedScreen) ScreenTitle() string {
	return "Segmented Control"
}
func (s *SegmentedScreen) ScreenDescription() string {
	return "Radiogroup styled as a pill toggle bar with a sliding indicator."
}
func (s *SegmentedScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SegmentedScreen) Render() render.HTML {
	demo := ui.SegmentedControl(ui.SegmentedControlConfig{
		Name:     "view-mode",
		Label:    "View mode",
		Selected: "week",
		Options: []ui.SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
			{Label: "Month", Value: "month"},
		},
	})
	twoOpt := ui.SegmentedControl(ui.SegmentedControlConfig{
		Name:  "billing",
		Label: "Billing cycle",
		Options: []ui.SegmentedOption{
			{Label: "Monthly", Value: "monthly"},
			{Label: "Annual (-20%)", Value: "annual"},
		},
	})
	disabled := ui.SegmentedControl(ui.SegmentedControlConfig{
		Name:  "tier",
		Label: "Tier",
		Options: []ui.SegmentedOption{
			{Label: "Free", Value: "free"},
			{Label: "Pro", Value: "pro"},
			{Label: "Enterprise", Value: "ent", Disabled: true},
		},
	})
	src := `ui.SegmentedControl(ui.SegmentedControlConfig{
    Name:     "view-mode",
    Label:    "View mode",
    Selected: "week",
    Options: []ui.SegmentedOption{
        {Label: "Day",   Value: "day"},
        {Label: "Week",  Value: "week"},
        {Label: "Month", Value: "month"},
    },
})`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Segmented Control")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Native radio inputs styled as a pill toggle bar. Browser handles Tab + Arrow-key + Space/Enter navigation; CSS :has(input:checked) slides the indicator. Keyboard-accessible by construction, form-submittable, no JS for selection.")),
		demoFrame(demo, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Two options")),
		demoFrame(twoOpt, `ui.SegmentedControl(ui.SegmentedControlConfig{
    Name:  "billing",
    Label: "Billing cycle",
    Options: []ui.SegmentedOption{
        {Label: "Monthly", Value: "monthly"},
        {Label: "Annual (-20%)", Value: "annual"},
    },
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Disabled segment")),
		demoFrame(disabled, `Options: []ui.SegmentedOption{
    {Label: "Free", Value: "free"},
    {Label: "Pro",  Value: "pro"},
    {Label: "Enterprise", Value: "ent", Disabled: true},
}`),
	)
}
