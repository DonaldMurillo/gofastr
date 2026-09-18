package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Select ─────────────────────────────────────────────────────────
//
// Labelled native <select>, rendered through headless.Field +
// headless.Select: the field owns the label, the hint, the error and
// the wiring that ties them to the control; the select is the control,
// marked with this component's own data-fui-comp so its sheet loads
// wherever a Select renders, inside a Form or alone.

// SelectOption describes a single <option>.
type SelectOption struct {
	Value    string
	Text     string
	Selected bool
}

// SelectConfig configures a Select.
type SelectConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required).
	Label string
	// Options is the list of <option> elements (required, at least one).
	Options []SelectOption
	// Placeholder adds a disabled, selected-first option with empty value
	// that acts as a placeholder hint (e.g. "Choose a country…").
	Placeholder string
	// Required marks the field required.
	Required bool
	// Disabled disables interaction.
	Disabled bool
	// Help renders supporting text under the field.
	Help string
	// Error renders the field's error message and marks the control
	// invalid. The help stays visible alongside it, the error first.
	Error string
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <select>
	// element (a relation's data-rel-entity among them). Keys the
	// component owns are dropped: class and id (use Class / ID),
	// data-fui-*, name, disabled, required, aria-invalid, and
	// aria-describedby.
	ExtraAttrs html.Attrs
}

// Select renders a labelled native <select> dropdown.
func Select(cfg SelectConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Select requires Name")
	}
	if cfg.Label == "" {
		panic("ui: Select requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	// The last Selected option wins, which is the browser's own rule
	// when more than one carries the attribute.
	selected := ""
	for _, opt := range cfg.Options {
		if opt.Selected {
			selected = opt.Value
		}
	}
	control := func(c headless.FieldControl) render.HTML {
		opts := make([]headless.Option, 0, len(cfg.Options))
		for _, opt := range cfg.Options {
			opts = append(opts, headless.Option{Value: opt.Value, Label: opt.Text})
		}
		// The select carries this component's own marker: its sheet is
		// fetched wherever the control renders, not only inside the
		// field whose marker fetches the field sheet.
		return selectStyle.WrapHTML(headless.Select(headless.SelectProps{
			Name:        cfg.Name,
			DescribedBy: c.DescribedBy,
			Options:     opts,
			Selected:    selected,
			Placeholder: cfg.Placeholder,
			Required:    c.Required,
			Disabled:    cfg.Disabled,
			Invalid:     c.Invalid,
			ID:          c.ID,
			Extra: html.SafeExtraAttrs(cfg.ExtraAttrs,
				"name", "disabled", "required", "aria-invalid", "aria-describedby"),
		}, selectClasses))
	}
	return formFieldStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label:    cfg.Label,
		For:      id,
		Hint:     cfg.Help,
		Error:    cfg.Error,
		Required: cfg.Required,
		Parts:    rootClassParts(cfg.Class),
	}, fieldClasses, control))
}

var selectStyle = registry.RegisterStyle("ui-select", selectCSS)

func selectCSS(_ style.Theme) string {
	return `.fui-select {
  font: inherit;
  font-size: var(--text-base, 1rem);
  padding: 10px var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--fui-field-radius);
  color: var(--color-text, #18181B);
  appearance: none;
  -webkit-appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2371717A' d='M2 4l4 4 4-4'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 12px center;
  padding-right: 36px;
  cursor: pointer;
  min-block-size: var(--fui-density-control-h);
  max-inline-size: 100%;
}
.fui-select:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
.fui-select[aria-invalid="true"] {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
.fui-select:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
