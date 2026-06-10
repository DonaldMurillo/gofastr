package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── BackToTop ──────────────────────────────────────────────────────
//
// A fixed-position button that appears after the user scrolls past a
// configurable threshold and smooth-scrolls to the top of the page on
// click. Uses a lightweight runtime module with IntersectionObserver
// for the visibility toggle — no scroll-event listener churn.

// BackToTopPosition selects which corner the button anchors to.
type BackToTopPosition string

const (
	BackToTopBottomRight BackToTopPosition = "br"
	BackToTopBottomLeft  BackToTopPosition = "bl"
	BackToTopTopRight    BackToTopPosition = "tr"
	BackToTopTopLeft     BackToTopPosition = "tl"
)

// BackToTopSize controls the button diameter.
type BackToTopSize string

const (
	BackToTopSM BackToTopSize = "sm"
	BackToTopMD BackToTopSize = "" // default (2.75rem)
	BackToTopLG BackToTopSize = "lg"
)

// BackToTopVariant selects the color variant.
type BackToTopVariant string

const (
	BackToTopPrimary   BackToTopVariant = ""          // default — solid primary
	BackToTopSecondary BackToTopVariant = "secondary" // outlined, subtle
	BackToTopGhost     BackToTopVariant = "ghost"     // transparent bg, only visible on hover
)

// BackToTopOffset presets for distance from the viewport edge.
type BackToTopOffset string

const (
	BackToTopOffsetNone BackToTopOffset = "none"
	BackToTopOffsetSM   BackToTopOffset = "sm"
	BackToTopOffsetMD   BackToTopOffset = "" // default
	BackToTopOffsetLG   BackToTopOffset = "lg"
	BackToTopOffsetXL   BackToTopOffset = "xl"
)

// BackToTopScrollBehavior controls the scroll animation.
type BackToTopScrollBehavior string

const (
	BackToTopSmooth  BackToTopScrollBehavior = "" // default
	BackToTopInstant BackToTopScrollBehavior = "instant"
)

// BackToTopConfig configures the back-to-top button.
type BackToTopConfig struct {
	// Position selects which corner the button anchors to.
	// Defaults to BackToTopBottomRight when empty.
	Position BackToTopPosition

	// Icon overrides the button content. Pass any render.HTML
	// (SVG markup, text, an icon component, etc).
	// Defaults to a chevron-up arrow SVG.
	Icon render.HTML

	// ThresholdPx is the scroll distance in pixels before the
	// button becomes visible. Defaults to 400 when 0.
	ThresholdPx int

	// Smooth controls scroll-to-top behavior.
	// Defaults to smooth scrolling (BackToTopSmooth).
	// Set to BackToTopInstant for no animation.
	Smooth BackToTopScrollBehavior

	// Size controls the button diameter.
	// Defaults to BackToTopMD (2.75rem).
	Size BackToTopSize

	// Variant controls the color scheme.
	// Defaults to BackToTopPrimary (solid primary color).
	Variant BackToTopVariant

	// Offset controls the distance from the viewport edge.
	// Defaults to BackToTopOffsetMD.
	Offset BackToTopOffset

	// Label overrides the aria-label. Defaults to "Back to top".
	Label string

	// ScrollTarget overrides the scroll-to selector.
	// Defaults to scrolling to y=0. Set to a CSS selector
	// (e.g. "#main-content") to scroll a specific element
	// into view instead.
	ScrollTarget string

	// ID is an optional id for the root element.
	ID string

	// Class is an optional extra CSS class.
	Class string
}

// defaultArrowUpSVG is the chevron-up icon shown by default.
const defaultArrowUpSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="18 15 12 9 6 15"/></svg>`

// BackToTop renders a smooth-scroll "back to top" button that appears
// after the user scrolls past the configured threshold.
//
// The button is hidden on initial render (aria-hidden until visible).
// A small runtime module uses an IntersectionObserver on a sentinel
// to toggle visibility. No scroll-event listener is needed.
//
// Usage:
//
//	ui.BackToTop(ui.BackToTopConfig{})
//	ui.BackToTop(ui.BackToTopConfig{ThresholdPx: 800})
//	ui.BackToTop(ui.BackToTopConfig{
//	    Position: ui.BackToTopBottomLeft,
//	    Size:     ui.BackToTopLG,
//	    Variant:  ui.BackToTopGhost,
//	    Icon:     render.Raw(`<svg>...</svg>`),
//	})
func BackToTop(cfg BackToTopConfig) render.HTML {
	pos := cfg.Position
	if pos == "" {
		pos = BackToTopBottomRight
	}
	threshold := cfg.ThresholdPx
	if threshold == 0 {
		threshold = 400
	}
	label := cfg.Label
	if label == "" {
		label = "Back to top"
	}
	size := cfg.Size
	variant := cfg.Variant
	offset := cfg.Offset

	cls := "ui-back-to-top"
	if pos != "" {
		cls += " ui-back-to-top--" + string(pos)
	}
	if size != "" {
		cls += " ui-back-to-top--" + string(size)
	}
	if variant != "" {
		cls += " ui-back-to-top--" + string(variant)
	}
	if offset != "" && offset != BackToTopOffsetMD {
		cls += " ui-back-to-top--offset-" + string(offset)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"type":                   "button",
		"data-fui-back-to-top":   "",
		"data-fui-btt-threshold": fmt.Sprintf("%d", threshold),
		"aria-label":             label,
		"inert":                  "",
		"class":                  cls,
	}
	if cfg.Smooth != "" {
		attrs["data-fui-btt-scroll"] = string(cfg.Smooth)
	}
	if cfg.ScrollTarget != "" {
		attrs["data-fui-btt-target"] = cfg.ScrollTarget
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	icon := cfg.Icon
	if icon == "" {
		icon = render.Raw(defaultArrowUpSVG)
	}

	return backToTopStyle.WrapHTML(render.Tag("button", attrs, icon))
}
