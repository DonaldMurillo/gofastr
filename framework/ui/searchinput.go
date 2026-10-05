package ui

import (
	"context"
	_ "embed"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── SearchInput ────────────────────────────────────────────────────
//
// Search input with a search icon prefix and a clear button suffix.
// Optionally wraps in a <form role="search"> when Action is set.
// searchinput.js, registered below as this package's own behaviour,
// shows and hides the clear button and clears the input on click or
// Escape. It binds the component's classes, which is why it lives
// here and not in the kernel: core-ui/runtime names no kit class.

//go:embed searchinput.js
var searchInputJS string

// SearchInput has no headless counterpart by binding decision, so the
// module that drives it is framework/ui's, the filedropzone shape: the
// kernel demand-loads it when the marker is on the page, and the
// component's classes stay the component's to rename.
var _ = registry.RegisterBehavior("searchinput", searchInputJS, registry.Markers(`[data-cui-comp="ui-search-input"]`))

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
	// ExtraAttrs forwards additional attributes to the <input> element.
	// Keys the component owns are dropped: class and id, data-cui-*,
	// type, and name. "value" is deliberately NOT owned: the resource
	// UI prefills the current search term through it.
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

	cls := "fui-search"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	inputAttrs := map[string]string{
		"type":        "search",
		"name":        cfg.Name,
		"id":          cfg.ID,
		"class":       "fui-search__input",
		"placeholder": placeholder,
		"aria-label":  i18nui.T(ctx, i18nui.KeySearchLabel),
	}
	// Extras land on the <input>: SafeExtraAttrs drops every
	// case-variant of type/name (and id/class/data-cui-*), so a caller
	// cannot flip the box to a hidden input or clobber the submitted
	// field name. value survives — see the config comment.
	maps.Copy(inputAttrs, html.SafeExtraAttrs(cfg.ExtraAttrs, "type", "name"))

	inner := []render.HTML{
		html.Span(html.TextConfig{
			Class:      "fui-search__icon",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, Icon("search", IconConfig{Size: "16"})),
		render.VoidTag("input", inputAttrs),
		render.Tag("button", map[string]string{
			"type":       "button",
			"class":      "fui-search__clear",
			"aria-label": i18nui.T(ctx, i18nui.KeySearchClear),
			"hidden":     "",
		}, Icon("close", IconConfig{Size: "16"})),
	}

	// The wrapper is a <label> so the whole visual box (icon + padding, not just
	// the input itself) is a click target that focuses the input. Otherwise the
	// hit area is smaller than it looks.
	labelAttrs := map[string]string{"class": cls, "for": cfg.ID}
	// The label is this render's own root when there is no Action, and
	// the root is exempt — but wrapped in a <form> it is no longer the
	// root, and holds nothing but this component's own icon/input/clear
	// trio, so it becomes the topmost internal subtree instead.
	if cfg.Action != "" {
		labelAttrs["data-cui-internal"] = ""
	}
	innerWrapper := render.Tag("label", labelAttrs, inner...)
	// Wrap in <form role="search"> when Action is provided. The action
	// runs through the same urlsafe.CleanAnchor allow-list as ui.Form so a
	// javascript:/vbscript:/data: Action never becomes a live form action;
	// a rejected value degrades to the inert "#" ui.Form uses.
	if cfg.Action != "" {
		action := urlsafe.CleanAnchor(cfg.Action)
		if action == "" {
			action = "#"
		}
		return searchInputStyle.WrapHTML(render.Tag("form", map[string]string{
			"role":   "search",
			"action": action,
			"method": method,
			"class":  "fui-search__form",
		}, innerWrapper))
	}

	return searchInputStyle.WrapHTML(innerWrapper)
}

var searchInputStyle = registry.RegisterStyle("ui-search-input", searchInputCSS)

// searchInputCSS keys every rule on the .fui-search class, NOT the
// data-cui-comp marker: WrapHTML injects the marker into the OUTERMOST
// tag, which is the <label> in the bare variant but the <form> in the
// Action variant. Attribute-ancestor selectors therefore stop matching
// the label the moment Action is set (#239) — the class is the one
// thing the label carries in both shapes.
func searchInputCSS(_ style.Theme) string {
	return `.fui-search {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
}
.fui-search .fui-search__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding-block: 0;
  padding-inline: 12px var(--spacing-sm, 4px);
  color: var(--color-text-subtle, #71717A);
  user-select: none;
}
.fui-search .fui-search__input {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  padding: var(--ui-control-padding-y, 10px) var(--spacing-xs, 2px);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
  /* Remove native search clear button (we provide our own). */
  appearance: none;
  -webkit-appearance: none;
}
.fui-search .fui-search__input::placeholder {
  color: var(--color-text-subtle, #71717A);
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) {
  .fui-search .fui-search__input { font-size: var(--text-base, 1rem); }
}
.fui-search .fui-search__input::-webkit-search-cancel-button,
.fui-search .fui-search__input::-webkit-search-decoration {
  -webkit-appearance: none;
}
.fui-search .fui-search__input:focus,
.fui-search .fui-search__input:focus-visible {
  /* One ring, drawn on the input itself. The input fills the frame,
     so its outline lands at the frame's inner edge and reads as a
     single ring. The frame's old :focus-within outline showed nothing
     on the focused control itself, which a keyboard user scanning the
     control (and any element-local focus check) reads as no indicator. */
  outline: 2px solid var(--color-text-subtle);
  outline-offset: -2px;
}
.fui-search .fui-search__clear:focus-visible {
  outline: 2px solid var(--color-text-subtle);
  outline-offset: -2px;
}
.fui-search .fui-search__clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: 2rem;
  background: transparent;
  border: 0;
  color: var(--color-text-subtle, #71717A);
  cursor: pointer;
  user-select: none;
  padding: 0 var(--spacing-sm, 4px);
}
.fui-search .fui-search__clear:hover {
  color: var(--color-text, #18181B);
  background: var(--color-surface-soft, #F4F4F5);
}
.fui-search .fui-search__clear[hidden] {
  display: none;
}
.fui-search__form {
  display: inline-flex;
}
/* When a parent gives the form a width, the label must fill it so the
   input's flex:1 spans the box instead of shrink-wrapping. */
.fui-search__form > .fui-search {
  flex: 1 1 auto;
  min-inline-size: 0;
}`
}
