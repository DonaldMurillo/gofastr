package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── TextArea ───────────────────────────────────────────────────────
//
// Labelled multi-line text input. Wraps core-ui/html.TextArea but
// exposes Autogrow as a typed field so the data-fui-autogrow runtime
// hook can resize the height to fit content as the user types.

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
	// Autogrow opts the textarea into runtime auto-resize: every
	// input event resets the height to scrollHeight so the field
	// always shows all content without an internal scrollbar.
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
	MaxLength  int
	ID         string
	Class      string
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

	cls := "ui-textarea"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	taAttrs := map[string]string{
		"name":  cfg.Name,
		"id":    id,
		"class": "ui-textarea__input",
		"rows":  strconv.Itoa(rows),
	}
	if cfg.Placeholder != "" {
		taAttrs["placeholder"] = cfg.Placeholder
	}
	if cfg.Disabled {
		taAttrs["disabled"] = ""
	}
	if cfg.Required {
		taAttrs["required"] = ""
	}
	if cfg.MaxLength > 0 {
		taAttrs["maxlength"] = strconv.Itoa(cfg.MaxLength)
	}
	if cfg.Autogrow {
		taAttrs["data-fui-autogrow"] = ""
	}
	if cfg.Error != "" {
		taAttrs["aria-invalid"] = "true"
		taAttrs["aria-describedby"] = id + "-error"
	} else if cfg.Help != "" {
		taAttrs["aria-describedby"] = id + "-help"
	}
	for k, v := range cfg.ExtraAttrs {
		taAttrs[k] = v
	}

	children := []render.HTML{
		render.Tag("label", map[string]string{"for": id, "class": "ui-textarea__label"},
			render.Text(cfg.Label)),
		render.Tag("textarea", taAttrs, render.Text(cfg.Value)),
	}
	if cfg.Error != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:         id + "-error",
			Class:      "ui-textarea__error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	} else if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			ID:    id + "-help",
			Class: "ui-textarea__help",
		}, render.Text(cfg.Help)))
	}

	return textAreaStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

var textAreaStyle = registry.RegisterStyle("ui-textarea", textAreaCSS)

func textAreaCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-textarea"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-textarea"] .ui-textarea__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-textarea"] .ui-textarea__input {
  font: inherit;
  font-size: 0.95rem;
  padding: 10px var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  resize: vertical;
  min-block-size: 44px;
  line-height: 1.5;
}
[data-fui-comp="ui-textarea"] .ui-textarea__input[data-fui-autogrow] {
  /* Autogrow rules the height; user resize would fight the JS. */
  resize: none;
  overflow: hidden;
}
[data-fui-comp="ui-textarea"] .ui-textarea__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-textarea"] .ui-textarea__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-textarea"] .ui-textarea__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-textarea"].is-error .ui-textarea__input {
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
[data-fui-comp="ui-textarea"].is-disabled .ui-textarea__input {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
