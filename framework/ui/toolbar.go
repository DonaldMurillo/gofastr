package ui

import (
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ToolbarGroup is one group of controls, separated from the next.
type ToolbarGroup struct {
	// Label, when set, makes the group a labelled role="group".
	Label string
	// Children are the group's controls.
	Children []render.HTML
}

// ToolbarConfig configures a Toolbar.
type ToolbarConfig struct {
	// Label is the toolbar's aria-label. Required.
	Label string
	// Groups render in order; a group with no children is skipped.
	Groups []ToolbarGroup
	// Align is "start" (default), "center", "end" or "between".
	Align string
	ID    string
	Class string
	// Plain removes the frame and padding when placed in existing chrome.
	Plain bool
	// ExtraAttrs land on the root; class, id, data-cui-*, role and aria-label are dropped.
	ExtraAttrs html.Attrs
}

// Toolbar renders a role="toolbar" strip of control groups.
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
			`. Pick one of: "" (start), start, center, end, between`)
	}
	cls := "fui-toolbar"
	if cfg.Plain {
		cls += " fui-toolbar--plain"
	}
	if cfg.Align != "" && cfg.Align != "start" {
		cls += " fui-toolbar--" + cfg.Align
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
	maps.Copy(attrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label"))

	parts := make([]render.HTML, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		if len(g.Children) == 0 {
			continue
		}
		groupAttrs := map[string]string{"class": "fui-toolbar__group"}
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
	return `[data-cui-comp="ui-toolbar"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-sm, 4px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  flex-wrap: wrap;
}
[data-cui-comp="ui-toolbar"].fui-toolbar--plain { padding: 0; border: 0; border-radius: 0; background: transparent; }
[data-cui-comp="ui-toolbar"].fui-toolbar--center { justify-content: center; }
[data-cui-comp="ui-toolbar"].fui-toolbar--end    { justify-content: flex-end; }
[data-cui-comp="ui-toolbar"].fui-toolbar--between { justify-content: space-between; }

[data-cui-comp="ui-toolbar"] .fui-toolbar__group {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
}
/* Visual separator between groups — a thin line. Drawn via
   :not(:last-child)::after so the LAST group has no trailing line. */
[data-cui-comp="ui-toolbar"] .fui-toolbar__group:not(:last-child)::after {
  content: "";
  display: inline-block;
  width: 1px;
  height: var(--ui-toolbar-separator-height, 20px);
  background: var(--color-border, #E4E4E7);
  margin-inline-start: var(--spacing-md, 8px);
}`
}
