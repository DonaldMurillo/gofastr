package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── JSONViewer ─────────────────────────────────────────────────────
//
// Collapsible tree renderer for arbitrary Go values. Pure render
// (uses native <details>/<summary> for collapse), no JS. Good for
// admin / debug surfaces.
//
// Render flow: marshal the value to JSON to walk it generically, then
// re-walk the parsed tree to emit nested <details> elements. Strings
// / numbers / bools render inline; objects / arrays become collapsible
// nodes whose summary line shows the type + element count.

// JSONViewerConfig configures a JSONViewer.
type JSONViewerConfig struct {
	// Value is the data to render (required).
	Value any
	// OpenDepth is the recursion depth that renders open by default.
	// 0 means just the root is open; -1 means everything is open.
	OpenDepth int
	// MaxStringLen truncates long strings with "…". 0 = no limit.
	MaxStringLen int
	ID           string
	Class        string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the viewer's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// JSONViewer renders a collapsible tree view of any Go value through
// headless.JSONTree (deterministic sorted keys, native details).
func JSONViewer(cfg JSONViewerConfig) render.HTML {
	cls := "ui-json-viewer"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	parts := headless.Parts{Attrs: headless.PartAttrs{
		headless.PartRoot:      {"class": cls},
		headless.PartControl:   {"class": "ui-json-viewer__node"},
		headless.PartTitle:     {"class": "ui-json-viewer__summary"},
		headless.PartLabel:     {"class": "ui-json-viewer__key"},
		headless.PartBody:      {"class": "ui-json-viewer__list"},
		headless.PartText:      {"class": "ui-json-viewer__item"},
		headless.PartJSONColon: {"class": "ui-json-viewer__colon"},
		headless.PartJSONType:  {"class": "ui-json-viewer__type"},
		headless.PartJSONCount: {"class": "ui-json-viewer__count"},
		headless.PartJSONStr:   {"class": "ui-json-viewer__str"},
		headless.PartJSONNum:   {"class": "ui-json-viewer__num"},
		headless.PartJSONBool:  {"class": "ui-json-viewer__bool"},
		headless.PartJSONNull:  {"class": "ui-json-viewer__null"},
		headless.PartJSONEmpty: {"class": "ui-json-viewer__empty"},
	}}
	return jsonViewerStyle.WrapHTML(headless.JSONTree(headless.JSONTreeProps{
		Value:        cfg.Value,
		OpenDepth:    cfg.OpenDepth,
		MaxStringLen: cfg.MaxStringLen,
		ID:           cfg.ID,
		ExtraAttrs:   html.SafeExtraAttrs(cfg.ExtraAttrs),
		Parts:        parts,
	}, nil))
}

var jsonViewerStyle = registry.RegisterStyle("ui-json-viewer", jsonViewerCSS)

func jsonViewerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-json-viewer"] {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.5;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__node {
  display: block;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__summary {
  cursor: pointer;
  list-style: none;
  user-select: none;
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__summary::-webkit-details-marker {
  display: none;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__summary::before {
  content: "▸";
  color: var(--color-text-muted, #52525B);
  transition: transform 100ms ease;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__node[open] > .ui-json-viewer__summary::before {
  transform: rotate(90deg);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__type {
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__count {
  color: var(--color-text-muted, #52525B);
  font-size: 0.85em;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__list {
  margin: 0;
  padding-inline-start: var(--spacing-lg, 16px);
  list-style: none;
  border-inline-start: 1px dashed var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__item {
  padding-block: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__key {
  color: var(--color-info, #3B82F6);
  font-weight: 600;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__colon {
  color: var(--color-text-muted, #52525B);
  margin-inline-end: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__str { color: var(--color-success, #16A34A); }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__num { color: var(--color-warning, #D97706); }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__bool { color: var(--color-primary, #4F46E5); font-weight: 600; }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__null { color: var(--color-text-muted, #52525B); font-style: italic; }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__empty { color: var(--color-text-muted, #52525B); }`
}
