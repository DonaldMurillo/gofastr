package ui

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
	// Label renders a centered label ("OR") on a div role=separator instead of an <hr>.
	Label       string
	Orientation DividerOrientation
	ID          string
	Class       string
	// ExtraAttrs reach the root; class, id, style, role, aria-orientation and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var dividerClasses = headless.Classes{
	headless.PartRoot:                      "fui-divider",
	headless.Part("root--orient-vertical"): "fui-divider--vertical",
	headless.PartDividerLine:               "fui-divider__line",
	headless.PartText:                      "fui-divider__label",
}

// Divider renders a separator: an <hr>, or a labelled div role=separator.
func Divider(cfg DividerConfig) render.HTML {
	cls := cfg.Class
	if cfg.Label != "" {
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
