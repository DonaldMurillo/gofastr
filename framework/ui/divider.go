package ui

// ─── Divider ────────────────────────────────────────────────────────
//
// headless.Divider carries the separator contract: a meaningful break
// is an <hr>, a vertical break carries aria-orientation, a labelled
// one claims the separator role on the div that replaces the hr. This
// adapter dresses it with the fui-divider class map.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// DividerOrientation selects horizontal vs. vertical line.
type DividerOrientation string

const (
	DividerHorizontal DividerOrientation = "" // default
	DividerVertical   DividerOrientation = "vertical"
)

// DividerConfig configures a divider.
type DividerConfig struct {
	// Label optionally renders a centered inline label. Common
	// usage: "OR" between two auth options, "Pinned" above the rest
	// of a list. When set, the divider switches from a plain <hr>
	// to a labelled <div role="separator">.
	Label string

	// Orientation selects horizontal (default) or vertical.
	Orientation DividerOrientation

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root element (<hr>,
	// or the role=separator div for vertical / labelled shapes).
	// Keys the component owns are dropped: class and id (use Class /
	// ID), style, data-fui-*, role and aria-orientation (use
	// Orientation).
	ExtraAttrs html.Attrs
}

// dividerClasses dresses headless.Divider's parts.
var dividerClasses = headless.Classes{
	headless.PartRoot:                      "fui-divider",
	headless.Part("root--orient-vertical"): "fui-divider--vertical",
	headless.PartDividerLine:               "fui-divider__line",
	headless.PartText:                      "fui-divider__label",
}

// Divider renders a semantic separator on headless.Divider: plain
// horizontal dividers are the native <hr>; vertical or labelled
// dividers carry the orientation / label on the element the contract
// gives them.
func Divider(cfg DividerConfig) render.HTML {
	cls := cfg.Class
	if cfg.Label != "" {
		// The labelled shape is its own modifier in this package's
		// sheet; the primitive knows the role, the class map knows
		// the look.
		cls = joinNonEmpty("fui-divider--labelled", cls)
	}
	return dividerStyle.WrapHTML(headless.Divider(headless.DividerProps{
		Label:      cfg.Label,
		Vertical:   cfg.Orientation == DividerVertical,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-orientation"),
		Parts:      rootClassParts(cls),
	}, dividerClasses))
}
func joinNonEmpty(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += p
	}
	return out
}
