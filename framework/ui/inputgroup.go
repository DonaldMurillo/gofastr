package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── InputGroup ─────────────────────────────────────────────────────
//
// Composite input wrapper that prepends and/or appends decorative
// content (text, icons, currency symbols, units) to a core input
// element, rendered through headless.InputGroup. Pure CSS, no runtime
// JS needed.

// InputGroupConfig configures an InputGroup.
type InputGroupConfig struct {
	// Prepend is optional content rendered before the input (text, icon, etc.).
	Prepend render.HTML
	// Input is the actual input element (required). Inside a
	// FormField builder, build it from the wiring the field handed
	// the closure — ui.Control, a typed control, or headless directly
	// — so the id and the description chain arrive with it.
	Input render.HTML
	// Append is optional content rendered after the input.
	Append render.HTML
	// Class adds extra CSS classes to the wrapper.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the group's root wrapper
	// div. Keys the component owns are dropped: class and id (use
	// Class) and data-fui-*.
	ExtraAttrs html.Attrs
}

// InputGroup renders an input with optional prepend and append addons.
// The prepend/append addons share borders with the input for a merged
// appearance.
func InputGroup(cfg InputGroupConfig) render.HTML {
	if cfg.Input == "" {
		panic("ui: InputGroup requires Input")
	}

	children := []render.HTML{}

	if cfg.Prepend != "" {
		children = append(children,
			render.Tag("span", map[string]string{
				"class":       "fui-input-group__prepend",
				"aria-hidden": "true",
			}, cfg.Prepend))
	}

	children = append(children, cfg.Input)

	if cfg.Append != "" {
		children = append(children,
			render.Tag("span", map[string]string{
				"class":       "fui-input-group__append",
				"aria-hidden": "true",
			}, cfg.Append))
	}

	return inputGroupStyle.WrapHTML(headless.InputGroup(headless.InputGroupProps{
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, withRootClass(inputGroupClasses, cfg.Class), children...))
}

var inputGroupStyle = registry.RegisterStyle("ui-input-group", inputGroupCSS)

func inputGroupCSS(_ style.Theme) string {
	return `.fui-input-group {
  display: inline-flex;
  align-items: stretch;
  max-inline-size: 100%;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #FFFFFF);
  overflow: hidden;
}
.fui-input-group > input,
.fui-input-group > select {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 1rem);
  padding: 10px var(--spacing-md, 8px);
  color: var(--color-text, #18181B);
  min-block-size: var(--fui-density-control-h);
  min-width: 0;
}
.fui-input-group > input:focus-visible,
.fui-input-group > select:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
.fui-input-group .fui-input-group__prepend,
.fui-input-group .fui-input-group__append {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--spacing-md, 8px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  white-space: nowrap;
  user-select: none;
  border-right: 1px solid var(--color-border, #E4E4E7);
}
.fui-input-group .fui-input-group__append {
  border-right: 0;
  border-left: 1px solid var(--color-border, #E4E4E7);
}`
}
