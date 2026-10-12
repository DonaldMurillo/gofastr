package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── StatStrip ──────────────────────────────────────────────────────
//
// One frame around a row of figures, divided by hairlines: a
// dashboard's headline numbers read as one band, not a row of loose
// cards. Each cell is the caller's markup (a poll region holding a
// StatCard with Plain set); the strip owns the frame, the columns and
// the dividers. Two columns on a narrow screen.

// StatStripConfig configures a StatStrip.
type StatStripConfig struct {
	// Cells are the figures, one to six.
	Cells []render.HTML
	// Label names the group for assistive tech.
	Label string
	ID    string
	Class string
	// ExtraAttrs land on the root; class, id, role and aria-label are
	// the component's.
	ExtraAttrs html.Attrs
}

// StatStrip renders the figures in one frame.
func StatStrip(cfg StatStripConfig) render.HTML {
	if len(cfg.Cells) < 1 || len(cfg.Cells) > 6 {
		panic("ui: StatStrip requires 1-6 Cells")
	}
	cells := make([]render.HTML, len(cfg.Cells))
	for i, c := range cfg.Cells {
		cells[i] = html.Div(html.DivConfig{Class: "fui-stat-strip__cell", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, c)
	}
	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["class"] = cls("fui-stat-strip fui-stat-strip--"+strconv.Itoa(len(cfg.Cells)), cfg.Class)
	attrs["role"] = "group"
	if cfg.Label != "" {
		attrs["aria-label"] = cfg.Label
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return statStripStyle.WrapHTML(render.Tag("div", attrs, cells...))
}

var statStripStyle = registry.RegisterStyle("ui-stat-strip", statStripCSS)

// statStripCSS frames the cells and divides them: each cell draws the
// hairline on its start edge (and, wrapped, on its top), the first in
// each row draws none.
func statStripCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-stat-strip"]).fui-stat-strip {
  display: grid;
  grid-template-columns: repeat(var(--ui-stat-strip-columns, 1), minmax(0, 1fr));
  overflow: hidden;
  background: var(--color-surface);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-xl, 14px);
  box-shadow: var(--shadow-xs);
}
[data-cui-comp="ui-stat-strip"].fui-stat-strip--2 { --ui-stat-strip-columns: 2; }
[data-cui-comp="ui-stat-strip"].fui-stat-strip--3 { --ui-stat-strip-columns: 3; }
[data-cui-comp="ui-stat-strip"].fui-stat-strip--4 { --ui-stat-strip-columns: 4; }
[data-cui-comp="ui-stat-strip"].fui-stat-strip--5 { --ui-stat-strip-columns: 5; }
[data-cui-comp="ui-stat-strip"].fui-stat-strip--6 { --ui-stat-strip-columns: 6; }
[data-cui-comp="ui-stat-strip"] > .fui-stat-strip__cell {
  min-inline-size: 0;
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border);
}
[data-cui-comp="ui-stat-strip"] > .fui-stat-strip__cell:first-child {
  border-inline-start: 0;
}
@media (max-width: 45rem) {
  [data-cui-comp="ui-stat-strip"].fui-stat-strip { --ui-stat-strip-columns: 2; }
  [data-cui-comp="ui-stat-strip"].fui-stat-strip--1 { --ui-stat-strip-columns: 1; }
  [data-cui-comp="ui-stat-strip"] > .fui-stat-strip__cell:nth-child(odd) { border-inline-start: 0; }
  [data-cui-comp="ui-stat-strip"] > .fui-stat-strip__cell:nth-child(n+3) {
    border-block-start: var(--stroke-thin, 1px) solid var(--color-border);
  }
  /* An odd last figure spans the row, not half of it. */
  [data-cui-comp="ui-stat-strip"] > .fui-stat-strip__cell:last-child:nth-child(odd) { grid-column: 1 / -1; }
}`
}
