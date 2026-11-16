package ui

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FormFrameConfig configures a FormFrame.
type FormFrameConfig struct {
	// Main is the wide column's content; empty drops the column.
	Main []render.HTML

	// Side is the narrow column's content; empty drops the column.
	Side []render.HTML

	// SideWidth names the side column's width; an unknown name panics.
	SideWidth FormFrameSideWidth

	// SidePanel draws the side column as a panel that sticks beside the fields.
	SidePanel bool

	ID    string
	Class string
	// ExtraAttrs lands on the root; style is dropped.
	ExtraAttrs html.Attrs
}

// FormFrameSideWidth is one of the named side-rail widths.
type FormFrameSideWidth string

const (
	// FormFrameSideDefault is the 16rem rail.
	FormFrameSideDefault FormFrameSideWidth = ""
	// FormFrameSideNarrow is the 12rem rail.
	FormFrameSideNarrow FormFrameSideWidth = "narrow"
	// FormFrameSideWide is the 22rem rail.
	FormFrameSideWide FormFrameSideWidth = "wide"
)

// FormFrame renders a record form's main and side columns, stacked when narrow.
func FormFrame(cfg FormFrameConfig) render.HTML {
	cls := "fui-form-frame"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	switch cfg.SideWidth {
	case FormFrameSideDefault, FormFrameSideNarrow, FormFrameSideWide:
		if cfg.SideWidth != FormFrameSideDefault {
			cls += " fui-form-frame--side-" + string(cfg.SideWidth)
		}
	default:
		panic("ui: FormFrame unknown SideWidth " + string(cfg.SideWidth) +
			`. Pick one of: "" (default), narrow, wide`)
	}
	if cfg.SidePanel {
		cls += " fui-form-frame--side-panel"
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "style"))

	withSide := len(cfg.Side) > 0
	colsCls := "fui-form-frame__columns"
	if withSide && len(cfg.Main) > 0 {
		colsCls += " fui-form-frame--with-side"
	}
	var cols []render.HTML
	if len(cfg.Main) > 0 {
		cols = append(cols, render.Tag("div", html.Attrs{"class": "fui-form-frame__main"}, cfg.Main...))
	}
	if withSide {
		cols = append(cols, render.Tag("div", html.Attrs{"class": "fui-form-frame__side"}, cfg.Side...))
	}
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
/* Fields read best at a line length an eye can track: past ~45rem a
   text input is mostly empty box. The main column stops there and the
   rest of its track is air before the rail. */
[data-cui-comp="ui-form-frame"] .fui-form-frame__main {
  max-inline-size: var(--ui-form-frame-main-max, 45rem);
}
/* The panel rail: the record's facts on their own surface. */
[data-cui-comp="ui-form-frame"].fui-form-frame--side-panel .fui-form-frame__side {
  padding: var(--spacing-lg, 16px);
  background: var(--color-surface);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-lg, 10px);
}
/* Named rail widths. The modifier sets the --ui-form-frame-side knob
   on the frame's root, where it outranks a value any ancestor
   declared; each step reads its own token so a theme or page retunes
   one width (--ui-form-frame-side-narrow / -wide) without touching the
   others. The default rail stays 16rem through the knob's fallback in
   the @container rule below. */
[data-cui-comp="ui-form-frame"].fui-form-frame--side-narrow {
  --ui-form-frame-side: var(--ui-form-frame-side-narrow, 12rem);
}
[data-cui-comp="ui-form-frame"].fui-form-frame--side-wide {
  --ui-form-frame-side: var(--ui-form-frame-side-wide, 22rem);
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
  /* Beside the fields the panel stays in view while they scroll. */
  [data-cui-comp="ui-form-frame"].fui-form-frame--side-panel .fui-form-frame__side {
    position: sticky;
    /* Under a sticky toolbar (ContentRow Sticky publishes its height
       as --ui-sticky-top), not behind it. */
    inset-block-start: calc(var(--ui-sticky-top, 0px) + var(--spacing-lg, 16px));
  }
}`
}
