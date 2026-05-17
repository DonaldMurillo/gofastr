package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Tooltip ────────────────────────────────────────────────────────
//
// A CSS-only hover/focus tooltip. The visible pop element is always
// present in the DOM and toggled by `:hover` / `:focus-visible` /
// `:focus-within` rules on the wrapper — no JavaScript required, no
// runtime callouts, no flash-on-mount.
//
// The wrapper carries data-fui-comp="ui-tooltip" so the stylesheet
// loads lazily on first appearance. The popped element is wired via
// aria-describedby so screen readers announce the tooltip alongside
// the trigger.

// TooltipPlacement selects the side the tooltip appears on.
type TooltipPlacement string

const (
	TooltipTop    TooltipPlacement = "" // default
	TooltipBottom TooltipPlacement = "bottom"
	TooltipLeft   TooltipPlacement = "left"
	TooltipRight  TooltipPlacement = "right"
)

// TooltipConfig configures a tooltip.
type TooltipConfig struct {
	// Text is the tooltip message. Required.
	Text string

	// Placement selects the side. Default top.
	Placement TooltipPlacement

	// ID is the tooltip's id; the trigger's aria-describedby points
	// to it. When empty, a stable id is derived from the trigger's
	// content position.
	ID string

	Class string
}

// Tooltip wraps the given trigger HTML and appends a hidden tooltip
// pop. The trigger is unwrapped — Tooltip only adds a containing
// span + the pop element, so inline buttons and links stay inline.
//
// Use on icon-only buttons, truncated labels, or anywhere extra
// context is useful without occupying layout space.
func Tooltip(cfg TooltipConfig, trigger render.HTML) render.HTML {
	if cfg.Text == "" {
		panic("ui: Tooltip requires Text")
	}
	id := cfg.ID
	if id == "" {
		// Derive a content-stable id so SSR and runtime agree.
		id = "tip-" + slug(cfg.Text)
	}

	cls := "ui-tooltip"
	placement := cfg.Placement
	if placement != TooltipTop {
		cls += " ui-tooltip--" + string(placement)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// The trigger receives aria-describedby — splice it via the
	// existing injectAttrs helper from form.go so the caller's
	// element gets the attribute without re-parsing.
	triggerWithDescribedBy := injectAttrs(trigger, ` aria-describedby="`+id+`"`)

	pop := html.Span(html.TextConfig{
		Class: "ui-tooltip__pop",
		ID:    id,
		Attrs: html.Attrs{"role": "tooltip"},
	}, render.Text(cfg.Text))

	return tooltipStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls,
	}, triggerWithDescribedBy, pop))
}
