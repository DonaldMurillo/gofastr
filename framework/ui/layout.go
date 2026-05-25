package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Layout primitives ──────────────────────────────────────────────
//
// Six small wrappers that cover the boring spatial decisions every
// page makes: vertical stacking, horizontal clustering, grid, centring,
// spacing, and box-with-padding. All emit a single shared
// data-fui-comp="ui-layout" so one stylesheet covers the family.
//
// Apps that need anything beyond the canonical tokens are expected to
// reach for raw CSS via Class — these primitives intentionally don't
// proliferate options.

// Gap is a named spacing token. Maps to var(--spacing-*).
type Gap string

const (
	GapNone Gap = "none"
	GapXS   Gap = "xs"
	GapSM   Gap = "sm"
	GapMD   Gap = ""   // default
	GapLG   Gap = "lg"
	GapXL   Gap = "xl"
	Gap2XL  Gap = "2xl"
)

// Align is a cross-axis alignment value.
type Align string

const (
	AlignStart    Align = "start"
	AlignCenter   Align = "center"
	AlignEnd      Align = "end"
	AlignBaseline Align = "baseline"
	AlignStretch  Align = "stretch"
)

// Justify is a main-axis alignment value.
type Justify string

const (
	JustifyStart   Justify = "start"
	JustifyCenter  Justify = "center"
	JustifyEnd     Justify = "end"
	JustifyBetween Justify = "between"
	JustifyAround  Justify = "around"
)

// ─── Stack — vertical flex column ───────────────────────────────────

// StackConfig configures a vertical stack.
type StackConfig struct {
	Gap     Gap     // gap between children (default md)
	Align   Align   // cross-axis (horizontal) alignment
	Justify Justify // main-axis (vertical) alignment
	ID      string
	Class   string
}

// Stack renders children in a vertical column with consistent gap.
// The default replacement for hand-rolled `<div style="display:flex;
// flex-direction:column;gap:…">` patterns.
func Stack(cfg StackConfig, children ...render.HTML) render.HTML {
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{
		Class: layoutClass("ui-stack", cfg.Class, cfg.Gap, cfg.Align, cfg.Justify),
		ID:    cfg.ID,
	}, children...))
}

// ─── Cluster — horizontal flex row with wrap ────────────────────────

// ClusterConfig configures a horizontal cluster.
type ClusterConfig struct {
	Gap     Gap
	Align   Align
	Justify Justify
	Wrap    bool // when true (default), children wrap onto multiple lines
	ID      string
	Class   string
}

// Cluster renders children in a horizontal row that wraps onto
// multiple lines when narrow. Good for tag lists, action rows,
// breadcrumb trails.
func Cluster(cfg ClusterConfig, children ...render.HTML) render.HTML {
	cls := layoutClass("ui-cluster", cfg.Class, cfg.Gap, cfg.Align, cfg.Justify)
	if !cfg.Wrap {
		cls += " ui-cluster--nowrap"
	}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, children...))
}

// ─── Grid — responsive CSS grid ─────────────────────────────────────

// GridConfig configures a CSS grid.
type GridConfig struct {
	// Min is the minimum column width (e.g. "20rem"). The grid uses
	// `repeat(auto-fit, minmax(<Min>, 1fr))` so columns wrap at the
	// breakpoint implied by the minimum. Defaults to "16rem".
	Min   string
	Gap   Gap
	ID    string
	Class string
}

// Grid renders children in an auto-fitting CSS grid. The default
// replacement for hand-rolled `grid-template-columns` declarations.
//
// Min is passed through `--ui-grid-min` (a CSS custom property the
// component declares on the root), so no inline `style="…"` is
// emitted — strict-CSP clean.
func Grid(cfg GridConfig, children ...render.HTML) render.HTML {
	cls := layoutClass("ui-grid", cfg.Class, cfg.Gap, "", "")
	min := cfg.Min
	if min == "" {
		min = "16rem"
	}
	// CSS custom property goes via a data attribute that the
	// stylesheet reads with attr(). Falls back to the default via
	// var() chaining for browsers that don't support attr() with
	// non-string types yet (Chrome ≥ 125; we expose a class hook for
	// the size buckets to keep older browsers usable).
	attrs := html.Attrs{"data-min": min}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{
		Class: cls, ID: cfg.ID, ExtraAttrs: attrs,
	}, children...))
}

// ─── Center — single child centered both axes ───────────────────────

// CenterConfig configures a centered region.
type CenterConfig struct {
	// MinHeight maps to a class — "viewport" (100vh), "screen" (100dvh
	// where supported), or "" (auto). Used for empty-state landing /
	// onboarding panels.
	MinHeight string
	ID        string
	Class     string
}

// Center centers its children both horizontally and vertically.
func Center(cfg CenterConfig, children ...render.HTML) render.HTML {
	cls := "ui-layout ui-center"
	if cfg.MinHeight != "" {
		cls += " ui-center--" + cfg.MinHeight
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, children...))
}

// ─── Spacer — flexible filler ───────────────────────────────────────

// Spacer renders an empty flexible element that grows to fill
// available space. Use inside a Stack or Cluster to push a sibling
// (e.g. an action button) to the far edge. Aria-hidden because it's
// purely visual.
func Spacer() render.HTML {
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{
		Class: "ui-layout ui-spacer",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	}))
}

// ─── Box — wrapper with optional padding / background ───────────────

// BoxPad selects a named padding value. "" is no padding.
type BoxPad string

const (
	BoxPadNone BoxPad = ""
	BoxPadSM   BoxPad = "sm"
	BoxPadMD   BoxPad = "md"
	BoxPadLG   BoxPad = "lg"
	BoxPadXL   BoxPad = "xl"
)

// BoxConfig configures a Box wrapper.
type BoxConfig struct {
	Pad      BoxPad // padding (none | sm | md | lg | xl)
	Surface  bool   // when true, applies the surface background + border-radius
	Outlined bool   // when true, applies a 1px border (pairs well with Surface=false)
	ID       string
	Class    string
}

// Box is a wrapper that applies token-scaled padding and optional
// surface chrome. Use as the visible shell of any "content card"
// that doesn't need the full Card primitive's header/body/footer
// slots.
func Box(cfg BoxConfig, children ...render.HTML) render.HTML {
	cls := "ui-layout ui-box"
	if cfg.Pad != BoxPadNone {
		cls += " ui-box--pad-" + string(cfg.Pad)
	}
	if cfg.Surface {
		cls += " ui-box--surface"
	}
	if cfg.Outlined {
		cls += " ui-box--outlined"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, children...))
}

// ─── Sticky ────────────────────────────────────────────────────────

// StickyEdge selects which edge the element sticks to.
type StickyEdge string

const (
	StickyTop    StickyEdge = "top"
	StickyBottom StickyEdge = "bottom"
)

// StickyOffset presets for common sticky offsets.
type StickyOffset string

const (
	StickyOffsetNone StickyOffset = "0"
	StickyOffsetSm   StickyOffset = "sm"
	StickyOffsetMd   StickyOffset = "md"
	StickyOffsetLg   StickyOffset = "lg"
	StickyOffsetXl   StickyOffset = "xl"
)

// StickyConfig configures a position:sticky wrapper.
//
// Wraps children in a div that sticks to the chosen viewport edge
// on scroll. Uses theme tokens for z-index so sticky elements
// layer consistently with modals, widgets, and other surfaces.
//
// Usage:
//
//	ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop},
//		ui.Button(ui.ButtonConfig{Label: "Save"}),
//	)
//	ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop, Offset: ui.StickyOffsetLg}, header)
//	ui.Sticky(ui.StickyConfig{Edge: ui.StickyBottom}, toolbar)
type StickyConfig struct {
	// Edge selects which edge to stick to.
	// Defaults to StickyTop when empty.
	Edge StickyEdge

	// Offset selects the distance preset from the edge.
	// Defaults to StickyOffsetNone when empty.
	Offset StickyOffset

	// ZIndexTier selects the z-index tier from the theme token
	// system. Defaults to "sticky" when empty.
	// Common values: "sticky", "dropdown", "overlay", "modal".
	ZIndexTier string

	ID    string
	Class string
}

// Sticky wraps children in a position:sticky container.
func Sticky(cfg StickyConfig, children ...render.HTML) render.HTML {
	edge := cfg.Edge
	if edge == "" {
		edge = StickyTop
	}
	offset := cfg.Offset
	if offset == "" {
		offset = StickyOffsetNone
	}
	tier := cfg.ZIndexTier
	if tier == "" {
		tier = "sticky"
	}

	cls := "ui-sticky ui-sticky--" + string(edge) + " ui-sticky--offset-" + string(offset)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":           cls,
		"data-fui-sticky":  string(edge),
		"data-fui-z-tier":  tier,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return stickyStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// ─── AspectRatio ───────────────────────────────────────────────────
//
// Pure-CSS aspect-ratio wrapper that prevents layout shift for images,
// videos, and embeds whose dimensions aren't known at SSR time.

// AspectRatio selects a CSS aspect-ratio bucket.
type AspectRatio string

const (
	AspectRatio1_1  AspectRatio = "1-1"
	AspectRatio4_3  AspectRatio = "4-3"
	AspectRatio16_9 AspectRatio = "16-9"
	AspectRatio21_9 AspectRatio = "21-9"
	AspectRatio3_4  AspectRatio = "3-4"
	AspectRatio3_2  AspectRatio = "3-2"
	AspectRatio2_3  AspectRatio = "2-3"
	AspectRatioAuto AspectRatio = "auto"
)

// AspectRatioConfig configures an aspect-ratio wrapper.
type AspectRatioConfig struct {
	// Ratio is the aspect-ratio bucket (required). Use one of the
	// AspectRatio* constants.
	Ratio AspectRatio

	// Class adds extra CSS classes.
	Class string

	// ID sets the element id.
	ID string
}

// AspectRatio wraps a single child in a container with the given
// aspect ratio. The child is absolutely positioned to fill the box.
//
// Use for responsive images, video embeds, placeholder skeletons
// with known proportions, or any content whose intrinsic size is
// unknown at SSR time.
func AspectRatioComponent(cfg AspectRatioConfig, child render.HTML) render.HTML {
	cls := "ui-ar--" + string(cfg.Ratio)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"data-fui-comp": "ui-aspect-ratio",
		"class":         cls,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return aspectRatioStyle.WrapHTML(render.Tag("div", attrs, child))
}

// ─── helpers ────────────────────────────────────────────────────────

func layoutClass(base, extra string, gap Gap, align Align, justify Justify) string {
	cls := "ui-layout " + base
	if gap != GapMD {
		cls += " ui-layout--gap-" + string(gap)
	}
	if align != "" {
		cls += " ui-layout--align-" + string(align)
	}
	if justify != "" {
		cls += " ui-layout--justify-" + string(justify)
	}
	if extra != "" {
		cls += " " + extra
	}
	return cls
}
