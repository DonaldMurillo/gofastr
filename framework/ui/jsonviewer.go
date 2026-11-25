package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// JSONViewerConfig configures a JSONViewer.
type JSONViewerConfig struct {
	// Ctx localizes the type words and counts; nil uses English.
	Ctx   context.Context
	Value any
	// OpenDepth is the depth rendered open: 0 the root, -1 everything.
	OpenDepth int
	// MaxStringLen cuts longer strings on the server with "…", a bound on
	// the bytes sent; 0 is no limit.
	MaxStringLen int
	// EllipsizeStrings draws each string value on one line, cut with a
	// CSS ellipsis where it overflows; the full value stays in the page,
	// so find-in-page, selection and copy see all of it.
	EllipsizeStrings bool
	ID               string
	Class            string
	// ExtraAttrs land on the root, minus class, id and data-cui-*.
	ExtraAttrs html.Attrs
}

// JSONViewer renders any Go value as a collapsible <details> tree
// through headless.JSONTree, with keys sorted.
func JSONViewer(cfg JSONViewerConfig) render.HTML {
	cls := "fui-json-viewer"
	if cfg.EllipsizeStrings {
		cls += " fui-json-viewer--ellipsis"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	parts := headless.Parts{Attrs: headless.PartAttrs{
		headless.PartRoot:      {"class": cls},
		headless.PartControl:   {"class": "fui-json-viewer__node"},
		headless.PartTitle:     {"class": "fui-json-viewer__summary"},
		headless.PartLabel:     {"class": "fui-json-viewer__key"},
		headless.PartBody:      {"class": "fui-json-viewer__list"},
		headless.PartText:      {"class": "fui-json-viewer__item"},
		headless.PartJSONColon: {"class": "fui-json-viewer__colon"},
		headless.PartJSONType:  {"class": "fui-json-viewer__type"},
		headless.PartJSONCount: {"class": "fui-json-viewer__count"},
		headless.PartJSONStr:   {"class": "fui-json-viewer__str"},
		headless.PartJSONNum:   {"class": "fui-json-viewer__num"},
		headless.PartJSONBool:  {"class": "fui-json-viewer__bool"},
		headless.PartJSONNull:  {"class": "fui-json-viewer__null"},
		headless.PartJSONEmpty: {"class": "fui-json-viewer__empty"},
	}}
	return jsonViewerStyle.WrapHTML(headless.JSONTree(headless.JSONTreeProps{
		Value:        cfg.Value,
		OpenDepth:    cfg.OpenDepth,
		MaxStringLen: cfg.MaxStringLen,
		ID:           cfg.ID,
		ExtraAttrs:   html.SafeExtraAttrs(cfg.ExtraAttrs),
		Parts:        parts,
		Strings:      StringsFor(cfg.Ctx),
	}, nil))
}

var jsonViewerStyle = registry.RegisterStyle("ui-json-viewer", jsonViewerCSS)

func jsonViewerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-json-viewer"] {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-normal, 1.5);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__node {
  display: block;
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__summary {
  cursor: pointer;
  list-style: none;
  user-select: none;
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__summary::-webkit-details-marker {
  display: none;
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__summary::before {
  content: "▸";
  color: var(--color-text-muted, #52525B);
  transition: transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__node[open] > .fui-json-viewer__summary::before {
  transform: rotate(90deg);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__type {
  color: var(--color-text-muted, #52525B);
  font-weight: var(--font-weight-semibold);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__count {
  color: var(--color-text-muted, #52525B);
  font-size: 0.85em;
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__list {
  margin: 0;
  padding-inline-start: var(--spacing-lg, 16px);
  list-style: none;
  border-inline-start: var(--stroke-thin, 1px) dashed var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__item {
  padding-block: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__key {
  color: var(--color-info, #1D4ED8);
  font-weight: var(--font-weight-semibold);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__colon {
  color: var(--color-text-muted, #52525B);
  margin-inline-end: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__str { color: var(--color-success, #166534); overflow-wrap: anywhere; }
/* EllipsizeStrings: a row holding a string is one line, its key kept
   whole and the value cut with an ellipsis; the text stays in the DOM. */
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis .fui-json-viewer__item:has(> .fui-json-viewer__str) {
  display: flex;
  align-items: baseline;
  min-inline-size: 0;
}
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis .fui-json-viewer__item > .fui-json-viewer__key,
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis .fui-json-viewer__item > .fui-json-viewer__colon { flex: none; }
/* A root that is itself a string sits straight in the viewer, with no
   row around it: it is one block line, cut the same way. */
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis > .fui-json-viewer__str { display: block; }
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis > .fui-json-viewer__str,
[data-cui-comp="ui-json-viewer"].fui-json-viewer--ellipsis .fui-json-viewer__item > .fui-json-viewer__str {
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  overflow-wrap: normal;
}
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__num { color: var(--color-warning, #854D0E); }
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__bool { color: var(--color-primary, #18181B); font-weight: var(--font-weight-semibold); }
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__null { color: var(--color-text-muted, #52525B); font-style: italic; }
[data-cui-comp="ui-json-viewer"] .fui-json-viewer__empty { color: var(--color-text-muted, #52525B); }`
}
