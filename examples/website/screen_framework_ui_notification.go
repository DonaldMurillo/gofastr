package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

type NotificationDemoScreen struct{}

func (s *NotificationDemoScreen) ScreenTitle() string        { return "Notification" }
func (s *NotificationDemoScreen) ScreenDescription() string  { return "Styled toast row with variants and dismiss." }
func (s *NotificationDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NotificationDemoScreen) Render() render.HTML {
	stack := render.Tag("div", map[string]string{"style": "display:grid;gap:0.75rem;max-width:28rem"},
		ui.Notification(ui.NotificationConfig{
			Title:   "Saved",
			Body:    "Your changes were applied.",
			Variant: ui.StatusSuccess,
			DismissHref: "/notifications/dismiss/1",
		}),
		ui.Notification(ui.NotificationConfig{
			Title:   "Heads up",
			Body:    "Deploy ETA pushed to 4:30pm.",
			Variant: ui.StatusInfo,
			DismissHref: "/notifications/dismiss/2",
		}),
		ui.Notification(ui.NotificationConfig{
			Title:   "Long-running job",
			Body:    "Batch import is taking longer than expected.",
			Variant: ui.StatusWarning,
			DismissHref: "/notifications/dismiss/3",
		}),
		ui.Notification(ui.NotificationConfig{
			Title:   "Connection lost",
			Body:    "Reconnecting…",
			Variant: ui.StatusDanger,
		}),
		ui.Notification(ui.NotificationConfig{
			Title:   "Title only",
			Variant: ui.StatusNeutral,
		}),
	)

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui", Title: "Notification",
			Subtitle: "Styled content for ephemeral messages. Pair with core-ui/widget/preset.Toast for positioning, or render inline as a stack.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Variants",
			Description: "Five semantic variants; danger and warning auto-apply role=alert so screen readers announce them assertively.",
		}, stack),
		ui.Section(ui.SectionConfig{
			Heading: "Composition",
			Description: "Notification composes elements.Span + elements.Strong + elements.Paragraph + elements.LinkHTML — every visible element is a core-ui primitive. Variants drive the leading icon background and the accent rail color via framework/ui/theme tokens.",
		}),
	)
}
