package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── InputGroup ─────────────────────────────────────────────────────
//
// Composite input wrapper that prepends and/or appends decorative
// content (text, icons, currency symbols, units) to a core input
// element. Pure CSS — no runtime JS needed.

// InputGroupConfig configures an InputGroup.
type InputGroupConfig struct {
	// Prepend is optional content rendered before the input (text, icon, etc.).
	Prepend render.HTML
	// Input is the actual input element (required).
	Input render.HTML
	// Append is optional content rendered after the input.
	Append render.HTML
	// Class adds extra CSS classes to the wrapper.
	Class string
}

// InputGroup renders an input with optional prepend and append addons.
// The prepend/append addons share borders with the input for a merged
// appearance.
func InputGroup(cfg InputGroupConfig) render.HTML {
	if cfg.Input == "" {
		panic("ui: InputGroup requires Input")
	}

	cls := "ui-input-group"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	children := []render.HTML{}

	if cfg.Prepend != "" {
		children = append(children,
			render.Tag("span", map[string]string{
				"class":       "ui-input-group__prepend",
				"aria-hidden": "true",
			}, cfg.Prepend))
	}

	children = append(children, cfg.Input)

	if cfg.Append != "" {
		children = append(children,
			render.Tag("span", map[string]string{
				"class":       "ui-input-group__append",
				"aria-hidden": "true",
			}, cfg.Append))
	}

	return inputGroupStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls},
		children...))
}

var inputGroupStyle = registry.RegisterStyle("ui-input-group", inputGroupCSS)

func inputGroupCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-input-group"] {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
}
[data-fui-comp="ui-input-group"] > input,
[data-fui-comp="ui-input-group"] > select {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 0.95rem);
  padding: 10px var(--spacing-md, 12px);
  color: var(--color-text, #18181B);
  min-block-size: var(--spacing-touch-target, 44px);
  min-width: 0;
}
[data-fui-comp="ui-input-group"] > input:focus-visible,
[data-fui-comp="ui-input-group"] > select:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-input-group"] .ui-input-group__prepend,
[data-fui-comp="ui-input-group"] .ui-input-group__append {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--spacing-md, 12px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.9rem);
  white-space: nowrap;
  user-select: none;
  border-right: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-input-group"] .ui-input-group__append {
  border-right: 0;
  border-left: 1px solid var(--color-border, #E4E4E7);
}`
}
