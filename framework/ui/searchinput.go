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

//go:embed searchinput.js
var searchInputJS string

var _ = registry.RegisterBehavior("searchinput", searchInputJS, registry.Markers(`[data-cui-comp="ui-search-input"]`))

// SearchInputConfig configures a SearchInput.
type SearchInputConfig struct {
	// Name is the input's form name. Required.
	Name string
	// ID is the input's id. Required.
	ID string
	// Placeholder defaults to "Search...".
	Placeholder string
	// Action, when set, wraps the field in a <form role="search">.
	Action string
	// Method is "GET" (default) or "POST".
	Method string
	// Class adds classes to the wrapper.
	Class string
	// ExtraAttrs land on the input; class, id, data-cui-*, type and name are dropped, value is kept.
	ExtraAttrs map[string]string
	// Ctx resolves the default words. Nil uses English.
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
		placeholder = i18nui.SearchInputPlaceholder.Text(ctx)
	}
	method := cfg.Method
	if method == "" {
		method = "GET"
	}
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
		"aria-label":  i18nui.SearchLabel.Text(ctx),
	}
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
			"aria-label": i18nui.SearchClear.Text(ctx),
			"hidden":     "",
		}, Icon("close", IconConfig{Size: "16"})),
	}

	labelAttrs := map[string]string{"class": cls, "for": cfg.ID}
	if cfg.Action != "" {
		labelAttrs["data-cui-internal"] = ""
	}
	innerWrapper := render.Tag("label", labelAttrs, inner...)
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

func searchInputCSS(_ style.Theme) string {
	return `.fui-search {
  display: inline-flex;
  align-items: stretch;
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
  /* The frame is the control: its height counts the border, so the
     field lines up with the buttons beside it. The input and the clear
     button stretch to fill it. */
  box-sizing: border-box;
  min-block-size: var(--fui-density-control-h, var(--spacing-touch-target, 44px));
}
.fui-search .fui-search__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding-block: 0;
  padding-inline: calc(var(--spacing-sm, 4px) * 3) var(--spacing-sm, 4px);
  color: var(--color-text-subtle, #71717A);
  user-select: none;
}
.fui-search .fui-search__input {
  flex: 1;
  /* An input's own minimum is about 20 characters: without this a
     narrow field pushes the clear button out of its frame. */
  min-inline-size: 0;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  padding: var(--ui-control-padding-y, 10px) var(--spacing-xs, 2px);
  color: var(--color-text, #09090B);
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
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
}
.fui-search .fui-search__clear:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
}
.fui-search .fui-search__clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: var(--ui-search-clear-min-width, 2rem);
  background: transparent;
  border: 0;
  color: var(--color-text-subtle, #71717A);
  cursor: pointer;
  user-select: none;
  padding: 0 var(--spacing-sm, 4px);
}
.fui-search .fui-search__clear:hover {
  color: var(--color-text, #09090B);
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
