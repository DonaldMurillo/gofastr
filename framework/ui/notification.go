package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Notification is the styled content for an ephemeral toast. Drop it
// inside a [core-ui/widget/preset.Toast] surface (or any container)
// to render a status pill with optional icon, title, body, and a
// dismiss link.
//
// Notification is intentionally stateless — auto-dismiss timing is the
// host's responsibility. The dismiss link can target a URL that the
// server uses to remove the notification from session state, then a
// signal-driven re-render swaps it out.
type NotificationConfig struct {
	// Title is the prominent first line. Required.
	Title string

	// Body is optional supporting text below the title.
	Body string

	// Variant colors the leading icon and accent rail. Defaults to Info.
	Variant StatusVariant

	// DismissHref optionally adds a × link with this href. Pair with a
	// server-side handler that removes the notification from session
	// state. Empty omits the dismiss control.
	DismissHref string

	// DismissLabel overrides the dismiss link's accessible label.
	// Defaults to "Dismiss notification".
	DismissLabel string

	// Position pins the notification to a screen corner via fixed
	// positioning. Defaults to NotificationInline (in document flow).
	Position NotificationPosition

	ID    string
	Class string
}

// NotificationPosition controls where a Notification renders.
type NotificationPosition string

const (
	// NotificationInline (default) renders in document flow. Hosts
	// position a stack themselves.
	NotificationInline NotificationPosition = ""

	NotificationTopRight    NotificationPosition = "top-right"
	NotificationTopLeft     NotificationPosition = "top-left"
	NotificationBottomRight NotificationPosition = "bottom-right"
	NotificationBottomLeft  NotificationPosition = "bottom-left"
)

// Notification renders the toast row.
func Notification(cfg NotificationConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: Notification requires Title")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	switch v {
	case StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral:
	default:
		panic("ui: Notification unknown Variant " + string(v) + " — pick one of: success, warning, danger, info, neutral")
	}
	cls := "ui-notification ui-notification--" + string(v)
	if cfg.Position != NotificationInline {
		cls += " ui-notification--floating ui-notification--at-" + string(cfg.Position)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Pair role with the matching aria-live politeness so SR
	// behavior matches the implied severity. role="alert" implies
	// assertive; "polite" would contradict it.
	role := "status"
	live := "polite"
	if v == StatusDanger || v == StatusWarning {
		role = "alert"
		live = "assertive"
	}
	attrs := html.Attrs{
		"role":      role,
		"aria-live": live,
	}

	icon := html.Span(html.TextConfig{
		Class: "ui-notification__icon",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	}, render.Text(notificationGlyph(v)))

	textChildren := []render.HTML{
		html.Strong(html.TextConfig{Class: "ui-notification__title"},
			render.Text(cfg.Title)),
	}
	if cfg.Body != "" {
		textChildren = append(textChildren,
			html.Paragraph(html.TextConfig{Class: "ui-notification__body"},
				render.Text(cfg.Body)))
	}
	textBlock := html.Div(html.DivConfig{Class: "ui-notification__text"}, textChildren...)

	children := []render.HTML{icon, textBlock}
	if cfg.DismissHref != "" {
		label := cfg.DismissLabel
		if label == "" {
			label = "Dismiss notification"
		}
		children = append(children, html.LinkHTML(html.LinkHTMLConfig{
			Href:    sanitizeHref(cfg.DismissHref),
			Class:   "ui-notification__dismiss",
			ExtraAttrs:   html.Attrs{"aria-label": label},
			Content: render.Text("×"),
		}))
	}
	return notificationStyle.WrapHTML(html.Div(html.DivConfig{
		Class: cls, ID: cfg.ID,
		ExtraAttrs: attrs,
	}, children...))
}

func notificationGlyph(v StatusVariant) string {
	switch v {
	case StatusSuccess:
		return "✓"
	case StatusWarning:
		return "!"
	case StatusDanger:
		return "✕"
	case StatusInfo:
		return "i"
	default:
		return "•"
	}
}
