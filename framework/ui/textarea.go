package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── TextArea ───────────────────────────────────────────────────────
//
// Labelled multi-line text input, rendered through headless.Field +
// headless.Textarea the way Select is: the field owns the label, the
// hint, the error and the wiring that ties them to the control, and
// the textarea carries this component's own marker so its sheet loads
// wherever a TextArea renders, inside a Form or alone.
//
// Autogrow is ui-only surface with its own runtime module
// (textarea.js, bound on data-fui-autogrow), so it reaches the
// control through headless.Textarea's typed Autogrow prop — the one
// seam that survives the data-fui-* refusal every caller-reachable
// extra goes through.

// TextAreaConfig configures a TextArea.
type TextAreaConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required).
	Label string
	// Value is the initial value.
	Value string
	// Placeholder renders the native placeholder.
	Placeholder string
	// Rows is the initial visible row count. Defaults to 3.
	Rows int
	// Autogrow opts into runtime auto-resize: every input event
	// resets the height to scrollHeight so the field always shows all
	// content without an internal scrollbar.
	Autogrow bool
	// Required marks the field required.
	Required bool
	// Disabled disables interaction.
	Disabled bool
	// Help renders supporting text under the field.
	Help string
	// Error overrides Help with an error message + aria-invalid.
	Error string
	// MaxLength applies the native maxlength attribute.
	MaxLength int
	ID        string
	Class     string
	// ExtraAttrs forwards additional attributes to the <textarea>
	// element. Keys the component owns are dropped: class and id (use
	// Class / ID), data-fui-* (incl. the autogrow wiring), name, rows,
	// placeholder, disabled, required, maxlength, aria-invalid, and
	// aria-describedby.
	ExtraAttrs html.Attrs
}

// TextArea renders a labelled multi-line text input.
func TextArea(cfg TextAreaConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: TextArea requires Name")
	}
	if cfg.Label == "" {
		panic("ui: TextArea requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	rows := cfg.Rows
	if rows == 0 {
		rows = 3
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"name", "rows", "placeholder", "disabled", "required", "maxlength",
		"aria-invalid", "aria-describedby")
	if extra == nil {
		extra = html.Attrs{}
	}
	if cfg.MaxLength > 0 {
		extra["maxlength"] = strconv.Itoa(cfg.MaxLength)
	}

	control := func(c headless.FieldControl) render.HTML {
		return textAreaStyle.WrapHTML(headless.Textarea(headless.TextareaProps{
			Name:        cfg.Name,
			DescribedBy: c.DescribedBy,
			Value:       cfg.Value,
			Placeholder: cfg.Placeholder,
			Rows:        rows,
			Required:    c.Required,
			Disabled:    cfg.Disabled,
			Invalid:     c.Invalid,
			Autogrow:    cfg.Autogrow,
			ID:          c.ID,
			Extra:       extra,
		}, textAreaClasses))
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

var textAreaStyle = registry.RegisterStyle("ui-textarea", textAreaCSS)

func textAreaCSS(_ style.Theme) string {
	return `.fui-textarea {
  font: inherit;
  font-size: var(--text-base, 1rem);
  padding: 10px var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  resize: vertical;
  min-block-size: 44px;
  line-height: 1.5;
}
.fui-textarea[data-fui-autogrow] {
  /* Autogrow rules the height; user resize would fight the JS. */
  resize: none;
  overflow: hidden;
}
.fui-textarea:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
.fui-textarea[aria-invalid="true"] {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
.fui-textarea:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
