package main

import (
	"context"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// NotificationDemoScreen lets you see Notification in two modes:
//
//   - inline: stacked in document flow (caller positions them).
//   - floating: pinned to a screen corner via position:fixed.
//
// The "show toast" button is a real link that round-trips ?show=… so
// the toast appears on the next render. Clicking the × dismisses it
// by navigating to the same page without ?show. Pure server-driven —
// no JS — but feels like a real toast because the floating variant
// uses `position: fixed` + a CSS slide-in animation.
type NotificationDemoScreen struct {
	showToast string
}

func (s *NotificationDemoScreen) ScreenTitle() string        { return "Notification" }
func (s *NotificationDemoScreen) ScreenDescription() string  { return "Styled toast row with variants and dismiss." }
func (s *NotificationDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NotificationDemoScreen) Load(ctx context.Context) error {
	s.showToast = app.QueryFromContext(ctx).Get("show")
	return nil
}

func (s *NotificationDemoScreen) Render() render.HTML {
	stack := render.Tag("div", map[string]string{"class": "demo-stack demo-stack--toast"},
		ui.Notification(ui.NotificationConfig{
			Title:       "Saved",
			Body:        "Your changes were applied.",
			Variant:     ui.StatusSuccess,
			DismissHref: "/framework-ui/notification?dismissed=1",
		}),
		ui.Notification(ui.NotificationConfig{
			Title:       "Heads up",
			Body:        "Deploy ETA pushed to 4:30pm.",
			Variant:     ui.StatusInfo,
			DismissHref: "/framework-ui/notification?dismissed=2",
		}),
		ui.Notification(ui.NotificationConfig{
			Title:       "Long-running job",
			Body:        "Batch import is taking longer than expected.",
			Variant:     ui.StatusWarning,
			DismissHref: "/framework-ui/notification?dismissed=3",
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

	// Trigger row — four buttons each navigate to ?show=<variant>.
	triggers := render.Tag("div", map[string]string{"class": "demo-trigger-row"},
		linkButton("?show=success", "Show success toast"),
		linkButton("?show=info", "Show info toast"),
		linkButton("?show=warning", "Show warning toast"),
		linkButton("?show=danger", "Show danger toast"),
	)

	var floating render.HTML
	switch s.showToast {
	case "success":
		floating = ui.Notification(ui.NotificationConfig{
			Title: "Saved", Body: "Your changes are live.",
			Variant: ui.StatusSuccess, Position: ui.NotificationBottomRight,
			DismissHref: "/framework-ui/notification",
		})
	case "info":
		floating = ui.Notification(ui.NotificationConfig{
			Title: "Reminder", Body: "Standup starts in 5 minutes.",
			Variant: ui.StatusInfo, Position: ui.NotificationBottomRight,
			DismissHref: "/framework-ui/notification",
		})
	case "warning":
		floating = ui.Notification(ui.NotificationConfig{
			Title: "Almost out of credits", Body: "Top up to keep auto-deploys running.",
			Variant: ui.StatusWarning, Position: ui.NotificationBottomRight,
			DismissHref: "/framework-ui/notification",
		})
	case "danger":
		floating = ui.Notification(ui.NotificationConfig{
			Title: "Deploy failed", Body: "Build #482 returned a non-zero exit code.",
			Variant: ui.StatusDanger, Position: ui.NotificationBottomRight,
			DismissHref: "/framework-ui/notification",
		})
	}

	body := []render.HTML{
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui", Title: "Notification",
			Subtitle: "Styled content for ephemeral messages. Renders inline by default; set Position to pin it to a screen corner with position:fixed + a slide-in animation.",
		}),

		ui.Section(ui.SectionConfig{
			Heading:     "Try it — floating toast",
			Description: "Click any of the buttons below. A notification appears at the bottom-right corner with a slide-in animation. Click × on the toast (or any of the buttons again) to dismiss / replace it.",
		}, triggers),

		ui.Section(ui.SectionConfig{
			Heading:     "Inline variants",
			Description: "All five variants rendered as inline rows. Use these inside a list or a Toast widget for stacked notifications.",
		}, stack),

		ui.Section(ui.SectionConfig{
			Heading: "Composition",
			Description: "Notification composes html.Span + html.Strong + html.Paragraph + html.LinkHTML. Variants drive the leading icon background and accent rail color via framework/ui/theme tokens. Position adds .ui-notification--floating which sets position:fixed + animation.",
		}),
	}
	if floating != "" {
		body = append(body, floating)
	}
	return render.Tag("div", nil, body...)
}

func linkButton(href, label string) render.HTML {
	return html.Link(html.LinkConfig{
		Href: href, Text: label, Class: "ui-button",
	})
}
