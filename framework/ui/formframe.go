package ui

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── FormFrame ─────────────────────────────────────────────────────
//
// The record-form frame: a wide main column beside a narrow side
// column, stacking when the form's own width is narrow. A record's
// fields read top to bottom in one place — the bulk in Main, the
// short controllers (status, owner, dates) in Side — and the same
// form draws side by side on a full page and stacked in a drawer on a
// wide screen, because the switch keys off the frame's own width, not
// the viewport's. That is the piece no existing component owns:
// ContentRow is the page frame (nav and aside landmarks around a
// main region) and does not fit inside a form; Grid is a peer grid
// with no narrow rail; Workbench is a viewport-height inspector.
//
// The frame owns only the split. It is a layout fact with no
// accessibility contract: plain divs, no landmark, no region name —
// the fields inside arrive with their own labels, and a name here
// would be read before each one. Put it inside the form (ui.Form or
// the record screen's own <form>) and bring FormFields, sections and
// rows in each column:
//
//	ui.Form(ui.FormConfig{Action: "/customers/42", ID: "customer"},
//		ui.FormFrame(ui.FormFrameConfig{
//			Main: []render.HTML{numberField, amountField, datesSection},
//			Side: []render.HTML{statusField, customerField},
//		}))
//
// A form's default measure caps well below the split point (ui.Form
// at --ui-form-max), so the frame stacks until the host widens the
// form — the one-column shape stays the default, and a record page
// opts into the rail.

// FormFrameConfig configures a FormFrame.
type FormFrameConfig struct {
	// Main is the wide column's content: the record's bulk. When empty
	// the column is not rendered and Side takes the full width.
	Main []render.HTML

	// Side is the narrow column's content: short controllers and
	// metadata. When empty the column is not rendered, the
	// two-column modifier is not armed, and Main takes the full
	// width.
	Side []render.HTML

	// SideWidth overrides the side column's inline size. One plain
	// CSS length (number + unit, e.g. "18rem", or a var(--token)
	// reference); anything else is dropped and the CSS default
	// applies. Defaults to 16rem, which fits a labelled select
	// comfortably.
	SideWidth string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-cui-*, and style (SideWidth owns the inline custom
	// property).
	ExtraAttrs html.Attrs
}

// FormFrame renders the two-column record-form frame. The frame is
// its own query container and switches on its own inline size, so a
// drawer-width form stacks and a page-width form sits side by side on
// the same viewport.
func FormFrame(cfg FormFrameConfig) render.HTML {
	cls := "fui-form-frame"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if w := cssLengthOr(cfg.SideWidth, ""); w != "" {
		// The rail width as a scoped custom property rather than an
		// inline width, so the CSS keeps ownership of how the value is
		// used and a strict-CSP host still gets no inline style
		// attribute it has to allow. One plain CSS length only
		// (cssLengthOr): a declaration list in request-derived config
		// would otherwise ship verbatim as live page CSS. See
		// core-ui/check/noinlinescripts.go.
		attrs["style"] = "--ui-form-frame-side: " + w
	}
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "style"))

	withSide := len(cfg.Side) > 0
	colsCls := "fui-form-frame__columns"
	if withSide {
		colsCls += " fui-form-frame--with-side"
	}
	var cols []render.HTML
	if len(cfg.Main) > 0 {
		cols = append(cols, render.Tag("div", html.Attrs{"class": "fui-form-frame__main"}, cfg.Main...))
	}
	if withSide {
		cols = append(cols, render.Tag("div", html.Attrs{"class": "fui-form-frame__side"}, cfg.Side...))
	}
	// The columns wrapper carries nothing when both columns are empty:
	// a frame with no columns is an empty container, and an empty
	// wrapper is structure nobody reaches (the marking gate's rule).
	var inner []render.HTML
	if len(cols) > 0 {
		inner = append(inner, render.Tag("div", html.Attrs{"class": colsCls}, cols...))
	}
	return formFrameStyle.WrapHTML(render.Tag("div", attrs, inner...))
}

var formFrameStyle = registry.RegisterStyle("ui-form-frame", formFrameCSS)

func formFrameCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-form-frame"] {
  /* The frame switches on its OWN width, never the viewport's: a form
     in a drawer on a wide screen stacks, the same form on a full page
     sits side by side. The query container is the root; the grid it
     switches lives on the child, because a container query styles
     descendants of the container, never the container itself. */
  container-type: inline-size;
  /* Containment gives the frame no width of its own, so as a flex or
     grid item with an auto basis it collapsed to 0px. It fills its
     row. */
  inline-size: 100%;
}
[data-cui-comp="ui-form-frame"] .fui-form-frame__columns {
  display: grid;
  /* One column is the default: a narrow frame, and any frame whose
     side column never arrived, is a single stack. */
  grid-template-columns: minmax(0, 1fr);
  gap: var(--spacing-lg, 16px);
  /* Columns start at their own top edge: a tall main column does not
     stretch a short side rail to match, and vice versa. */
  align-items: start;
}
[data-cui-comp="ui-form-frame"] .fui-form-frame__main,
[data-cui-comp="ui-form-frame"] .fui-form-frame__side {
  display: grid;
  gap: var(--spacing-lg, 16px);
  /* Fields can be wider than their column (a code block, a long
     select); min 0 lets the column shrink instead of overflowing the
     frame. */
  min-inline-size: 0;
}
/* A form 48rem wide holds a usable main column (28rem and up for
   field rows) beside the 16rem rail with room for the gap; below it
   the side rail drops under the main column in source order. The
   threshold is a literal because a container query condition cannot
   read a custom property — the same posture as the carousel's and
   the filter toolbar's thresholds. */
@container (min-width: 48rem) {
  [data-cui-comp="ui-form-frame"] .fui-form-frame__columns.fui-form-frame--with-side {
    grid-template-columns: minmax(0, 1fr) var(--ui-form-frame-side, 16rem);
  }
}`
}
