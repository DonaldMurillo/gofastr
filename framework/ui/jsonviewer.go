package ui

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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
}

// JSONViewer renders a collapsible tree view of any Go value.
func JSONViewer(cfg JSONViewerConfig) render.HTML {
	raw, err := json.Marshal(cfg.Value)
	if err != nil {
		panic("ui: JSONViewer cannot marshal Value: " + err.Error())
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		panic("ui: JSONViewer cannot re-parse marshalled JSON: " + err.Error())
	}

	cls := "ui-json-viewer"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	body := jsonRender(parsed, 0, cfg)
	return jsonViewerStyle.WrapHTML(render.Tag("div", attrs, render.HTML(body)))
}

func jsonRender(v any, depth int, cfg JSONViewerConfig) string {
	switch t := v.(type) {
	case nil:
		return `<span class="ui-json-viewer__null">null</span>`
	case bool:
		if t {
			return `<span class="ui-json-viewer__bool">true</span>`
		}
		return `<span class="ui-json-viewer__bool">false</span>`
	case float64:
		// JSON numbers come back as float64.
		s := strconv.FormatFloat(t, 'f', -1, 64)
		return `<span class="ui-json-viewer__num">` + escapeXML(s) + `</span>`
	case string:
		s := t
		if cfg.MaxStringLen > 0 && len(s) > cfg.MaxStringLen {
			s = s[:cfg.MaxStringLen] + "…"
		}
		return `<span class="ui-json-viewer__str">"` + escapeXML(s) + `"</span>`
	case []any:
		return jsonRenderArray(t, depth, cfg)
	case map[string]any:
		return jsonRenderObject(t, depth, cfg)
	}
	return ""
}

func jsonRenderArray(arr []any, depth int, cfg JSONViewerConfig) string {
	if len(arr) == 0 {
		return `<span class="ui-json-viewer__empty">[]</span>`
	}
	open := ""
	if cfg.OpenDepth < 0 || depth <= cfg.OpenDepth {
		open = " open"
	}
	var sb strings.Builder
	sb.WriteString(`<details class="ui-json-viewer__node"`)
	sb.WriteString(open)
	sb.WriteString(`><summary class="ui-json-viewer__summary"><span class="ui-json-viewer__type">Array</span><span class="ui-json-viewer__count">(`)
	sb.WriteString(strconv.Itoa(len(arr)))
	sb.WriteString(`)</span></summary><ol class="ui-json-viewer__list">`)
	for i, item := range arr {
		sb.WriteString(`<li class="ui-json-viewer__item"><span class="ui-json-viewer__key">`)
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(`</span><span class="ui-json-viewer__colon">:</span>`)
		sb.WriteString(jsonRender(item, depth+1, cfg))
		sb.WriteString(`</li>`)
	}
	sb.WriteString(`</ol></details>`)
	return sb.String()
}

func jsonRenderObject(obj map[string]any, depth int, cfg JSONViewerConfig) string {
	if len(obj) == 0 {
		return `<span class="ui-json-viewer__empty">{}</span>`
	}
	open := ""
	if cfg.OpenDepth < 0 || depth <= cfg.OpenDepth {
		open = " open"
	}
	// Sorted keys for deterministic render.
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(`<details class="ui-json-viewer__node"`)
	sb.WriteString(open)
	sb.WriteString(`><summary class="ui-json-viewer__summary"><span class="ui-json-viewer__type">Object</span><span class="ui-json-viewer__count">(`)
	sb.WriteString(strconv.Itoa(len(obj)))
	sb.WriteString(`)</span></summary><ul class="ui-json-viewer__list">`)
	for _, k := range keys {
		sb.WriteString(`<li class="ui-json-viewer__item"><span class="ui-json-viewer__key">"`)
		sb.WriteString(escapeXML(k))
		sb.WriteString(`"</span><span class="ui-json-viewer__colon">:</span>`)
		sb.WriteString(jsonRender(obj[k], depth+1, cfg))
		sb.WriteString(`</li>`)
	}
	sb.WriteString(`</ul></details>`)
	return sb.String()
}

var jsonViewerStyle = registry.RegisterStyle("ui-json-viewer", jsonViewerCSS)

func jsonViewerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-json-viewer"] {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.85rem;
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
  gap: 4px;
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
  padding-inline-start: var(--spacing-lg, 24px);
  list-style: none;
  border-inline-start: 1px dashed var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__item {
  padding-block: 2px;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__key {
  color: var(--color-info, #3B82F6);
  font-weight: 600;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__colon {
  color: var(--color-text-muted, #52525B);
  margin-inline-end: 4px;
}
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__str { color: var(--color-success, #16A34A); }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__num { color: var(--color-warning, #D97706); }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__bool { color: var(--color-primary, #4F46E5); font-weight: 600; }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__null { color: var(--color-text-muted, #52525B); font-style: italic; }
[data-fui-comp="ui-json-viewer"] .ui-json-viewer__empty { color: var(--color-text-muted, #52525B); }`
}
