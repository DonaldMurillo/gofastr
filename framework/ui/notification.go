package ui

import (
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
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

	ID    string
	Class string
}

// Notification renders the toast row.
func Notification(cfg NotificationConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: Notification requires Title")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	cls := "ui-notification ui-notification--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	role := "status"
	if v == StatusDanger || v == StatusWarning {
		role = "alert"
	}

	// elements.Div applies class/id; we attach role + aria-live via Attrs.
	attrs := elements.Attrs{
		"role":      role,
		"aria-live": "polite",
	}

	icon := elements.Span(elements.TextConfig{
		Class: "ui-notification__icon",
		Attrs: elements.Attrs{"aria-hidden": "true"},
	}, render.Text(notificationGlyph(v)))

	textChildren := []render.HTML{
		elements.Strong(elements.TextConfig{Class: "ui-notification__title"},
			render.Text(cfg.Title)),
	}
	if cfg.Body != "" {
		textChildren = append(textChildren,
			elements.Paragraph(elements.TextConfig{Class: "ui-notification__body"},
				render.Text(cfg.Body)))
	}
	textBlock := elements.Div(elements.DivConfig{Class: "ui-notification__text"}, textChildren...)

	children := []render.HTML{icon, textBlock}
	if cfg.DismissHref != "" {
		label := cfg.DismissLabel
		if label == "" {
			label = "Dismiss notification"
		}
		children = append(children, elements.LinkHTML(elements.LinkHTMLConfig{
			Href:    cfg.DismissHref,
			Class:   "ui-notification__dismiss",
			Attrs:   elements.Attrs{"aria-label": label},
			Content: render.Text("×"),
		}))
	}
	return elements.Div(elements.DivConfig{
		Class: cls, ID: cfg.ID,
		Attrs: attrs,
	}, children...)
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
