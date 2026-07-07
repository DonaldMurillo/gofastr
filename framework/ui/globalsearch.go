package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/combobox"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── GlobalSearch ───────────────────────────────────────────────────
//
// Sticky page-level search bar with `/`-shortcut focus + a Combobox-
// driven dropdown of results. Distinct from CommandPalette (which is
// a focus-trapped ⌘K modal) — GlobalSearch is inline, persistent, and
// per-page.
//
// Compositional shape: GlobalSearch is mostly a wrapper around the
// core-ui/patterns/combobox primitive, adding:
//   - Sticky position styling
//   - `/`-shortcut focus via data-fui-shortcut-focus
//   - Optional ShortcutHint chip ("Press / to search")

// GlobalSearchConfig configures a GlobalSearch.
type GlobalSearchConfig struct {
	// ID is the input element id (the runtime listbox uses
	// <ID>-listbox). Required, page-unique.
	ID string
	// Name is the form-submit name on the input. Required.
	Name string
	// Label is the visible label text (required, used as <label for=…>).
	Label string
	// RPCPath is the search endpoint. Required. POSTed with the
	// query in `<Name>=<value>`.
	RPCPath string
	// SignalName is the rpc-signal value used to swap the listbox
	// HTML after each search response. Required.
	SignalName string
	// Placeholder for the input. Default "Search…".
	Placeholder string
	// Shortcut, when set, opts the input into runtime focus-on-key:
	// `data-fui-shortcut-focus="<chord>"` (default "/"). Pass an
	// explicit empty string to disable.
	Shortcut string
	// ShowHint renders a small "Press <chord>" hint chip on the right
	// of the input. Default true when Shortcut is set.
	ShowHint *bool
	// DebounceMs is the input debounce window (passed to combobox).
	// Default 200.
	DebounceMs int
	// Sticky toggles position: sticky on the wrapper. Default true.
	Sticky bool
	Class  string
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
	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = "Search…"
	}
	shortcut := cfg.Shortcut
	if cfg.Shortcut == "" && cfg.Shortcut != " " {
		// Default to "/" unless caller explicitly passed " " to disable.
		// Use a sentinel " " to express "off" because zero-value collides
		// with "default".
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

	box := combobox.Render(combobox.Config{
		ID:          cfg.ID,
		Name:        cfg.Name,
		Label:       cfg.Label,
		RPCPath:     cfg.RPCPath,
		SignalName:  cfg.SignalName,
		DebounceMs:  debounceMs,
		Placeholder: placeholder,
		LabelHidden: true,
	})

	// Decorate the combobox input with shortcut markers — done by
	// emitting wrapping JS-free annotations the runtime picks up.
	// The framework's `data-fui-shortcut-focus` listens for the chord
	// globally and focuses the matching `[data-fui-shortcut-focus="<chord>"]`
	// element. We add it via a sibling marker element that points at
	// the input via aria-controls — but the simpler pattern is to
	// expose Shortcut on the GlobalSearch wrapper and let the runtime
	// focus the FIRST input within when the chord fires. We use a
	// per-instance wrapper-level shortcut marker (data-fui-shortcut-
	// focus + data-fui-shortcut-target="#<input-id>"). The runtime
	// handler already supports targeting by selector.
	wrapAttrs := html.Attrs{"class": clsGlobalSearch(cfg)}
	if shortcut != "" {
		wrapAttrs["data-fui-shortcut-focus"] = shortcut
		wrapAttrs["data-fui-shortcut-target"] = "#" + cfg.ID
	}

	children := []render.HTML{box}
	if showHint && shortcut != "" {
		// shortcut flows raw into the kbd body; HTML-escape it so a
		// CMS-supplied or untrusted-source shortcut hint can't smuggle
		// `<script>` or breakout markup into the page.
		children = append(children, html.Span(html.TextConfig{
			Class:      "ui-global-search__hint",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Tag("kbd", map[string]string{"class": "ui-global-search__chord"}, render.Text(shortcut))))
	}
	return globalSearchStyle.WrapHTML(render.Tag("div", wrapAttrs, children...))
}

func clsGlobalSearch(cfg GlobalSearchConfig) string {
	cls := "ui-global-search"
	if cfg.Sticky {
		cls += " ui-global-search--sticky"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return cls
}

var globalSearchStyle = registry.RegisterStyle("ui-global-search", globalSearchCSS)

func globalSearchCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-global-search"] {
  position: relative;
  display: block;
  inline-size: 100%;
  max-inline-size: 32rem;
}
[data-fui-comp="ui-global-search"].ui-global-search--sticky {
  position: sticky;
  inset-block-start: var(--spacing-md, 12px);
  z-index: 5;
  background: var(--color-background, #FFFFFF);
}
[data-fui-comp="ui-global-search"] .ui-global-search__hint {
  position: absolute;
  inset-inline-end: var(--spacing-sm, 8px);
  inset-block-start: 50%;
  transform: translateY(-50%);
  pointer-events: none;
}
[data-fui-comp="ui-global-search"] .ui-global-search__chord {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: 22px;
  block-size: 22px;
  padding: 0 6px;
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface-soft, #F4F4F5);
  border: 1px solid var(--color-border, #E4E4E7);
  color: var(--color-text-muted, #52525B);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 0.8rem);
  font-weight: 600;
}

/* Hide hint on touch — / shortcut is irrelevant. */
@media (hover: none) {
  [data-fui-comp="ui-global-search"] .ui-global-search__hint { display: none; }
}`
}
