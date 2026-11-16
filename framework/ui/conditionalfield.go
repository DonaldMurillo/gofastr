package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ConditionalFieldConfig configures a field conditionally shown based
// on another field's value.
type ConditionalFieldConfig struct {
	// WhenName is the form field name to watch. Required.
	WhenName string

	// WhenValue is the value that shows the children; set it or WhenValues.
	WhenValue string

	// WhenValues shows the children while the field holds any of them.
	WhenValues []string

	Children []render.HTML
	Class    string

	// ExtraAttrs lands on the region; hidden, aria-hidden and the
	// component's own keys are dropped.
	ExtraAttrs html.Attrs
}

// ConditionalField renders a region that is visible on first paint and
// that the headless module hides until the watched field matches.
func ConditionalField(cfg ConditionalFieldConfig) render.HTML {
	if cfg.WhenName == "" {
		panic("ui: ConditionalField requires WhenName")
	}
	if cfg.WhenValue != "" && len(cfg.WhenValues) > 0 {
		panic("ui: ConditionalField takes WhenValue or WhenValues, not both")
	}
	if cfg.WhenValue == "" && len(cfg.WhenValues) == 0 {
		panic("ui: ConditionalField requires WhenValue or WhenValues")
	}
	return conditionalFieldStyle.WrapHTML(headless.ConditionalField(
		headless.ConditionalFieldProps{
			When:       cfg.WhenName,
			Value:      cfg.WhenValue,
			Values:     cfg.WhenValues,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "hidden", "aria-hidden"),
		},
		withRootClass(whenClasses, cfg.Class),
		cfg.Children...))
}

var conditionalFieldStyle = registry.RegisterStyle("ui-conditional-field", conditionalFieldCSS)

func conditionalFieldCSS(_ style.Theme) string {
	return `.fui-when {
  display: grid;
  gap: var(--spacing-md, 8px);
}
/* The platform's hidden attribute already means display:none; this
   restates it on the class so a sheet that re-displayed the region
   for layout could not un-hide what the module hid — the hiding is
   the strongest fact on the element. */
.fui-when[hidden] {
  display: none;
}`
}
