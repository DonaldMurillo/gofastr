package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ConditionalField ───────────────────────────────────────────────
//
// Shows/hides content based on another form field's value. SSR-safe:
// the element renders hidden by default with data-when-name and
// data-when-value attributes. The runtime JS module
// (conditionalfield.js) listens for change/input events on the
// parent form and toggles the hidden attribute + aria-hidden.

// ConditionalFieldConfig configures a field conditionally shown based
// on another field's value.
type ConditionalFieldConfig struct {
	// WhenName is the form field name to watch. Required.
	WhenName string

	// WhenValue is the value that triggers showing the children.
	// For checkboxes/radios, this matches the value attribute. Required.
	WhenValue string

	// Children is the content to show when the condition is met.
	Children []render.HTML

	Class string
}

// ConditionalField renders a container that is hidden by default and
// shown via runtime JS when the watched field matches WhenValue.
//
// The component renders with `hidden` and `aria-hidden="true"` so it
// starts invisible. The runtime module listens for change/input events
// on the ancestor form and toggles visibility.
func ConditionalField(cfg ConditionalFieldConfig) render.HTML {
	if cfg.WhenName == "" {
		panic("ui: ConditionalField requires WhenName")
	}
	if cfg.WhenValue == "" {
		panic("ui: ConditionalField requires WhenValue")
	}

	cls := "ui-conditional-field"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"data-fui-comp":   "ui-conditional-field",
		"class":           cls,
		"data-when-name":  cfg.WhenName,
		"data-when-value": cfg.WhenValue,
		"hidden":          "",
		"aria-hidden":     "true",
	}

	return conditionalFieldStyle.WrapHTML(
		render.Tag("div", attrs, cfg.Children...))
}

// EvaluateInitialState returns true if the component should be visible
// based on the provided current value of the watched field. This is a
// helper for server-side rendering when the form already has a value
// that should pre-show the conditional content.
func (cfg ConditionalFieldConfig) EvaluateInitialState(currentValue string) bool {
	return currentValue == cfg.WhenValue
}

// ConditionalFieldVisible renders a ConditionalField that is initially
// visible (no hidden attribute). Use this when the server knows the
// watched field already matches (e.g. re-rendering after a POST with
// validation errors where the trigger field was already selected).
func ConditionalFieldVisible(cfg ConditionalFieldConfig) render.HTML {
	if cfg.WhenName == "" {
		panic("ui: ConditionalFieldVisible requires WhenName")
	}
	if cfg.WhenValue == "" {
		panic("ui: ConditionalFieldVisible requires WhenValue")
	}

	cls := "ui-conditional-field"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"data-fui-comp":   "ui-conditional-field",
		"class":           cls,
		"data-when-name":  cfg.WhenName,
		"data-when-value": cfg.WhenValue,
	}

	return conditionalFieldStyle.WrapHTML(
		render.Tag("div", attrs, cfg.Children...))
}

// conditionalFieldStyle is registered in styles_components.go

func conditionalFieldCSS(_ style.Theme) string {
	return fmt.Sprintf(`[data-fui-comp="ui-conditional-field"] {
  display: grid;
  gap: var(--spacing-md, 8px);
}
/* The hidden attribute on the element handles display:none.
   When the runtime JS removes [hidden], the grid layout takes over.
   This rule ensures the transition is clean. */
[data-fui-comp="ui-conditional-field"][hidden] {
  display: none;
}
/* Smooth reveal when becoming visible (no transition for hiding
   since display:none can't be transitioned). */
[data-fui-comp="ui-conditional-field"]:not([hidden]) {
  animation: ui-conditional-field-reveal 150ms ease-out;
}
@keyframes ui-conditional-field-reveal {
  from { opacity: 0; transform: translateY(-4px); }
  to   { opacity: 1; transform: translateY(0); }
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-conditional-field"]:not([hidden]) {
    animation: none;
  }
}`)
}
