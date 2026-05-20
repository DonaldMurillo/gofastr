package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/nestedlist"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type NestedListScreen struct{}

func (s *NestedListScreen) ScreenTitle() string {
	return "NestedList"
}
func (s *NestedListScreen) ScreenDescription() string {
	return "Recursive ul/ol with optional native <details> collapse on branches."
}
func (s *NestedListScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NestedListScreen) Render() render.HTML {
	flat := nestedlist.Render(nestedlist.Config{
		AriaLabel: "Quick links",
		Items: []nestedlist.Item{
			{Label: "Docs", Href: "/docs/"},
			{Label: "Examples", Href: "/examples/"},
			{Label: "About", Href: "/about"},
		},
	})
	srcFlat := `nestedlist.Render(nestedlist.Config{
    AriaLabel: "Quick links",
    Items: []nestedlist.Item{
        {Label: "Docs", Href: "/docs/"},
        {Label: "Examples", Href: "/examples/"},
        {Label: "About", Href: "/about"},
    },
})`

	nested := nestedlist.Render(nestedlist.Config{
		AriaLabel: "Settings",
		Items: []nestedlist.Item{
			{Label: "Account", Expanded: true, Children: []nestedlist.Item{
				{Label: "Profile", Href: "/settings/profile"},
				{Label: "Security", Href: "/settings/security"},
			}},
			{Label: "Notifications", Children: []nestedlist.Item{
				{Label: "Email", Href: "/settings/email"},
				{Label: "Push", Href: "/settings/push"},
			}},
			{Label: "Billing", Href: "/settings/billing"},
		},
	})
	srcNested := `nestedlist.Render(nestedlist.Config{
    AriaLabel: "Settings",
    Items: []nestedlist.Item{
        {Label: "Account", Expanded: true, Children: []nestedlist.Item{
            {Label: "Profile", Href: "/settings/profile"},
            {Label: "Security", Href: "/settings/security"},
        }},
        {Label: "Notifications", Children: []nestedlist.Item{
            {Label: "Email", Href: "/settings/email"},
            {Label: "Push", Href: "/settings/push"},
        }},
        {Label: "Billing", Href: "/settings/billing"},
    },
})`

	ordered := nestedlist.Render(nestedlist.Config{
		Ordered: true,
		Items: []nestedlist.Item{
			{Label: "Sign up"},
			{Label: "Verify email"},
			{Label: "Configure workspace"},
		},
	})
	srcOrdered := `nestedlist.Render(nestedlist.Config{
    Ordered: true,
    Items: []nestedlist.Item{
        {Label: "Sign up"},
        {Label: "Verify email"},
        {Label: "Configure workspace"},
    },
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("NestedList")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Recursive <ul>/<ol> with optional native <details>/<summary> collapse on branches. Lighter than the tree pattern (no lazy-load, no RPC). Pure render — no runtime module.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Flat (leaf links)")),
		demoFrame(flat, srcFlat),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Nested with collapse")),
		render.Tag("p", nil, render.Text(
			"Branches use native <details>. Set Expanded: true to render the branch open on first paint.")),
		demoFrame(nested, srcNested),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Ordered (numbered)")),
		demoFrame(ordered, srcOrdered),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Accessibility")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Set AriaLabel for nav-style usage (sidebar, settings menu).")),
			render.Tag("li", nil, render.Text("Branch summaries are non-navigable — disclosure trigger only.")),
			render.Tag("li", nil, render.Text("Native <details> = keyboard accessible (Enter/Space toggle).")),
		),
	)
}
