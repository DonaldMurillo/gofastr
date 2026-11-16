package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

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

// StackConfig configures a vertical stack.
type StackConfig struct {
	Gap     Gap     // gap between children (default md)
	Align   Align   // cross-axis (horizontal) alignment
	Justify Justify // main-axis (vertical) alignment
	ID      string
	Class   string
	// TrimMargins drops direct children's block margins so Gap owns the vertical rhythm.
	TrimMargins bool
	// Screen makes the stack the page column, at least one viewport tall with its
	// last child at the bottom. Put the site header inside it; never nest two.
	Screen bool
	// StickyHeader pins the <header> child and publishes --size-header-height as
	// --ui-sticky-top, the offset every sticky part below it reads.
	StickyHeader bool

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Stack renders children in a vertical column with a consistent gap.
func Stack(cfg StackConfig, children ...render.HTML) render.HTML {
	if cfg.TrimMargins {
		cfg.Class = cls(cfg.Class, "fui-stack--trim-margins")
	}
	if cfg.Screen {
		cfg.Class = cls(cfg.Class, "fui-stack--screen")
	}
	if cfg.StickyHeader {
		cfg.Class = cls(cfg.Class, "fui-stack--sticky-header")
	}
	return layoutStyle.WrapHTML(headless.Stack(headless.StackProps{
		Gap:        string(cfg.Gap),
		Align:      string(cfg.Align),
		Justify:    string(cfg.Justify),
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cfg.Class),
	}, stackClasses, children...))
}

// ClusterConfig configures a horizontal cluster.
type ClusterConfig struct {
	Gap     Gap
	Align   Align
	Justify Justify
	// NoWrap keeps the row on one line; use it only for chrome that always fits.
	NoWrap bool
	// Shrink lets the row narrow below its content, its last child taking the
	// squeeze (a truncating breadcrumb after a menu button). Pair it with NoWrap.
	Shrink bool
	ID     string
	Class  string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Cluster renders children in a horizontal row that wraps when narrow.
func Cluster(cfg ClusterConfig, children ...render.HTML) render.HTML {
	return layoutStyle.WrapHTML(headless.Cluster(headless.ClusterProps{
		Gap:        string(cfg.Gap),
		Align:      string(cfg.Align),
		Justify:    string(cfg.Justify),
		NoWrap:     cfg.NoWrap,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(clusterClass(cfg)),
	}, clusterClasses, children...))
}

func clusterClass(cfg ClusterConfig) string {
	if !cfg.Shrink {
		return cfg.Class
	}
	return strings.TrimSpace("fui-cluster--shrink " + cfg.Class)
}

// GridConfig configures a CSS grid.
type GridConfig struct {
	// Min is the minimum column width, default 16rem; columns auto-fit at it.
	Min string
	Gap Gap
	// Fill keeps empty columns (auto-fill), so a short last row keeps full-row widths.
	Fill  bool
	ID    string
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Grid renders children in an auto-fitting CSS grid.
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
	class := cfg.Class
	if cfg.Fill {
		class = strings.TrimSpace("fui-grid--fill " + class)
	}
	if class != "" {
		attrs["class"] = class
	}
	return layoutStyle.WrapHTML(headless.Grid(headless.GridProps{
		Gap:   string(cfg.Gap),
		ID:    cfg.ID,
		Parts: headless.Parts{Attrs: headless.PartAttrs{headless.PartRoot: attrs}},
	}, gridClasses, children...))
}

// CenterConfig configures a centered region.
type CenterConfig struct {
	// MinHeight is "", "viewport" (100vh) or "screen" (100dvh); anything else panics.
	MinHeight string
	ID        string
	Class     string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Center centers its children on both axes.
func Center(cfg CenterConfig, children ...render.HTML) render.HTML {
	cls := "fui-layout fui-center"
	switch cfg.MinHeight {
	case "":
	case "viewport", "screen":
		cls += " fui-center--" + cfg.MinHeight
	default:
		panic("ui: Center MinHeight must be \"\", \"viewport\" or \"screen\", got " + strconv.Quote(cfg.MinHeight))
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

// Spacer renders an aria-hidden filler that grows to push a sibling to the far edge.
func Spacer() render.HTML {
	return layoutStyle.WrapHTML(headless.Spacer(headless.SpacerProps{Grow: 1}, spacerClasses))
}

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
	// Bounded caps the block size at --ui-box-bounded-size (20rem) and scrolls the overflow.
	Bounded bool
	ID      string
	Class   string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Box wraps children in token-scaled padding with optional surface chrome.
func Box(cfg BoxConfig, children ...render.HTML) render.HTML {
	cls := "fui-layout fui-box"
	switch cfg.Pad {
	case BoxPadNone:
	case BoxPadSM, BoxPadMD, BoxPadLG, BoxPadXL:
		cls += " fui-box--pad-" + string(cfg.Pad)
	default:
		panic("ui: Box Pad must be one of \"\"/sm/md/lg/xl, got " + strconv.Quote(string(cfg.Pad)))
	}
	if cfg.Surface {
		cls += " fui-box--surface"
	}
	if cfg.Outlined {
		cls += " fui-box--outlined"
	}
	if cfg.Bounded {
		cls += " fui-box--bounded"
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
type StickyConfig struct {
	// Edge is the edge to stick to, StickyTop when empty.
	Edge StickyEdge

	// Offset is the distance preset from the edge, StickyOffsetNone when empty.
	Offset StickyOffset

	// ZIndexTier is sticky (the default), dropdown, modal, popover or toast; any other value panics.
	ZIndexTier string

	ID    string
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
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
	switch edge {
	case StickyTop, StickyBottom:
	default:
		panic("ui: Sticky Edge must be top or bottom, got " + strconv.Quote(string(edge)))
	}
	switch offset {
	case StickyOffsetNone, StickyOffsetSm, StickyOffsetMd, StickyOffsetLg, StickyOffsetXl:
	default:
		panic("ui: Sticky Offset must be one of 0/sm/md/lg/xl, got " + strconv.Quote(string(offset)))
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
	// Ratio is the aspect-ratio bucket, required: one of the AspectRatio* constants.
	Ratio AspectRatio

	// Class adds extra CSS classes.
	Class string

	// ID sets the element id.
	ID string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// AspectRatioComponent wraps one child in a box of the given aspect ratio, the
// child filling it, so media of unknown size causes no layout shift.
func AspectRatioComponent(cfg AspectRatioConfig, child render.HTML) render.HTML {
	switch cfg.Ratio {
	case AspectRatio1_1, AspectRatio4_3, AspectRatio16_9, AspectRatio21_9,
		AspectRatio3_4, AspectRatio3_2, AspectRatio2_3, AspectRatioAuto:
	default:
		panic("ui: AspectRatio Ratio must be one of the AspectRatio* constants, got " + strconv.Quote(string(cfg.Ratio)))
	}
	cls := "fui-ar--" + string(cfg.Ratio)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["data-cui-comp"] = "ui-aspect-ratio"
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return aspectRatioStyle.WrapHTML(render.Tag("div", attrs, child))
}
