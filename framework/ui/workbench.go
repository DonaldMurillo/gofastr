package ui

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Workbench ──────────────────────────────────────────────────────
//
// A viewport-height inspector shell: a fixed-width rail that scrolls on its
// own, beside a content pane that fills whatever is left.
//
// It exists because "controls on the left, the thing you are editing on the
// right" had no home in the design system, and the tool that needed it,
// `gofastr theme edit`, grew ~25 bespoke classes and ~21 hardcoded hex values
// standing in for one. Deleting those without adding this produced a rail with
// no scroll (a 2300px page) beside a preview iframe collapsed to its ~300x150
// default box. Both are the same missing piece.
//
// Workbench is the shell only. Put Stack/Collapsible/FormField inside the rail
// and whatever you are inspecting in the pane; the shell owns nothing but the
// split, the scroll and the fill.

// WorkbenchConfig configures a Workbench.
type WorkbenchConfig struct {
	// RailWidth overrides the rail's fixed inline size (a CSS length).
	// Defaults to 320px, which fits a label above a control comfortably.
	RailWidth string
	// Rail is the left column. It scrolls independently of the pane, so a
	// long control list never pushes the pane off screen.
	Rail render.HTML
	// Pane is the right column. It fills the remaining space in both axes:
	// an <iframe> placed directly inside fills it edge to edge, which is the
	// case that motivated the component.
	Pane render.HTML

	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// Workbench renders the two-pane inspector shell.
func Workbench(cfg WorkbenchConfig) render.HTML {
	cls := "ui-workbench"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.RailWidth != "" {
		// Set the rail width as a scoped custom property rather than an
		// inline width, so the CSS keeps ownership of how the value is used
		// and a strict-CSP host still gets no inline style attribute it has
		// to allow. See core-ui/check/noinlinescripts.go.
		attrs["style"] = "--ui-workbench-rail: " + cfg.RailWidth
	}
	maps.Copy(attrs, cfg.ExtraAttrs)
	return workbenchStyle.WrapHTML(render.Tag("div", attrs,
		render.Tag("div", html.Attrs{"class": "ui-workbench__rail"}, cfg.Rail),
		render.Tag("div", html.Attrs{"class": "ui-workbench__pane"}, cfg.Pane),
	))
}

var workbenchStyle = registry.RegisterStyle("ui-workbench", workbenchCSS)

func workbenchCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-workbench"] {
  display: flex;
  align-items: stretch;
  block-size: 100dvh;
  inline-size: 100%;
  overflow: hidden;
  box-sizing: border-box;
}

[data-fui-comp="ui-workbench"] .ui-workbench__rail {
  flex: 0 0 auto;
  inline-size: var(--ui-workbench-rail, 320px);
  min-inline-size: 0;
  /* The rail scrolls, the page does not. Without this the whole document
     grows to the length of the control list and the pane scrolls away. */
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  padding: var(--spacing-md, 16px);
  box-sizing: border-box;
  background-color: var(--color-surface, #fff);
  border-inline-end: 1px solid var(--color-border, #e4e4e7);
}

[data-fui-comp="ui-workbench"] .ui-workbench__pane {
  flex: 1 1 auto;
  min-inline-size: 0;
  block-size: 100%;
  overflow: hidden;
  background-color: var(--color-background, #fafaf9);
}

/* An iframe is the pane's motivating occupant and defaults to a small bordered
   box, so fill it here rather than making every caller remember. */
[data-fui-comp="ui-workbench"] .ui-workbench__pane > iframe {
  display: block;
  inline-size: 100%;
  block-size: 100%;
  border: 0;
}

/* Below the split point the rail sits above the pane and the page scrolls
   normally — a 320px rail beside anything is unusable on a phone. */
@media (max-width: 720px) {
  [data-fui-comp="ui-workbench"] {
    display: block;
    block-size: auto;
    overflow: visible;
  }
  [data-fui-comp="ui-workbench"] .ui-workbench__rail {
    inline-size: 100%;
    overflow-y: visible;
    border-inline-end: none;
    border-block-end: 1px solid var(--color-border, #e4e4e7);
  }
  [data-fui-comp="ui-workbench"] .ui-workbench__pane {
    block-size: 70vh;
  }
}`
}
