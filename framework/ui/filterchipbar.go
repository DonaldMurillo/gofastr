package ui

import (
	"cmp"
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// FilterChip is one active filter.
type FilterChip struct {
	// Label is the chip text. Required.
	Label string
	// DismissPath removes the filter: POSTed with script, linked
	// without. Required.
	DismissPath string
	// DismissBody is an optional static JSON body for the POST.
	DismissBody string
	Variant     StatusVariant
}

// FilterChipBarConfig configures the bar.
type FilterChipBarConfig struct {
	// Filters are the active filters; empty renders an empty group.
	Filters []FilterChip
	// ClearAllPath draws a "Clear all" link POSTing here when a filter
	// is active.
	ClearAllPath  string
	ClearAllLabel string
	// Label names the group. Default "Active filters".
	Label string
	// RPCSignal is the target for dismiss and Clear all. It defaults to
	// SignalName, or "filter-bar" if that is empty.
	RPCSignal string
	// SignalName makes the bar itself the html-mode signal region.
	SignalName string
	// Ctx resolves the bar's strings; nil uses English.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs land on the root; class, id, data-cui-*, role and
	// aria-label are the component's.
	ExtraAttrs html.Attrs
}

// FilterChipBar renders a labelled group of removable filter chips and
// an optional Clear all action.
func FilterChipBar(cfg FilterChipBarConfig) render.HTML {
	if cfg.ClearAllPath != "" {
		checkRPCPath("FilterChipBar", "ClearAllPath", cfg.ClearAllPath)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.FilterChipBarLabel.Text(ctx)
	}
	clearLabel := cfg.ClearAllLabel
	if clearLabel == "" {
		clearLabel = i18nui.FilterClearAll.Text(ctx)
	}

	cls := "fui-filter-bar"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	wrapAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label")
	if wrapAttrs == nil {
		wrapAttrs = html.Attrs{}
	}
	wrapAttrs["class"] = cls
	wrapAttrs["role"] = "group"
	wrapAttrs["aria-label"] = label
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	if cfg.SignalName != "" {
		wrapAttrs["data-cui-signal"] = cfg.SignalName
		wrapAttrs["data-cui-signal-mode"] = "html"
	}

	signal := cmp.Or(cfg.RPCSignal, cfg.SignalName, "filter-bar")
	items := make([]render.HTML, 0, len(cfg.Filters)+1)
	for _, f := range cfg.Filters {
		if f.Label == "" {
			panic("ui: FilterChip requires Label")
		}
		if f.DismissPath == "" {
			panic("ui: FilterChip requires DismissPath")
		}
		items = append(items, headless.Own(Tag(TagConfig{
			Ctx:           ctx,
			Label:         f.Label,
			Variant:       f.Variant,
			Dismiss:       f.DismissPath,
			DismissLabel:  i18nui.FilterChipRemove.Text(ctx, map[string]any{"label": f.Label}),
			Island:        headless.Island{Endpoint: f.DismissPath, Signal: signal},
			DismissMethod: "POST",
			DismissBody:   f.DismissBody,
		})))
	}

	if cfg.ClearAllPath != "" && len(cfg.Filters) > 0 {
		items = append(items, render.Tag("a", map[string]string{
			"href":                cfg.ClearAllPath,
			"class":               "fui-filter-bar__clear",
			"aria-label":          clearLabel,
			"data-cui-rpc":        cfg.ClearAllPath,
			"data-cui-rpc-method": "POST",
			"data-cui-rpc-signal": signal,
			"data-cui-internal":   "",
		}, render.Text(clearLabel)))
	}

	return filterChipBarStyle.WrapHTML(render.Tag("div", flattenAttrs(wrapAttrs), items...))
}

var filterChipBarStyle = registry.RegisterStyle("ui-filter-bar", filterChipBarCSS)

func filterChipBarCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-filter-bar"] {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) 0;
}
[data-cui-comp="ui-filter-bar"]:empty { display: none; }
[data-cui-comp="ui-filter-bar"] .fui-filter-bar__clear {
  display: inline-flex;
  align-items: center;
  min-height: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 8px);
  margin-inline-start: var(--spacing-sm, 4px);
  /* A ghost button: the chips carry the state, Clear all stays quiet. */
  border: 0;
  border-radius: var(--radii-md, 8px);
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  text-decoration: none;
  cursor: pointer;
}
[data-cui-comp="ui-filter-bar"] .fui-filter-bar__clear:hover {
  color: var(--color-text, #09090B);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-filter-bar"] .fui-filter-bar__clear:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
`
}
