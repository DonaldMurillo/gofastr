package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── FilterChipBar ──────────────────────────────────────────────────
//
// A toolbar of active filter chips above a table or search result.
// Each chip is removable via an island RPC; an optional "Clear all"
// trailing action wipes them in one shot.
//
// The chip itself is the existing ui.Tag (with Dismiss set). This
// component handles:
//   - role=toolbar wrapper with aria-label
//   - "Clear all" affordance when ClearAllPath set
//   - Empty-state collapse (renders an empty wrapper so the surface
//     can be revealed without re-rendering when filters return)
//
// All chip dismissals share the same RPCSignal so the server can
// re-render the chip bar HTML once and the runtime swaps it in.

// FilterChip is one active filter.
type FilterChip struct {
	// Label is the visible chip text. Required.
	Label string

	// DismissPath is the POST endpoint that removes this filter on
	// click of the × button. Required.
	DismissPath string

	// DismissBody is an optional static JSON body sent with the
	// dismiss request (data-fui-rpc-body). When empty, the server
	// is expected to deduce the filter from DismissPath alone.
	DismissBody string

	// Variant maps to StatusVariant — defaults to neutral. Use to
	// surface filter kind (info chips for tags, success for "active"
	// status filters, etc).
	Variant StatusVariant
}

// FilterChipBarConfig configures the bar.
type FilterChipBarConfig struct {
	// Filters is the active filter set. Empty renders an empty
	// (but valid) toolbar.
	Filters []FilterChip

	// ClearAllPath, when non-empty, renders a trailing "Clear all"
	// button that POSTs here.
	ClearAllPath string

	// ClearAllLabel overrides the trailing button's text.
	// Default "Clear all".
	ClearAllLabel string

	// Label is the aria-label on the toolbar.
	// Default "Active filters".
	Label string

	// RPCSignal, when set, is broadcast on every chip dismiss AND
	// on Clear all — so the bar swaps itself with the server's
	// re-rendered HTML.
	RPCSignal string

	// SignalName, when set, is also placed on the wrapper as
	// data-fui-signal so the runtime can swap the entire bar from
	// the RPC response. Pair with data-fui-signal-mode="html" on
	// the parent container.
	SignalName string

	// Ctx carries the per-request context used to resolve i18n labels
	// (Clear all / "Remove filter <label>"). When nil, English fallbacks.
	Ctx context.Context

	ID    string
	Class string
}

// FilterChipBar renders the toolbar.
func FilterChipBar(cfg FilterChipBarConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = "Active filters"
	}
	clearLabel := cfg.ClearAllLabel
	if clearLabel == "" {
		clearLabel = i18nui.T(ctx, i18nui.KeyFilterClearAll)
	}

	cls := "ui-filter-bar"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	wrapAttrs := html.Attrs{
		"class":      cls,
		"role":       "toolbar",
		"aria-label": label,
	}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	if cfg.SignalName != "" {
		wrapAttrs["data-fui-signal"] = cfg.SignalName
		wrapAttrs["data-fui-signal-mode"] = "html"
	}

	items := make([]render.HTML, 0, len(cfg.Filters)+1)
	for _, f := range cfg.Filters {
		if f.Label == "" {
			panic("ui: FilterChip requires Label")
		}
		if f.DismissPath == "" {
			panic("ui: FilterChip requires DismissPath")
		}
		dismissAttrs := html.Attrs{}
		if f.DismissBody != "" {
			dismissAttrs["data-fui-rpc-body"] = f.DismissBody
		}
		if cfg.RPCSignal != "" {
			dismissAttrs["data-fui-rpc-signal"] = cfg.RPCSignal
		}
		dismissAttrs["data-fui-rpc-method"] = "POST"
		items = append(items, Tag(TagConfig{
			Label:        f.Label,
			Variant:      f.Variant,
			Dismiss:      f.DismissPath,
			DismissLabel: i18nui.TVars(ctx, i18nui.KeyFilterChipRemove, map[string]string{"label": f.Label}),
			DismissAttrs: dismissAttrs,
		}))
	}

	if cfg.ClearAllPath != "" && len(cfg.Filters) > 0 {
		clearAttrs := html.Attrs{
			"type":                "button",
			"class":               "ui-filter-bar__clear",
			"data-fui-rpc":        cfg.ClearAllPath,
			"data-fui-rpc-method": "POST",
		}
		if cfg.RPCSignal != "" {
			clearAttrs["data-fui-rpc-signal"] = cfg.RPCSignal
		}
		items = append(items, render.Tag("button", flattenAttrs(clearAttrs), render.Text(clearLabel)))
	}

	return filterChipBarStyle.WrapHTML(render.Tag("div", flattenAttrs(wrapAttrs), items...))
}

var filterChipBarStyle = registry.RegisterStyle("ui-filter-bar", filterChipBarCSS)

func filterChipBarCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-filter-bar"] {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  padding: var(--spacing-sm, 8px) 0;
}
[data-fui-comp="ui-filter-bar"]:empty { display: none; }
[data-fui-comp="ui-filter-bar"] .ui-filter-bar__clear {
  display: inline-flex;
  align-items: center;
  min-height: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 12px);
  margin-inline-start: var(--spacing-sm, 8px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 6px);
  background: transparent;
  color: var(--color-text, #111);
  font: inherit;
  font-size: var(--text-sm, 0.85rem);
  cursor: pointer;
}
[data-fui-comp="ui-filter-bar"] .ui-filter-bar__clear:hover {
  background: var(--color-muted, #f1f1f3);
}
[data-fui-comp="ui-filter-bar"] .ui-filter-bar__clear:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
`
}
