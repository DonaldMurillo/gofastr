package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── ConditionalField ───────────────────────────────────────────────
//
// A region shown or hidden by another field's value, rendered through
// headless.ConditionalField. The region renders VISIBLE and the
// headless behaviour module hides it when the watched field does not
// match: a field only a script can reveal is a field a reader without
// script never reaches, so the no-script page shows every dependent
// field and script takes them away, never the other way round. The
// module also disables the controls inside a hidden region (telling
// them apart from controls the page disabled itself) so nothing
// hidden submits.

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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the region's root
	// element. Keys the component owns are dropped: class (use
	// Class), id, data-fui-*, data-hui-* (the region's hooks are the
	// runtime's contract, not a caller's to forge), hidden and
	// aria-hidden (the module owns the region's visibility).
	ExtraAttrs html.Attrs
}

// ConditionalField renders a container that is visible on first paint
// and hidden by the headless runtime module until the watched field
// matches WhenValue. The watched field is resolved the way the form
// would submit it: the checked radio's value, a checkbox's value when
// checked, any other control's value.
func ConditionalField(cfg ConditionalFieldConfig) render.HTML {
	if cfg.WhenName == "" {
		panic("ui: ConditionalField requires WhenName")
	}
	if cfg.WhenValue == "" {
		panic("ui: ConditionalField requires WhenValue")
	}
	return conditionalFieldStyle.WrapHTML(headless.ConditionalField(
		headless.ConditionalFieldProps{
			When:  cfg.WhenName,
			Value: cfg.WhenValue,
			// hidden and aria-hidden are owned by the runtime module,
			// which sets and clears them as the watched field changes;
			// a caller-set value would fight it.
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
