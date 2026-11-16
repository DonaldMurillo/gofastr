package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// NotificationConfig configures a Notification, a stateless toast-styled status row.
type NotificationConfig struct {
	// Title is the first line. Required.
	Title string

	// Body is optional text below the title.
	Body string

	// Variant picks the tone and icon. Defaults to Info.
	Variant StatusVariant

	// DismissHref adds a dismiss link with this href; empty omits it.
	DismissHref string

	// DismissLabel overrides the dismiss link's accessible label.
	DismissLabel string

	// Position pins the notification to a screen corner; the default is inline.
	Position NotificationPosition

	// Island is the region the dismiss re-renders with script; required with DismissHref.
	Island headless.Island

	// Ctx resolves the default dismiss label; nil uses English.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, data-cui-*, role and aria-live are dropped.
	ExtraAttrs html.Attrs
}

// NotificationPosition controls where a Notification renders.
type NotificationPosition string

const (
	// NotificationInline (default) renders in document flow.
	NotificationInline NotificationPosition = ""

	NotificationTopRight    NotificationPosition = "top-right"
	NotificationTopLeft     NotificationPosition = "top-left"
	NotificationBottomRight NotificationPosition = "bottom-right"
	NotificationBottomLeft  NotificationPosition = "bottom-left"
)

// Notification renders the row: role=alert for warning and danger, role=status otherwise.
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

	dismissLabel := cfg.DismissLabel
	if dismissLabel == "" && cfg.DismissHref != "" {
		dismissLabel = i18nui.NotificationDismiss.Text(ctx)
	}
	return notificationStyle.WrapHTML(headless.Toast(headless.ToastProps{
		Tone:         tone,
		Icon:         headless.Own(render.Tag("span", nil, render.Text(notificationGlyph(v)))),
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

var notificationClasses = headless.Classes{
	headless.PartRoot:          "fui-notification",
	headless.PartToastToneWord: "fui-visually-hidden",
	headless.PartIcon:          "fui-notification__icon",
	headless.PartTitle:         "fui-notification__title",
	headless.PartBody:          "fui-notification__body",
	headless.PartDismiss:       "fui-notification__dismiss",
	headless.PartToastAction:   "fui-notification__action",

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
		if icon := registeredStatusIcon(v); icon != "" {
			return icon
		}
		return "•"
	}
}
