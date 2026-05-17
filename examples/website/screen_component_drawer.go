package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// DrawerScreen documents preset.Drawer.
type DrawerScreen struct{}

func (s *DrawerScreen) ScreenTitle() string        { return "Drawer" }
func (s *DrawerScreen) ScreenDescription() string  { return "Edge-mounted sliding panel with backdrop, focus trap, and URL deeplinking." }
func (s *DrawerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DrawerScreen) Render() render.HTML {
	open := render.Tag("button", map[string]string{
		"class":         "cta-button",
		"data-fui-open": "components-drawer",
	}, render.Text("Open drawer"))

	openDeep := render.Tag("button", map[string]string{
		"class":             "cta-button",
		"data-fui-open":     "components-filter-drawer",
		"data-fui-deeplink": "status=open&tag=urgent",
	}, render.Text("Filters: open + urgent"))

	src := `d := preset.Drawer("components-filter-drawer").
    Hidden().
    DeepLink("drawer", "filters").
    DeepLinkParam("status").
    DeepLinkParam("tag").
    Slot("body", &FilterForm{}).
    Build()
widget.Mount(r, &d)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Drawer")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A side-mounted sliding panel. Same dismiss affordances as Modal — backdrop, Escape, focus trap, scroll lock — plus URL deeplinking for filter / settings / detail views you want to share or bookmark.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic")),
		demoFrame(open, `<button data-fui-open="components-drawer">Open drawer</button>`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Deeplinked filters")),
		render.Tag("p", nil, render.Text(
			"Open the drawer below — the URL gains ?drawer=filters&status=open&tag=urgent. Bookmark it and reload: drawer reopens with the same filters seeded into its signals.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				openDeep,
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				render.Tag("pre", nil, render.Tag("code", nil, render.Text(src))),
			),
		),
	)
}
