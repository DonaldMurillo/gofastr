package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type RadioGroupScreen struct{}

func (s *RadioGroupScreen) ScreenTitle() string        { return "RadioGroup" }
func (s *RadioGroupScreen) ScreenDescription() string  { return "<fieldset> wrapper for grouped radio buttons." }
func (s *RadioGroupScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *RadioGroupScreen) Render() render.HTML {
	basicDemo := ui.RadioGroup(ui.RadioGroupConfig{
		Name:   "plan",
		Legend: "Choose a plan",
		Options: []ui.RadioGroupOption{
			{Value: "free", Label: "Free", Checked: true},
			{Value: "pro", Label: "Pro — $9/mo"},
			{Value: "enterprise", Label: "Enterprise — Contact us"},
		},
	})
	basicSrc := `ui.RadioGroup(ui.RadioGroupConfig{
    Name:   "plan",
    Legend: "Choose a plan",
    Options: []ui.RadioGroupOption{
        {Value: "free", Label: "Free", Checked: true},
        {Value: "pro",  Label: "Pro — $9/mo"},
        {Value: "enterprise", Label: "Enterprise"},
    },
})`

	helpDemo := ui.RadioGroup(ui.RadioGroupConfig{
		Name:   "notifications",
		Legend: "Email notifications",
		Help:   "Choose how often you want to receive email updates.",
		Options: []ui.RadioGroupOption{
			{Value: "all", Label: "All notifications", Checked: true},
			{Value: "important", Label: "Important only"},
			{Value: "none", Label: "None"},
		},
	})
	helpSrc := `ui.RadioGroup(ui.RadioGroupConfig{
    Name:   "notifications",
    Legend: "Email notifications",
    Help:   "Choose how often you want to receive email updates.",
    Options: []ui.RadioGroupOption{...},
})`

	errorDemo := ui.RadioGroup(ui.RadioGroupConfig{
		Name:   "tos",
		Legend: "Accept terms?",
		Required: true,
		Error:  "You must accept the terms to continue.",
		Options: []ui.RadioGroupOption{
			{Value: "yes", Label: "Yes, I accept"},
			{Value: "no", Label: "No, I decline"},
		},
	})
	errorSrc := `ui.RadioGroup(ui.RadioGroupConfig{
    Name:     "tos",
    Legend:   "Accept terms?",
    Required: true,
    Error:    "You must accept the terms to continue.",
    Options:  []ui.RadioGroupOption{...},
})`

	// Also show a CheckboxGroup
	cbDemo := ui.CheckboxGroup(ui.CheckboxGroupConfig{
		Name:   "contact",
		Legend: "Preferred contact methods",
		Help:   "Select all that apply.",
		Options: []ui.CheckboxGroupOption{
			{Value: "email", Label: "Email", Checked: true},
			{Value: "sms", Label: "SMS"},
			{Value: "push", Label: "Push notification"},
		},
	})
	cbSrc := `ui.CheckboxGroup(ui.CheckboxGroupConfig{
    Name:   "contact",
    Legend: "Preferred contact methods",
    Help:   "Select all that apply.",
    Options: []ui.CheckboxGroupOption{
        {Value: "email", Label: "Email", Checked: true},
        {Value: "sms",   Label: "SMS"},
        {Value: "push",  Label: "Push notification"},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("RadioGroup & CheckboxGroup")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"<fieldset> wrappers for grouped radio buttons and checkboxes with a shared legend, help text, and error state. Built on top of the existing Radio() and Checkbox() components — adds proper ARIA grouping and form-level validation wiring.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("RadioGroup — basic")),
		demoFrame(basicDemo, basicSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("RadioGroup — with help")),
		demoFrame(helpDemo, helpSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("RadioGroup — error state")),
		demoFrame(errorDemo, errorSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("CheckboxGroup")),
		render.Tag("p", nil, render.Text("Same pattern but for multi-select checkboxes. Name gets [] appended for form handling.")),
		demoFrame(cbDemo, cbSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("RadioGroup: role=\"radiogroup\" + aria-describedby for help/error.")),
			render.Tag("li", nil, render.Text("CheckboxGroup: role=\"group\" + aria-describedby.")),
			render.Tag("li", nil, render.Text("Both use <fieldset>/<legend> for native grouping semantics.")),
		),
	)
}
