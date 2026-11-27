package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
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

	// Placement selects the preferred side: TooltipTop (or empty), TooltipBottom, TooltipLeft or TooltipRight; any other value panics.
	Placement TooltipPlacement

	// ID is the pop's id, added to the trigger's aria-describedby list; empty derives it from Text. An ID that is not a fragment identifier (whitespace, #, quotes, < or >) panics.
	ID string

	Class string

	// ExtraAttrs forwards attributes to the wrapper span; class, id, role, style, on* and the framework's own data-* hooks are dropped.
	ExtraAttrs html.Attrs

	// Ctx is the page's render context; without an id scope a derived id takes a counter suffix.
	Ctx context.Context
}

// Tooltip wraps trigger with a role=tooltip pop and points the trigger at
// it with aria-describedby. The sheet shows the pop on hover and focus;
// the headless-tooltip module raises it into the top layer, keeps it open
// while the pointer is over it and closes it on Escape.
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

	root := "fui-tooltip"
	placement := string(cfg.Placement)
	if placement != "" {
		root += " fui-tooltip--" + placement
	}
	if cfg.Class != "" {
		root += " " + cfg.Class
	}
	return tooltipStyle.WrapHTML(headless.Tooltip(headless.TooltipProps{
		Text:       cfg.Text,
		Trigger:    trigger,
		Placement:  placement,
		ID:         id,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, headless.Classes{
		headless.PartRoot:       root,
		headless.PartTooltipPop: "fui-tooltip__pop",
	}))
}
