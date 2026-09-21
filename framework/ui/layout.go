package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Layout primitives ──────────────────────────────────────────────
//
// Stack, Cluster, Grid and Spacer render through their headless
// primitives dressed with this package's fui-* class map; Center, Box,
// Sticky and AspectRatio are layout facts with no accessibility
// contract of their own, and stay styled divs under the same fui-*
// vocabulary. All share one registered sheet (ui-layout) so the
// family loads as one unit.
//
// Apps that need anything beyond the canonical tokens are expected to
// reach for raw CSS via Class. These primitives intentionally don't
// proliferate options.

// Gap is a named spacing token. Maps to var(--spacing-*).
type Gap string

const (
	GapNone Gap = "none"
	GapXS   Gap = "xs"
	GapSM   Gap = "sm"
	GapMD   Gap = "" // default
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

// layoutModifierClasses is the shared modifier half of the layout
// family's class maps: the named gap, alignment and justify steps the
// headless primitives look up as root variants. One builder serves
// Stack, Cluster and Grid because the three share the scale.
func layoutModifierClasses(root string) headless.Classes {
	c := headless.Classes{headless.PartRoot: "fui-layout " + root}
	for _, g := range []string{"none", "xs", "sm", "lg", "xl", "2xl"} {
		c[headless.Part("root--gap-"+g)] = "fui-layout--gap-" + g
	}
	for _, a := range []string{"start", "center", "end", "baseline", "stretch"} {
		c[headless.Part("root--align-"+a)] = "fui-layout--align-" + a
	}
	for _, j := range []string{"start", "center", "end", "between", "around"} {
		c[headless.Part("root--justify-"+j)] = "fui-layout--justify-" + j
	}
	return c
}

var (
	stackClasses   = layoutModifierClasses("fui-stack")
	clusterClasses = func() headless.Classes {
		c := layoutModifierClasses("fui-cluster")
		c["root--wrap-none"] = "fui-cluster--nowrap"
		return c
	}()
	gridClasses   = layoutModifierClasses("fui-grid")
	spacerClasses = headless.Classes{headless.PartRoot: "fui-layout fui-spacer"}
)

// ─── Stack: vertical flex column ───────────────────────────────────

// StackConfig configures a vertical stack.
type StackConfig struct {
	Gap     Gap     // gap between children (default md)
	Align   Align   // cross-axis (horizontal) alignment
	Justify Justify // main-axis (vertical) alignment
	ID      string
	Class   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the stack's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-fui-* and the data-hui-* hooks.
	ExtraAttrs html.Attrs
}

// Stack renders children in a vertical column with consistent gap, on
// headless.Stack under this package's class map. The default
// replacement for hand-rolled `<div style="display:flex;
// flex-direction:column;gap:…">` patterns.
func Stack(cfg StackConfig, children ...render.HTML) render.HTML {
	return layoutStyle.WrapHTML(headless.Stack(headless.StackProps{
		Gap:        string(cfg.Gap),
		Align:      string(cfg.Align),
		Justify:    string(cfg.Justify),
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cfg.Class),
	}, stackClasses, children...))
}

// ─── Cluster: horizontal flex row with wrap ────────────────────────

// ClusterConfig configures a horizontal cluster.
type ClusterConfig struct {
	Gap     Gap
	Align   Align
	Justify Justify
	// NoWrap opts out of the default responsive wrapping behavior. Use it only
	// for compact chrome that is guaranteed to fit, such as two icon controls.
	NoWrap bool
	ID     string
	Class  string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the cluster's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-fui-* and the data-hui-* hooks.
	ExtraAttrs html.Attrs
}

// Cluster renders children in a horizontal row that wraps onto
// multiple lines when narrow, on headless.Cluster. Good for tag
// lists, action rows, breadcrumb trails.
func Cluster(cfg ClusterConfig, children ...render.HTML) render.HTML {
	return layoutStyle.WrapHTML(headless.Cluster(headless.ClusterProps{
		Gap:        string(cfg.Gap),
		Align:      string(cfg.Align),
		Justify:    string(cfg.Justify),
		NoWrap:     cfg.NoWrap,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cfg.Class),
	}, clusterClasses, children...))
}

// ─── Grid: responsive CSS grid ─────────────────────────────────────

// GridConfig configures a CSS grid.
type GridConfig struct {
	// Min is the minimum column width (e.g. "20rem"). The grid uses
	// `repeat(auto-fit, minmax(<Min>, 1fr))` so columns wrap at the
	// breakpoint implied by the minimum. Defaults to "16rem".
	Min   string
	Gap   Gap
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the grid's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-fui-*, data-hui-* and data-min (use Min).
	ExtraAttrs html.Attrs
}

// Grid renders children in an auto-fitting CSS grid, on headless.Grid
// under this package's class map. The default replacement for
// hand-rolled `grid-template-columns` declarations.
//
// Min is passed through `--ui-grid-min` (a CSS custom property the
// component declares on the root), so no inline `style="…"` is
// emitted, strict-CSP clean.
func Grid(cfg GridConfig, children ...render.HTML) render.HTML {
	min := cfg.Min
	if min == "" {
		min = "16rem"
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id", "data-min")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["data-min"] = min
	if cfg.Class != "" {
		// The class rides in the same root attrs as the hook: a part
		// map set beside them would be replaced, not merged.
		attrs["class"] = cfg.Class
	}
	return layoutStyle.WrapHTML(headless.Grid(headless.GridProps{
		Gap:   string(cfg.Gap),
		ID:    cfg.ID,
		Parts: headless.Parts{Attrs: headless.PartAttrs{headless.PartRoot: attrs}},
	}, gridClasses, children...))
}

// ─── Center: single child centered both axes ───────────────────────

// CenterConfig configures a centered region.
type CenterConfig struct {
	// MinHeight maps to a class: "viewport" (100vh), "screen" (100dvh
	// where supported), or "" (auto). Used for empty-state landing /
	// onboarding panels.
	MinHeight string
	ID        string
	Class     string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the region's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style and data-fui-*.
	ExtraAttrs html.Attrs
}

// Center centers its children both horizontally and vertically. A
// layout fact with no accessibility contract: it stays a styled div.
func Center(cfg CenterConfig, children ...render.HTML) render.HTML {
	cls := "fui-layout fui-center"
	if cfg.MinHeight != "" {
		cls += " fui-center--" + cfg.MinHeight
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
	}, children...))
}

// ─── Spacer: flexible filler ───────────────────────────────────────

// Spacer renders an empty flexible element that grows to fill
// available space, on headless.Spacer. Use inside a Stack or Cluster
// to push a sibling (e.g. an action button) to the far edge.
// Aria-hidden because it's purely visual.
func Spacer() render.HTML {
	return layoutStyle.WrapHTML(headless.Spacer(headless.SpacerProps{Grow: 1}, spacerClasses))
}

// ─── Box: wrapper with optional padding / background ───────────────

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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the box's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style and data-fui-*.
	ExtraAttrs html.Attrs
}

// Box is a wrapper that applies token-scaled padding and optional
// surface chrome. A layout fact with no accessibility contract: it
// stays a styled div. Use as the visible shell of any "content card"
// that doesn't need the full Card primitive's header/body/footer
// slots.
func Box(cfg BoxConfig, children ...render.HTML) render.HTML {
	cls := "fui-layout fui-box"
	if cfg.Pad != BoxPadNone {
		cls += " fui-box--pad-" + string(cfg.Pad)
	}
	if cfg.Surface {
		cls += " fui-box--surface"
	}
	if cfg.Outlined {
		cls += " fui-box--outlined"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return layoutStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
	}, children...))
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

	// ZIndexTier selects the z-index tier. Defaults to "sticky" when
	// empty. Valid values are exactly the five built-in ZIndexSet tiers
	// "sticky", "dropdown", "modal", "popover", "toast", which the
	// stylesheet maps to z-index: var(--z-<tier>). These are fixed
	// built-ins, not arbitrary theme-supplied tokens: validation and
	// the generated CSS only know these five, so an unknown tier panics
	// (a typo would otherwise silently fall back to the default layer).
	ZIndexTier string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the sticky wrapper's root
	// <div>. Keys the component owns are dropped: class and id (use
	// Class / ID), style and data-fui-* (which covers the derived
	// data-fui-z-tier).
	ExtraAttrs html.Attrs
}

// Sticky wraps children in a position:sticky container. A layout
// fact with no accessibility contract: it stays a styled div.
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
	switch tier {
	case "sticky", "dropdown", "modal", "popover", "toast":
	default:
		panic("ui: Sticky ZIndexTier must be one of sticky/dropdown/modal/popover/toast (theme ZIndexSet tokens), got " + strconv.Quote(tier))
	}

	cls := "fui-sticky fui-sticky--" + string(edge) + " fui-sticky--offset-" + string(offset)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["class"] = cls
	attrs["data-fui-z-tier"] = tier
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return stickyStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// ─── AspectRatio ───────────────────────────────────────────────────
//
// Pure-CSS aspect-ratio wrapper that prevents layout shift for images,
// videos, and embeds whose dimensions aren't known at SSR time. A
// layout fact with no accessibility contract: it stays a styled div.

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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the wrapper's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style and data-fui-*.
	ExtraAttrs html.Attrs
}

// AspectRatio wraps a single child in a container with the given
// aspect ratio. The child is absolutely positioned to fill the box.
//
// Use for responsive images, video embeds, placeholder skeletons
// with known proportions, or any content whose intrinsic size is
// unknown at SSR time.
func AspectRatioComponent(cfg AspectRatioConfig, child render.HTML) render.HTML {
	cls := "fui-ar--" + string(cfg.Ratio)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["data-fui-comp"] = "ui-aspect-ratio"
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return aspectRatioStyle.WrapHTML(render.Tag("div", attrs, child))
}
