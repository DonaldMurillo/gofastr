package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type StepsScreen struct{}

func (s *StepsScreen) ScreenTitle() string { return "Progress Steps" }
func (s *StepsScreen) ScreenDescription() string {
	return "Step indicator for multi-step flows."
}
func (s *StepsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *StepsScreen) Render() render.HTML {
	horizontal := ui.ProgressSteps(ui.ProgressStepsConfig{
		Steps: []ui.ProgressStep{
			{Label: "Account", Hint: "Sign in", Status: ui.ProgressStepComplete, Href: "/signup/account"},
			{Label: "Profile", Hint: "Tell us about you", Status: ui.ProgressStepComplete, Href: "/signup/profile"},
			{Label: "Workspace", Hint: "Pick a team", Status: ui.ProgressStepCurrent},
			{Label: "Plan", Hint: "Free or Pro"},
			{Label: "Done"},
		},
	})

	vertical := ui.ProgressSteps(ui.ProgressStepsConfig{
		Orientation: ui.ProgressStepsVertical,
		Steps: []ui.ProgressStep{
			{Label: "Deploy ready", Status: ui.ProgressStepComplete},
			{Label: "Smoke tests", Status: ui.ProgressStepComplete},
			{Label: "Promoting to prod", Hint: "ETA ~2 min", Status: ui.ProgressStepCurrent},
			{Label: "Verification"},
			{Label: "Cleanup"},
		},
	})

	src := `ui.ProgressSteps(ui.ProgressStepsConfig{
    Steps: []ui.ProgressStep{
        {Label: "Account",   Hint: "Sign in",           Status: ui.ProgressStepComplete, Href: "/signup/account"},
        {Label: "Profile",   Hint: "Tell us about you", Status: ui.ProgressStepComplete, Href: "/signup/profile"},
        {Label: "Workspace", Hint: "Pick a team",       Status: ui.ProgressStepCurrent},
        {Label: "Plan",      Hint: "Free or Pro"},
        {Label: "Done"},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Progress Steps")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Linear step indicator. The current step gets aria-current=\"step\"; completed steps with Href become clickable links so users can navigate back without losing the trail.")),
		demoFrame(horizontal, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Vertical orientation")),
		render.Tag("p", nil, render.Text(
			"Vertical layout for tall narrow contexts — mobile, sidebars, deployment logs. Same semantics; only the layout changes.")),
		demoFrame(vertical, `ui.ProgressSteps(ui.ProgressStepsConfig{
    Orientation: ui.ProgressStepsVertical,
    Steps:       …,
})`),
	)
}
