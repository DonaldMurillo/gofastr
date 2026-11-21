package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/idslug"
)

// TooltipPlacement selects the side the tooltip appears on; Left and
// Right are the reading direction's start and end.
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

	// ID is the pop's id, named by the trigger's aria-describedby; empty derives it from Text.
	ID string

	Class string

	// ExtraAttrs forwards attributes to the wrapper span; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs

	// Ctx is the page's render context; without an id scope a derived id takes a counter suffix.
	Ctx context.Context
}

// Tooltip wraps trigger with a role=tooltip pop that CSS shows on hover
// and focus, and points the trigger at it with aria-describedby.
func Tooltip(cfg TooltipConfig, trigger render.HTML) render.HTML {
	if cfg.Text == "" {
		panic("ui: Tooltip requires Text")
	}
	id := cfg.ID
	if id == "" {
		id = "tip-" + idslug.Make(cfg.Text)
		if id == "tip-" {
			id += textHash(cfg.Text)
		}
		id = scopedID(cfg.Ctx, id)
	}

	cls := "fui-tooltip"
	placement := cfg.Placement
	if placement != TooltipTop {
		cls += " fui-tooltip--" + string(placement)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	triggerWithDescribedBy, _ := registry.InjectAttribute(trigger, "aria-describedby", id)

	pop := html.Span(html.TextConfig{
		Class:      "fui-tooltip__pop",
		ID:         id,
		ExtraAttrs: html.Attrs{"role": "tooltip", "data-cui-internal": ""},
	}, render.Text(cfg.Text))

	return tooltipStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, triggerWithDescribedBy, pop))
}
