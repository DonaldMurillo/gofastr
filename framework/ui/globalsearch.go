package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── GlobalSearch ───────────────────────────────────────────────────
//
// Sticky page-level search bar: headless.Combobox dressed with the
// fui-global-search class map, a `/`-shortcut focus chord, and an
// optional hint chip. Distinct from CommandPalette (which is a
// focus-trapped ⌘K modal): GlobalSearch is inline, persistent, and
// per-page.

// GlobalSearchConfig configures a GlobalSearch.
type GlobalSearchConfig struct {
	// ID is the input element id (the listbox takes <ID>-listbox).
	// Required, page-unique.
	ID string
	// Name is the form-submit name on the input. Required.
	Name string
	// Label is the visible label text (required, used as <label for=…>;
	// visually hidden by the bar's shape).
	Label string
	// RPCPath is the search endpoint. Required. POSTed with the query
	// in `<Name>=<value>`; the listbox re-renders through the signal.
	RPCPath string
	// SignalName is the rpc-signal value used to swap the listbox HTML
	// after each search response. Required.
	SignalName string
	// NoScriptAction is the same-origin GET destination the wrapping
	// form submits to without script. Required: a reader without
	// script must still reach the results page. `#` is refused.
	NoScriptAction string
	// Placeholder for the input. Default "Search…".
	Placeholder string
	// Shortcut, when set, opts the input into runtime focus-on-key:
	// `data-hui-shortcut-focus="<chord>"` (default "/"). Pass an
	// explicit empty string to disable.
	Shortcut string
	// ShowHint renders a small "Press <chord>" hint chip on the right
	// of the input. Default true when Shortcut is set.
	ShowHint *bool
	// DebounceMs is the input debounce window. Default 200.
	DebounceMs int
	// Sticky toggles position: sticky on the wrapper. Default true.
	Sticky bool
	Class  string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the search bar's root
	// wrapper. Keys the component owns are dropped: class, the
	// data-hui-* wiring, and the shortcut chords.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve i18n labels
	// (placeholder, the combobox's sentences). When nil, English
	// fallbacks apply.
	Ctx context.Context
}

// GlobalSearch renders the search bar.
func GlobalSearch(cfg GlobalSearchConfig) render.HTML {
	if cfg.ID == "" {
		panic("ui: GlobalSearch requires ID")
	}
	if cfg.Name == "" {
		panic("ui: GlobalSearch requires Name")
	}
	if cfg.Label == "" {
		panic("ui: GlobalSearch requires Label")
	}
	if cfg.RPCPath == "" {
		panic("ui: GlobalSearch requires RPCPath")
	}
	if cfg.SignalName == "" {
		panic("ui: GlobalSearch requires SignalName")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = i18nui.T(ctx, i18nui.KeySearchPlaceholder)
	}
	shortcut := cfg.Shortcut
	if cfg.Shortcut == "" && cfg.Shortcut != " " {
		// Default to "/" unless caller explicitly passed " " to disable.
		// Use a sentinel " " to express "off" because zero-value
		// collides with "default".
		shortcut = "/"
	}
	if cfg.Shortcut == " " {
		shortcut = ""
	}
	showHint := shortcut != ""
	if cfg.ShowHint != nil {
		showHint = *cfg.ShowHint
	}
	debounceMs := cfg.DebounceMs
	if debounceMs == 0 {
		debounceMs = 200
	}

	classes := headless.Classes{
		headless.PartLabel:           "fui-visually-hidden",
		headless.PartComboboxForm:    "fui-global-search__field",
		headless.PartComboboxInput:   "fui-global-search__input",
		headless.PartComboboxListbox: "fui-global-search__listbox",
		headless.PartComboboxOption:  "fui-global-search__option",
		headless.PartComboboxStatus:  "fui-visually-hidden",
		headless.PartText:            "fui-global-search__option-label",
	}

	box := headless.Combobox(headless.ComboboxProps{
		ID:             cfg.ID,
		Name:           cfg.Name,
		Label:          cfg.Label,
		Placeholder:    placeholder,
		Island:         &headless.Island{Endpoint: cfg.RPCPath, Signal: cfg.SignalName},
		NoScriptAction: cfg.NoScriptAction,
		DebounceMS:     debounceMs,
		Strings:        StringsFor(ctx),
	}, classes)

	// The bar's wrapper owns the class, the sticky posture, the chord,
	// and the hint chip: one element the page composes, one place the
	// shortcut lives.
	cls := "fui-global-search"
	if cfg.Sticky {
		cls += " fui-global-search--sticky"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	wrapAttrs := headless.Safe(cfg.ExtraAttrs, "class")
	wrapAttrs["class"] = cls
	if shortcut != "" {
		wrapAttrs["data-hui-shortcut-focus"] = shortcut
		wrapAttrs["data-hui-shortcut-target"] = "#" + cfg.ID
	}
	children := []render.HTML{box}
	if showHint && shortcut != "" {
		children = append(children, html.Span(html.TextConfig{
			Class:      "fui-global-search__hint",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Tag("kbd", map[string]string{"class": "fui-global-search__chord"}, render.Text(shortcut))))
	}
	boxWrapped := html.Div(html.DivConfig{ExtraAttrs: wrapAttrs}, children...)
	return globalSearchStyle.WrapHTML(boxWrapped)
}

var globalSearchStyle = registry.RegisterStyle("ui-global-search", globalSearchCSS)

func globalSearchCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-global-search"].fui-global-search {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-global-search"].fui-global-search--sticky {
  position: sticky;
  inset-block-start: var(--spacing-md, 8px);
  z-index: var(--z-sticky, 50);
}
[data-fui-comp="ui-global-search"] .fui-global-search__field {
  flex: 1;
}
[data-fui-comp="ui-global-search"] .fui-global-search__input {
  width: 100%;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  font: inherit;
  min-height: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-global-search"] .fui-global-search__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
[data-fui-comp="ui-global-search"] .fui-global-search__listbox {
  margin: 0;
  padding: var(--spacing-xs, 2px);
  list-style: none;
  background: var(--color-surface, #FFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
}
[data-fui-comp="ui-global-search"] .fui-global-search__option {
  display: flex;
  align-items: baseline;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 4px);
  cursor: pointer;
  min-height: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-global-search"] .fui-global-search__option.is-active {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-global-search"] .fui-global-search__hint {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-global-search"] .fui-global-search__chord {
  font-family: var(--font-mono, monospace);
  font-size: var(--text-xs, 0.75rem);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-sm, 4px);
  padding: 1px 6px;
  background: var(--color-surface-soft, #F4F4F5);
}`
}
