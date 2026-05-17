package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// MenuScreen documents framework/ui.Menu.
type MenuScreen struct{}

func (s *MenuScreen) ScreenTitle() string        { return "Menu" }
func (s *MenuScreen) ScreenDescription() string  { return "Anchored dropdown menu with keyboard navigation, type-ahead, and ARIA roles." }
func (s *MenuScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *MenuScreen) Render() render.HTML {
	basic := ui.Menu(ui.MenuConfig{
		Label: "Actions",
		Items: []ui.MenuItem{
			{Label: "Edit", Href: "#"},
			{Label: "Duplicate", Href: "#"},
			{Separator: true},
			{Label: "Archive", Href: "#"},
			{Label: "Delete", Danger: true, Href: "#"},
		},
	})

	withIcons := ui.Menu(ui.MenuConfig{
		Label:    "Settings",
		Position: ui.MenuBottomEnd,
		Items: []ui.MenuItem{
			{Label: "Profile", Icon: render.Text("◐"), Href: "#"},
			{Label: "Team", Icon: render.Text("◑"), Href: "#"},
			{Label: "Billing", Icon: render.Text("◒"), Href: "#"},
			{Separator: true},
			{Label: "Sign out", Icon: render.Text("◓"), Danger: true, Href: "#"},
		},
	})

	src := `ui.Menu(ui.MenuConfig{
    Label: "Actions",
    Items: []ui.MenuItem{
        {Label: "Edit",       Href: "/users/42/edit"},
        {Label: "Duplicate",  Href: "/users/42/duplicate"},
        {Separator: true},
        {Label: "Archive",    Href: "/users/42/archive"},
        {Label: "Delete",     Danger: true,
                              RPC: "/users/42",
                              RPCMethod: "DELETE"},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Menu")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A dropdown menu built on the native <details> disclosure, augmented with role=menu / role=menuitem and runtime keyboard navigation. Click the trigger, then arrow keys / Home / End / type-ahead to move, Enter to activate, Tab to close + escape, Esc to close + return focus to the trigger.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic action menu")),
		demoFrame(basic, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With icons + end-aligned")),
		render.Tag("p", nil, render.Text(
			"Position = MenuBottomEnd anchors the panel to the trigger's inline-end edge — handy when the trigger sits flush with the right side of its container.")),
		demoFrame(withIcons, `ui.Menu(ui.MenuConfig{
    Label:    "Settings",
    Position: ui.MenuBottomEnd,
    Items:    []ui.MenuItem{
        {Label: "Profile",  Icon: profileIcon, Href: "/profile"},
        ...
    },
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Keyboard contract")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Tag("kbd", nil, render.Text("Enter")), render.Text(" / "),
				render.Tag("kbd", nil, render.Text("Space")), render.Text(" / "),
				render.Tag("kbd", nil, render.Text("↓")),
				render.Text(" on trigger: open + focus first menuitem.")),
			render.Tag("li", nil, render.Tag("kbd", nil, render.Text("↑")), render.Text(" / "),
				render.Tag("kbd", nil, render.Text("↓")),
				render.Text(": move focus, wrapping at the ends.")),
			render.Tag("li", nil, render.Tag("kbd", nil, render.Text("Home")), render.Text(" / "),
				render.Tag("kbd", nil, render.Text("End")),
				render.Text(": jump to first / last item.")),
			render.Tag("li", nil, render.Text("Any letter: type-ahead to the next item whose label starts with what you've typed (800 ms reset window).")),
			render.Tag("li", nil, render.Tag("kbd", nil, render.Text("Esc")),
				render.Text(": close + return focus to the trigger.")),
			render.Tag("li", nil, render.Tag("kbd", nil, render.Text("Tab")),
				render.Text(": close + let focus escape naturally (menus aren't modal).")),
		),
	)
}
