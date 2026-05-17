package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Divider ────────────────────────────────────────────────────────

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
}

// Divider renders a semantic separator. Plain horizontal dividers use
// the native <hr> element; vertical or labelled dividers use a
// role="separator" div so the orientation / label gets announced.
func Divider(cfg DividerConfig) render.HTML {
	cls := "ui-divider"
	if cfg.Orientation != DividerHorizontal {
		cls += " ui-divider--" + string(cfg.Orientation)
	}
	if cfg.Label != "" {
		cls += " ui-divider--labelled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Native <hr> is the cleanest case — no label, horizontal, no
	// extra DOM. Keeps "<hr>" findable in view-source for plain
	// dividers and avoids unnecessary role announcements.
	if cfg.Label == "" && cfg.Orientation == DividerHorizontal {
		return dividerStyle.WrapHTML(render.Tag("hr", map[string]string{
			"class": cls,
			"id":    cfg.ID,
		}))
	}

	attrs := map[string]string{
		"class":         cls,
		"role":          "separator",
		"aria-orientation": string(orientationOrHorizontal(cfg.Orientation)),
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Label == "" {
		return dividerStyle.WrapHTML(render.Tag("div", attrs))
	}
	return dividerStyle.WrapHTML(render.Tag("div", attrs,
		html.Span(html.TextConfig{Class: "ui-divider__label"}, render.Text(cfg.Label)),
	))
}

func orientationOrHorizontal(o DividerOrientation) DividerOrientation {
	if o == "" {
		return "horizontal"
	}
	return o
}
