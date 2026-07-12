package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── SearchInput ────────────────────────────────────────────────────
//
// Search input with a search icon prefix and a clear button suffix.
// Optionally wraps in a <form role="search"> when Action is set.
// Runtime JS (core-ui/runtime/src/searchinput.js) handles show/hide
// of the clear button and clearing the input on click.

// SearchInputConfig configures a SearchInput.
type SearchInputConfig struct {
	// Name is the form-field name (required).
	Name string
	// ID is the input element's id (required).
	ID string
	// Placeholder renders the native placeholder. Defaults to "Search...".
	Placeholder string
	// Action is an optional form action URL. When set, wraps in <form role="search">.
	Action string
	// Method is the form method. Defaults to "GET".
	Method string
	// Class adds extra CSS classes to the wrapper.
	Class string
	// Attrs lets callers attach additional attributes.
	ExtraAttrs map[string]string
	// Ctx carries the per-request context used to resolve i18n labels
	// (placeholder, aria-labels). When nil, English fallbacks apply.
	Ctx context.Context
}

// SearchInput renders a search field with icon prefix and clear button.
func SearchInput(cfg SearchInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: SearchInput requires Name")
	}
	if cfg.ID == "" {
		panic("ui: SearchInput requires ID")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = i18nui.T(ctx, i18nui.KeySearchInputPlaceholder)
	}
	method := cfg.Method
	if method == "" {
		method = "GET"
	}
	// K-1: Reject invalid methods to prevent silent HTML bugs.
	if method != "GET" && method != "POST" {
		panic("ui: SearchInput Method must be GET or POST, got " + method)
	}

	cls := "ui-search-input"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":        "search",
		"name":        cfg.Name,
		"id":          cfg.ID,
		"class":       "ui-search-input__input",
		"placeholder": placeholder,
		"aria-label":  i18nui.T(ctx, i18nui.KeySearchLabel),
	}
	for k, v := range cfg.ExtraAttrs {
		inputAttrs[k] = v
	}
	// Protect critical attrs from Attrs override.
	inputAttrs["type"] = "search"
	inputAttrs["name"] = cfg.Name
	inputAttrs["id"] = cfg.ID

	inner := []render.HTML{
		html.Span(html.TextConfig{
			Class:      "ui-search-input__icon",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text("⌕")),
		render.VoidTag("input", inputAttrs),
		render.Tag("button", map[string]string{
			"type":       "button",
			"class":      "ui-search-input__clear",
			"aria-label": i18nui.T(ctx, i18nui.KeySearchClear),
			"hidden":     "",
		}, render.Text("×")),
	}

	// The wrapper is a <label> so the whole visual box (icon + padding, not just
	// the input itself) is a click target that focuses the input — otherwise the
	// hit area is smaller than it looks.
	innerWrapper := render.Tag("label",
		map[string]string{"class": cls, "for": cfg.ID},
		inner...)

	// Wrap in <form role="search"> when Action is provided.
	if cfg.Action != "" {
		return searchInputStyle.WrapHTML(render.Tag("form", map[string]string{
			"role":   "search",
			"action": cfg.Action,
			"method": method,
			"class":  "ui-search-input__form",
		}, innerWrapper))
	}

	return searchInputStyle.WrapHTML(innerWrapper)
}

var searchInputStyle = registry.RegisterStyle("ui-search-input", searchInputCSS)

func searchInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-search-input"] {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
}
[data-fui-comp="ui-search-input"] .ui-search-input__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--spacing-sm, 4px) 0 var(--spacing-md, 12px);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-base, 1rem);
  user-select: none;
}
[data-fui-comp="ui-search-input"] .ui-search-input__input {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 0.95rem);
  padding: 10px var(--spacing-xs, 4px);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
  /* Remove native search clear button (we provide our own). */
  appearance: none;
  -webkit-appearance: none;
}
[data-fui-comp="ui-search-input"] .ui-search-input__input::-webkit-search-cancel-button,
[data-fui-comp="ui-search-input"] .ui-search-input__input::-webkit-search-decoration {
  -webkit-appearance: none;
}
[data-fui-comp="ui-search-input"] .ui-search-input__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-search-input"] .ui-search-input__clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: 2rem;
  background: transparent;
  border: 0;
  font-size: var(--text-lg, 1.1rem);
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  user-select: none;
  padding: 0 var(--spacing-sm, 4px);
}
[data-fui-comp="ui-search-input"] .ui-search-input__clear:hover {
  color: var(--color-text, #18181B);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-search-input"] .ui-search-input__clear[hidden] {
  display: none;
}
[data-fui-comp="ui-search-input"] .ui-search-input__form {
  display: inline-flex;
}`
}
