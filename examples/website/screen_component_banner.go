package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type BannerScreen struct{}

func (s *BannerScreen) ScreenTitle() string { return "Banner" }
func (s *BannerScreen) ScreenDescription() string {
	return "Persistent in-page status strip — info, warn, danger, success."
}
func (s *BannerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *BannerScreen) Render() render.HTML {
	info := ui.Banner(ui.BannerConfig{
		Title:   "Scheduled maintenance",
		Body:    "We'll be performing read-only database upgrades Sunday 02:00 UTC. Writes will be rejected for ~10 minutes.",
		Variant: ui.BannerInfo,
	})

	success := ui.Banner(ui.BannerConfig{
		Title:   "Backups verified",
		Body:    "Today's snapshot restored cleanly in the staging environment.",
		Variant: ui.BannerSuccess,
	})

	warn := ui.Banner(ui.BannerConfig{
		Title:   "Action required",
		Body:    "Your payment method expires in 4 days. Update it before billing on the 23rd.",
		Variant: ui.BannerWarn,
		Action: ui.Link(ui.LinkConfig{
			Href:    "/billing",
			Text:    "Update card",
			Variant: ui.LinkAction,
		}),
	})

	danger := ui.Banner(ui.BannerConfig{
		Title:       "Service degraded",
		Body:        "Image processing is currently failing for ~3% of uploads. The on-call engineer is investigating.",
		Variant:     ui.BannerDanger,
		Dismissible: true,
	})

	persistent := ui.Banner(ui.BannerConfig{
		Title:       "New: Filter chips",
		Body:        "Try the new in-table filter bar — chips remove themselves on click.",
		Variant:     ui.BannerInfo,
		Dismissible: true,
		DismissID:   "feature-filter-chips-2026-05",
	})

	src := `// Info banner
ui.Banner(ui.BannerConfig{
    Title:   "Scheduled maintenance",
    Body:    "We'll be performing …",
    Variant: ui.BannerInfo,
})

// Warn with inline action
ui.Banner(ui.BannerConfig{
    Title:   "Action required",
    Body:    "Your payment method …",
    Variant: ui.BannerWarn,
    Action:  ui.Link(ui.LinkConfig{
        Href: "/billing", Text: "Update card",
        Variant: ui.LinkAction,
    }),
})

// Dismissible with localStorage persistence
ui.Banner(ui.BannerConfig{
    Title:       "New: Filter chips",
    Variant:     ui.BannerInfo,
    Dismissible: true,
    DismissID:   "feature-filter-chips-2026-05",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Banner")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Page-level persistent status strip. Distinct from Toast (transient) and Notification (record-bound). Warn / Danger emit role=\"alert\"; Info / Success emit role=\"status\" with aria-live=\"polite\" so screen-reader announcement matches severity.")),
		demoFrame(render.Tag("div", map[string]string{"class": "demo-stack"}, info, success, warn, danger), src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Dismiss persistence")),
		render.Tag("p", nil, render.Text(
			"Setting DismissID records the dismissal in localStorage; the same banner stays hidden across reloads until you clear gofastr.banner-dismiss.<id>. Dismiss the banner below, then reload — it stays gone.")),
		demoFrame(persistent, `ui.Banner(ui.BannerConfig{
    Title:       "New: Filter chips",
    Variant:     ui.BannerInfo,
    Dismissible: true,
    DismissID:   "feature-filter-chips-2026-05",
})`),
	)
}
