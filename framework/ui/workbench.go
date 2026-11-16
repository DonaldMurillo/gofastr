package ui

import (
	"maps"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// WorkbenchConfig configures a Workbench.
type WorkbenchConfig struct {
	// RailWidth is WorkbenchRailNarrow, the default or WorkbenchRailWide; any other value panics.
	RailWidth WorkbenchRailWidth
	// Rail is the left column; it scrolls independently of the pane.
	Rail render.HTML
	// Pane is the right column; it fills the rest in both axes, an <iframe> edge to edge.
	Pane render.HTML

	ID    string
	Class string
	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// WorkbenchRailWidth is one of the named rail widths.
type WorkbenchRailWidth string

const (
	// WorkbenchRailDefault is the 320px rail.
	WorkbenchRailDefault WorkbenchRailWidth = ""
	// WorkbenchRailNarrow is the 240px rail.
	WorkbenchRailNarrow WorkbenchRailWidth = "narrow"
	// WorkbenchRailWide is the 480px rail.
	WorkbenchRailWide WorkbenchRailWidth = "wide"
)

// Workbench renders the two-pane inspector shell.
func Workbench(cfg WorkbenchConfig) render.HTML {
	cls := "fui-workbench"
	switch cfg.RailWidth {
	case WorkbenchRailDefault:
	case WorkbenchRailNarrow, WorkbenchRailWide:
		cls += " fui-workbench--rail-" + string(cfg.RailWidth)
	default:
		panic("ui.Workbench: unknown RailWidth " + strconv.Quote(string(cfg.RailWidth)) + "; use WorkbenchRailNarrow, WorkbenchRailWide or the default")
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "style"))
	railAttrs := html.Attrs{"class": "fui-workbench__rail"}
	if cfg.Rail == "" {
		railAttrs["data-cui-internal"] = ""
	}
	paneAttrs := html.Attrs{"class": "fui-workbench__pane"}
	if cfg.Pane == "" {
		paneAttrs["data-cui-internal"] = ""
	}
	return workbenchStyle.WrapHTML(render.Tag("div", attrs,
		render.Tag("div", railAttrs, cfg.Rail),
		render.Tag("div", paneAttrs, cfg.Pane),
	))
}

var workbenchStyle = registry.RegisterStyle("ui-workbench", workbenchCSS)

func workbenchCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-workbench"] {
  display: flex;
  align-items: stretch;
  block-size: 100dvh;
  inline-size: 100%;
  overflow: hidden;
  box-sizing: border-box;
}

[data-cui-comp="ui-workbench"] .fui-workbench__rail {
  flex: 0 0 auto;
  inline-size: var(--ui-workbench-rail, 320px);
  min-inline-size: 0;
  /* The rail scrolls, the page does not. Without this the whole document
     grows to the length of the control list and the pane scrolls away. */
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  --ui-sticky-top: 0px;
  padding: var(--spacing-md, 8px);
  box-sizing: border-box;
  background-color: var(--color-surface, #fff);
  border-inline-end: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
}

/* Named rail widths. The modifier sets the --ui-workbench-rail knob from
   its own token, so a theme can move one width without an inline style. */
[data-cui-comp="ui-workbench"].fui-workbench--rail-narrow {
  --ui-workbench-rail: var(--ui-workbench-rail-narrow, 240px);
}
[data-cui-comp="ui-workbench"].fui-workbench--rail-wide {
  --ui-workbench-rail: var(--ui-workbench-rail-wide, 480px);
}

[data-cui-comp="ui-workbench"] .fui-workbench__pane {
  flex: 1 1 auto;
  min-inline-size: 0;
  block-size: 100%;
  overflow: hidden;
  background-color: var(--color-background, #FFFFFF);
}

/* An iframe is the pane's motivating occupant and defaults to a small bordered
   box, so fill it here rather than making every caller remember. */
[data-cui-comp="ui-workbench"] .fui-workbench__pane > iframe {
  display: block;
  inline-size: 100%;
  block-size: 100%;
  border: 0;
}

/* Below the split point the rail sits above the pane and the page scrolls
   normally — a 320px rail beside anything is unusable on a phone. */
@media (max-width: 720px) {
  [data-cui-comp="ui-workbench"] {
    display: block;
    block-size: auto;
    overflow: visible;
  }
  [data-cui-comp="ui-workbench"] .fui-workbench__rail {
    inline-size: 100%;
    overflow-y: visible;
    border-inline-end: none;
    border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
  }
  [data-cui-comp="ui-workbench"] .fui-workbench__pane {
    block-size: 70vh;
  }
}`
}
