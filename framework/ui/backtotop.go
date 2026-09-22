package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── BackToTop ──────────────────────────────────────────────────────
//
// A fixed-position button that appears after the user scrolls past a
// configurable threshold and smooth-scrolls to the top of the page on
// click. Uses a lightweight runtime module with IntersectionObserver
// for the visibility toggle. No scroll-event listener churn.

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
	BackToTopPrimary   BackToTopVariant = ""          // default: solid primary
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root <button>. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// data-fui-*, type, aria-label (use Label), and inert (the
	// runtime's initial-hidden wiring).
	ExtraAttrs html.Attrs
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
	threshold := cfg.ThresholdPx
	if threshold == 0 {
		threshold = 400
	}
	// The scroll target is an element id on the headless primitive,
	// not a CSS selector: a "#main" selector string becomes "main"
	// here, because the primitive refuses the "#" spelling and the
	// module resolves by id.
	target := strings.TrimPrefix(cfg.ScrollTarget, "#")

	var mods []string
	if cfg.Position != "" {
		mods = append(mods, "fui-back-to-top--"+string(cfg.Position))
	}
	if cfg.Size != "" {
		mods = append(mods, "fui-back-to-top--"+string(cfg.Size))
	}
	if cfg.Variant != "" {
		mods = append(mods, "fui-back-to-top--"+string(cfg.Variant))
	}
	if cfg.Offset != "" && cfg.Offset != BackToTopOffsetMD {
		mods = append(mods, "fui-back-to-top--offset-"+string(cfg.Offset))
	}
	if cfg.Class != "" {
		mods = append(mods, cfg.Class)
	}
	parts := headless.Parts{}
	if len(mods) > 0 {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.Join(mods, " ")}}
	}

	icon := cfg.Icon
	if icon == "" {
		icon = render.HTML(defaultArrowUpSVG)
	}

	return backToTopStyle.WrapHTML(headless.BackToTop(headless.BackToTopProps{
		Href:      "#top",
		Target:    target,
		Label:     cfg.Label,
		Threshold: threshold,
		Icon:      icon,
		Smooth:    cfg.Smooth != BackToTopInstant,
		ID:        cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "href",
			"aria-label", "data-hui-back-to-top", "data-hui-back-to-top-target"),
		Parts:   parts,
		Strings: StringsFor(nil),
	}, backToTopClasses))
}

// backToTopClasses dresses headless.BackToTop's parts in this
// package's own vocabulary; the icon slot carries the chevron.
var backToTopClasses = headless.Classes{
	headless.PartRoot:  "fui-back-to-top",
	headless.PartIcon:  "fui-back-to-top__icon",
	headless.PartLabel: "fui-visually-hidden",
}
