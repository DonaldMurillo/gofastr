package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type AvatarGroupScreen struct{}

func (s *AvatarGroupScreen) ScreenTitle() string {
	return "Avatar Group"
}
func (s *AvatarGroupScreen) ScreenDescription() string {
	return "Overlapping avatar stack with +N overflow indicator."
}
func (s *AvatarGroupScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AvatarGroupScreen) Render() render.HTML {
	team := []ui.AvatarConfig{
		{Name: "Ada Lovelace"},
		{Name: "Grace Hopper"},
		{Name: "Alan Turing"},
		{Name: "Edsger Dijkstra"},
		{Name: "Margaret Hamilton"},
		{Name: "Linus Torvalds"},
	}

	basic := ui.AvatarGroup(ui.AvatarGroupConfig{
		ID:      "avatars-demo",
		Label:   "Project team",
		Max:     4,
		Avatars: team,
	})

	withTooltips := ui.AvatarGroup(ui.AvatarGroupConfig{
		ID:        "avatars-tooltips",
		Label:     "Project team",
		Max:       4,
		Avatars:   team,
		ShowNames: true,
	})

	// Avatar + popover trigger: clicking the "View team" button opens
	// an anchored Popover (preset registered in new_components_demos.go).
	popoverDemo := render.Tag("div", map[string]string{"class": "demo-team-trigger"},
		ui.AvatarGroup(ui.AvatarGroupConfig{
			Label:     "Team",
			Max:       3,
			Avatars:   team,
			ShowNames: true,
		}),
		ui.Button(ui.ButtonConfig{
			Label:   "View team",
			Variant: ui.ButtonGhost,
			Attrs: html.Attrs{
				"data-fui-open":           "demo-team-popover",
				"data-fui-popover-anchor": "bottom",
			},
		}),
	)

	sizes := render.Tag("div", map[string]string{"class": "demo-avatar-sizes"},
		ui.AvatarGroup(ui.AvatarGroupConfig{Label: "Small team", Size: ui.AvatarSm, Max: 3, Avatars: team}),
		ui.AvatarGroup(ui.AvatarGroupConfig{Label: "Medium team", Max: 3, Avatars: team}),
		ui.AvatarGroup(ui.AvatarGroupConfig{Label: "Large team", Size: ui.AvatarLg, Max: 3, Avatars: team}),
		ui.AvatarGroup(ui.AvatarGroupConfig{Label: "XL team", Size: ui.AvatarXl, Max: 3, Avatars: team}),
	)

	srcBasic := `ui.AvatarGroup(ui.AvatarGroupConfig{
    Label: "Project team",
    Max:   4,
    Avatars: []ui.AvatarConfig{
        {Name: "Ada Lovelace"}, {Name: "Grace Hopper"},
        {Name: "Alan Turing"},  {Name: "Edsger Dijkstra"},
        {Name: "Margaret Hamilton"}, {Name: "Linus Torvalds"},
    },
})`

	srcTooltips := `ui.AvatarGroup(ui.AvatarGroupConfig{
    Label:     "Project team",
    Max:       4,
    Avatars:   team,
    ShowNames: true, // each avatar wrapped in a Tooltip
})`

	srcPopover := `// Mount once at app start (scoped to this page).
preset.Popover("team-popover").
    Pages("/components/avatargroup").
    LabelledBy("team-popover-title").
    Slot("body", teamPopoverBody{}).
    Build()

// Trigger anywhere in your page.
<button data-fui-open="team-popover"
        data-fui-popover-anchor="bottom">View team</button>`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Avatar Group")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Overlapping stack of avatars with a \"+N\" overflow indicator. role=group + aria-label so screen readers announce the cluster; per-variant overlap keeps the stack tight at every Size.")),
		demoFrame(basic, srcBasic),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With name tooltips")),
		render.Tag("p", nil, render.Text(
			"Hover or keyboard-focus any avatar to reveal the person's name via the framework's Tooltip (CSS-only, aria-describedby wired). The SR-only name is still in the DOM for screen-reader users.")),
		demoFrame(withTooltips, srcTooltips),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With a roster Popover")),
		render.Tag("p", nil, render.Text(
			"Tooltips cover hover-reveal of single names; a Popover gives the user click-to-open profile detail (full names, roles, links). The button below opens a preset.Popover anchored to its trigger — Esc / click-outside dismiss it, focus returns to the trigger.")),
		demoFrame(popoverDemo, srcPopover),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sizes")),
		render.Tag("p", nil, render.Text(
			"Size propagates to children unless a child Avatar overrides it. Overlap is scaled per-size so the stack stays visually tight from sm to xl.")),
		demoFrame(sizes, `ui.AvatarGroup(ui.AvatarGroupConfig{Size: ui.AvatarSm, Avatars: team})
ui.AvatarGroup(ui.AvatarGroupConfig{               Avatars: team})  // md (default)
ui.AvatarGroup(ui.AvatarGroupConfig{Size: ui.AvatarLg, Avatars: team})
ui.AvatarGroup(ui.AvatarGroupConfig{Size: ui.AvatarXl, Avatars: team})`),
	)
}
