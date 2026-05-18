package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type NotificationBellScreen struct{}

func (s *NotificationBellScreen) ScreenTitle() string { return "Notification Bell" }
func (s *NotificationBellScreen) ScreenDescription() string {
	return "Bell trigger + popover dropdown of recent items."
}
func (s *NotificationBellScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NotificationBellScreen) Render() render.HTML {
	trigger, _ := ui.NotificationBell(ui.NotificationBellConfig{
		Name:        "components-bell-demo",
		Label:       "Notifications",
		UnreadCount: 3,
		Pages:       []string{"/components/notificationbell"},
		Items: []ui.NotificationItem{
			{Title: "Build #4821 passed", Body: "examples/website is green", Time: "2m ago", Unread: true, Href: "/builds/4821"},
			{Title: "New review on PR #92", Body: "“LGTM, just one nit on naming.”", Time: "8m ago", Unread: true, Href: "/pull/92"},
			{Title: "Sarah commented on Doc", Body: "Replied to your draft.", Time: "1h ago", Unread: true, Href: "/docs/architecture"},
			{Title: "Daily backups verified", Body: "All databases restored in staging.", Time: "Yesterday", Href: "/ops/backups"},
		},
	})

	empty, _ := ui.NotificationBell(ui.NotificationBellConfig{
		Name:  "components-bell-empty",
		Label: "Notifications (empty state)",
		Pages: []string{"/components/notificationbell"},
		Items: nil,
	})

	src := `trigger, pop := ui.NotificationBell(ui.NotificationBellConfig{
    Name:        "app-bell",
    Label:       "Notifications",
    UnreadCount: 3,
    Items: []ui.NotificationItem{
        {Title: "Build #4821 passed", Time: "2m ago", Unread: true, Href: "/builds/4821"},
        {Title: "New review on PR #92", Time: "8m ago", Unread: true, Href: "/pull/92"},
        // …
    },
})

// Mount the popover once at app startup, then render trigger in your header:
widget.Mount(r, pop.Build())`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Notification Bell")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Bell button + unread-count badge + popover dropdown of recent items. Composes preset.Popover. Optional SignalUnread + SignalList bind the badge text and the list HTML to runtime signals for live SSE-driven updates without page reloads.")),
		demoFrame(render.Tag("div", map[string]string{"class": "demo-row-flex"},
			trigger,
			html.Span(html.TextConfig{}, render.Text("← click the bell")),
		), src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Empty state")),
		demoFrame(empty, `ui.NotificationBell(ui.NotificationBellConfig{
    Name: "empty-bell", Label: "Notifications",
    Items: nil,
})`),
	)
}
