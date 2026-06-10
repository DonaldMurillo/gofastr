package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Toolbar ────────────────────────────────────────────────────────
//
// role="toolbar" wrapper for a horizontal strip of action buttons,
// optionally split into groups. Native browser keyboard semantics:
// the user Tab-stops once on the toolbar, then arrows between
// buttons inside (caller handles arrow-key roving if needed; the
// default is straight Tab between buttons).

// ToolbarGroup is a logical group of buttons inside a toolbar. Groups
// are rendered side-by-side with a visual separator between.
type ToolbarGroup struct {
	// Label is the accessible group name (optional). When set the
	// group renders with role="group" + aria-label.
	Label string
	// Children are the actual button/link elements. The caller
	// decides what goes in — Button, Link, IconButton, etc.
	Children []render.HTML
}

// ToolbarConfig configures a Toolbar.
type ToolbarConfig struct {
	// Label is the accessible name for the toolbar (required —
	// becomes aria-label).
	Label string
	// Groups are rendered in order with separators between.
	Groups []ToolbarGroup
	// Align picks justify-content. Default is "start". Options:
	// "start", "center", "end", "between".
	Align      string
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// Toolbar renders a horizontal action strip with role=toolbar.
func Toolbar(cfg ToolbarConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Toolbar requires Label")
	}
	if len(cfg.Groups) == 0 {
		panic("ui: Toolbar requires ≥1 Group")
	}
	switch cfg.Align {
	case "", "start", "center", "end", "between":
	default:
		panic("ui: Toolbar unknown Align " + cfg.Align +
			` — pick one of: "" (start), start, center, end, between`)
	}
	cls := "ui-toolbar"
	if cfg.Align != "" && cfg.Align != "start" {
		cls += " ui-toolbar--" + cfg.Align
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{
		"class":      cls,
		"role":       "toolbar",
		"aria-label": cfg.Label,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	parts := make([]render.HTML, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		if len(g.Children) == 0 {
			continue
		}
		groupAttrs := map[string]string{"class": "ui-toolbar__group"}
		if g.Label != "" {
			groupAttrs["role"] = "group"
			groupAttrs["aria-label"] = g.Label
		}
		parts = append(parts, render.Tag("div", groupAttrs, g.Children...))
	}

	return toolbarStyle.WrapHTML(render.Tag("div", attrs, parts...))
}

var toolbarStyle = registry.RegisterStyle("ui-toolbar", toolbarCSS)

func toolbarCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-toolbar"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 12px);
  padding: var(--spacing-sm, 8px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  flex-wrap: wrap;
}
[data-fui-comp="ui-toolbar"].ui-toolbar--center { justify-content: center; }
[data-fui-comp="ui-toolbar"].ui-toolbar--end    { justify-content: flex-end; }
[data-fui-comp="ui-toolbar"].ui-toolbar--between { justify-content: space-between; }

[data-fui-comp="ui-toolbar"] .ui-toolbar__group {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 4px);
}
/* Visual separator between groups — a thin line. Drawn via
   :not(:last-child)::after so the LAST group has no trailing line. */
[data-fui-comp="ui-toolbar"] .ui-toolbar__group:not(:last-child)::after {
  content: "";
  display: inline-block;
  width: 1px;
  height: 20px;
  background: var(--color-border, #E4E4E7);
  margin-inline-start: var(--spacing-md, 12px);
}`
}
