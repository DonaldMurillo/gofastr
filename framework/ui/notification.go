package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Notification is the styled content for an ephemeral toast. Drop it
// inside a [core-ui/widget/preset.Toast] surface (or any container)
// to render a status pill with optional icon, title, body, and a
// dismiss link.
//
// Notification is intentionally stateless: auto-dismiss timing is the
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

	// Island is where the dismiss goes with script: a dismissing
	// toast changes the stack, which is an in-page state change and
	// needs the endpoint that renders the region again. Required by
	// the primitive when DismissHref is set; without script the same
	// link navigates to DismissHref.
	Island headless.Island

	// Ctx carries the per-request context used to resolve the
	// dismiss-label string. When nil, English fallbacks apply.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the notification's root
	// element. Keys the component owns are dropped: class and id
	// (use Class / ID), data-fui-*, role, and aria-live.
	ExtraAttrs html.Attrs
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
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	checkStatusVariant("Notification", v)

	// The toast's tone vocabulary is closed; a registered status
	// variant outside it keeps the info tone and its sheet class.
	tone := string(v)
	switch tone {
	case "info", "success", "warning", "danger":
	default:
		tone = "info"
	}
	live := headless.LivePolite
	if v == StatusDanger || v == StatusWarning {
		live = headless.LiveAssertive
	}

	var mods []string
	if string(v) != tone {
		// A registered variant (say "beta"): the sheet keeps its class
		// through the root's class append, the tone through the word.
		mods = append(mods, "fui-notification--"+string(v))
	}
	if cfg.Position != NotificationInline {
		mods = append(mods, "fui-notification--floating", "fui-notification--at-"+string(cfg.Position))
	}
	if cfg.Class != "" {
		mods = append(mods, cfg.Class)
	}
	parts := headless.Parts{}
	if len(mods) > 0 {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.Join(mods, " ")}}
	}
	if cfg.ID != "" {
		parts.Attrs = nil
		// id rides the primitive's own ID prop, not the attrs.
	}

	dismissLabel := cfg.DismissLabel
	if dismissLabel == "" && cfg.DismissHref != "" {
		dismissLabel = i18nui.T(ctx, i18nui.KeyNotificationDismiss)
	}
	return notificationStyle.WrapHTML(headless.Toast(headless.ToastProps{
		Tone:         tone,
		Icon:         render.Text(notificationGlyph(v)),
		Title:        cfg.Title,
		Body:         cfg.Body,
		DismissHref:  cfg.DismissHref,
		DismissLabel: dismissLabel,
		Island:       cfg.Island,
		Live:         live,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "role", "aria-live"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, notificationClasses))
}

// notificationClasses dresses headless.Toast's parts in this
// package's own vocabulary — the tone variant keeps the sheet's
// fui-notification--<tone> family, which the primitive renders as the
// root's variant class.
var notificationClasses = headless.Classes{
	headless.PartRoot:          "fui-notification",
	headless.PartToastToneWord: "fui-visually-hidden",
	headless.PartIcon:          "fui-notification__icon",
	headless.PartTitle:         "fui-notification__title",
	headless.PartBody:          "fui-notification__body",
	headless.PartDismiss:       "fui-notification__dismiss",

	"root--info":    "fui-notification--info",
	"root--success": "fui-notification--success",
	"root--warning": "fui-notification--warning",
	"root--danger":  "fui-notification--danger",
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
		// Registered custom status variants carry their own glyph.
		if icon := registeredStatusIcon(v); icon != "" {
			return icon
		}
		return "•"
	}
}
