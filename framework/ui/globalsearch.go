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

// GlobalSearchConfig configures a GlobalSearch.
type GlobalSearchConfig struct {
	// ID is the page-unique input id. Required.
	ID string
	// Name is the input's form name. Required.
	Name string
	// Label is the visually hidden label. Required.
	Label string
	// RPCPath is the search endpoint. Required.
	RPCPath string
	// SignalName is the signal the listbox HTML is swapped through. Required.
	SignalName string
	// NoScriptAction is the same-origin GET destination without script. Required.
	NoScriptAction string
	// Placeholder defaults to "Search…".
	Placeholder string
	// Shortcut is the chord that focuses the input. Empty is "/"; " " turns it off.
	Shortcut string
	// ShowHint draws the chord chip. Default true when there is a shortcut.
	ShowHint *bool
	// DebounceMs is the input debounce window. Default 200.
	DebounceMs int
	// Sticky pins the bar under the sticky header.
	Sticky bool
	Class  string
	// ExtraAttrs land on the wrapper; class, the data-hui-* wiring and the chords are dropped.
	ExtraAttrs html.Attrs
	// Ctx resolves the default words. Nil uses English.
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
		placeholder = i18nui.SearchPlaceholder.Text(ctx)
	}
	shortcut := cfg.Shortcut
	if cfg.Shortcut == "" && cfg.Shortcut != " " {
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
	box = headless.Own(box)

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
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
		}, render.Tag("kbd", map[string]string{"class": "fui-global-search__chord"}, render.Text(shortcut))))
	}
	boxWrapped := html.Div(html.DivConfig{ExtraAttrs: wrapAttrs}, children...)
	return globalSearchStyle.WrapHTML(boxWrapped)
}

var globalSearchStyle = registry.RegisterStyle("ui-global-search", globalSearchCSS)

func globalSearchCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-global-search"]).fui-global-search {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
:where([data-cui-comp="ui-global-search"]).fui-global-search--sticky {
  position: sticky;
  inset-block-start: calc(var(--ui-sticky-top, 0px) + var(--spacing-md, 8px));
  z-index: var(--z-sticky, 50);
}
[data-cui-comp="ui-global-search"] .fui-global-search__field {
  flex: 1;
}
[data-cui-comp="ui-global-search"] .fui-global-search__input {
  width: 100%;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #09090B);
  font: inherit;
  min-height: var(--spacing-touch-target, 44px);
}
[data-cui-comp="ui-global-search"] .fui-global-search__input:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(var(--stroke-focus-offset, 2px) / 2);
}
[data-cui-comp="ui-global-search"] .fui-global-search__listbox {
  margin: 0;
  padding: var(--spacing-xs, 2px);
  list-style: none;
  background: var(--color-surface, #FFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
}
[data-cui-comp="ui-global-search"] .fui-global-search__option {
  display: flex;
  align-items: baseline;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 6px);
  cursor: pointer;
  min-height: var(--spacing-touch-target, 44px);
}
[data-cui-comp="ui-global-search"] .fui-global-search__option.is-active {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-global-search"] .fui-global-search__hint {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-global-search"] .fui-global-search__chord {
  font-family: var(--font-mono, monospace);
  font-size: var(--text-xs, 0.75rem);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-sm, 6px);
  padding: 1px calc(var(--spacing-sm, 4px) * 1.5);
  background: var(--color-surface-soft, #F4F4F5);
}`
}
