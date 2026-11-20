package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// InputGroupConfig configures an InputGroup.
type InputGroupConfig struct {
	// Prepend is decoration drawn before the input, hidden from assistive tech.
	Prepend render.HTML
	// Input is the control. Required; inside a FormField builder, build
	// it from the wiring the field hands the closure.
	Input render.HTML
	// Append is decoration drawn after the input, hidden from assistive tech.
	Append     render.HTML
	Class      string
	ExtraAttrs html.Attrs
}

// InputGroup renders an input with optional prepend and append addons.
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
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
}
/* The group is the control: it takes the focus ring its input would. */
.fui-input-group:focus-within {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
.fui-input-group > input,
.fui-input-group > select {
  flex: 1;
  border: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  padding: var(--ui-control-padding-y, 10px) calc(var(--spacing-sm, 4px) * 3);
  color: var(--color-text, #09090B);
  min-block-size: var(--fui-density-control-h);
  min-width: 0;
}
.fui-input-group > input::placeholder { color: var(--color-text-subtle); }
/* So is the error state: the group draws the danger ring an invalid
   input would, and the input inside draws none, or the addon sits
   outside the red box. The input selectors outweigh the control's own
   [aria-invalid] rule, whose sheet may load after this one. */
.fui-input-group:has(> :is(:user-invalid, [aria-invalid="true"])) {
  border-color: var(--color-danger, #B91C1C);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-danger, #B91C1C);
}
.fui-input-group > input,
.fui-input-group > select,
.fui-input-group > input:is(:user-invalid, [aria-invalid="true"]),
.fui-input-group > select:is(:user-invalid, [aria-invalid="true"]) { box-shadow: none; }
.fui-input-group > input:focus-visible,
.fui-input-group > select:focus-visible {
  outline: none;
}
/* Phones keep text-base so iOS does not zoom into the focused control. */
@media (max-width: 767.98px) {
  .fui-input-group > input,
  .fui-input-group > select { font-size: var(--text-base, 1rem); }
}
.fui-input-group .fui-input-group__prepend,
.fui-input-group .fui-input-group__append {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* Addons sit inside the field in muted text, the way a unit or a
     currency symbol reads in print; no gray slab, no divider. */
  padding-block: 0;
  padding-inline: calc(var(--spacing-sm, 4px) * 3) 0;
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  white-space: nowrap;
  user-select: none;
}
.fui-input-group .fui-input-group__prepend + input,
.fui-input-group .fui-input-group__prepend + select { padding-inline-start: var(--spacing-md, 8px); }
.fui-input-group .fui-input-group__append { padding-inline: 0 calc(var(--spacing-sm, 4px) * 3); }
.fui-input-group input:has(+ .fui-input-group__append),
.fui-input-group select:has(+ .fui-input-group__append) { padding-inline-end: var(--spacing-md, 8px); }`
}
