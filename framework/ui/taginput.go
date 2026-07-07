package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── TagInput ───────────────────────────────────────────────────────
//
// Free-form text → chips. User types in the input, hits Enter or
// comma to commit a tag; Backspace on an empty input removes the
// last tag. Each committed tag becomes its own hidden <input> sharing
// the same Name, so form submission uses the standard repeated-key
// pattern (?tags=go&tags=rust).
//
// Different from MultiSelect: MultiSelect has a fixed option list;
// TagInput is open-ended free text.

// TagInputConfig configures a TagInput.
type TagInputConfig struct {
	// Name is the form-field name (required). Each tag is submitted
	// under this name (repeated key).
	Name string
	// Label is the accessible label (required).
	Label string
	// Values are the initial tags.
	Values []string
	// Placeholder for the text input.
	Placeholder string
	// MaxLength caps individual tag length (chars). 0 = no cap.
	MaxLength int
	// Help renders supporting text under the field.
	Help string
	// Disabled disables all interaction.
	Disabled bool
	ID       string
	Class    string
}

// TagInput renders a free-form tag input bound to a chip strip.
func TagInput(cfg TagInputConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: TagInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: TagInput requires Label")
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}
	cls := "ui-tag-input"
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Initial tags as hidden inputs.
	tagInputs := make([]render.HTML, 0, len(cfg.Values))
	for _, v := range cfg.Values {
		tagInputs = append(tagInputs, render.Tag("input", map[string]string{
			"type":  "hidden",
			"name":  cfg.Name,
			"value": v,
			"class": "ui-tag-input__hidden",
		}))
	}

	inputAttrs := map[string]string{
		"type":                  "text",
		"id":                    id,
		"class":                 "ui-tag-input__field",
		"aria-label":            cfg.Label,
		"autocomplete":          "off",
		"data-fui-tag-input":    cfg.Name,
		"data-fui-tag-input-id": id,
	}
	if cfg.Placeholder != "" {
		inputAttrs["placeholder"] = cfg.Placeholder
	}
	if cfg.Disabled {
		inputAttrs["disabled"] = ""
	}
	if cfg.MaxLength > 0 {
		inputAttrs["maxlength"] = strItoa(cfg.MaxLength)
	}

	zone := render.Tag("div", map[string]string{
		"class":                   "ui-tag-input__zone",
		"data-fui-tag-input-zone": "true",
	},
		append(tagInputs,
			render.Tag("input", inputAttrs),
		)...,
	)

	children := []render.HTML{
		render.Tag("label", map[string]string{"for": id, "class": "ui-tag-input__label"},
			render.Text(cfg.Label)),
		zone,
	}
	if cfg.Help != "" {
		children = append(children, html.Paragraph(html.TextConfig{
			Class: "ui-tag-input__help",
		}, render.Text(cfg.Help)))
	}

	return tagInputStyle.WrapHTML(render.Tag("div",
		map[string]string{"class": cls}, children...))
}

func strItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	out := make([]byte, 0, 4)
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

var tagInputStyle = registry.RegisterStyle("ui-tag-input", tagInputCSS)

func tagInputCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-tag-input"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__label {
  font-weight: 500;
  font-size: var(--text-sm, 0.9rem);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__zone {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 6px);
  align-items: center;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__zone:focus-within {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__chip {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px) var(--spacing-xs, 2px) 10px;
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  border-radius: 999px;
  font-size: var(--text-sm, 0.85rem);
  font-weight: 500;
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__chip-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--text-base, 1rem);
  line-height: 1;
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__chip-remove:hover {
  background: color-mix(in srgb, var(--color-primary-fg, #FFFFFF) 25%, transparent);
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__field {
  flex: 1 1 8rem;
  border: 0;
  outline: 0;
  background: transparent;
  font: inherit;
  font-size: var(--text-base, 0.95rem);
  color: var(--color-text, #18181B);
  min-block-size: 28px;
  padding: 0;
}
[data-fui-comp="ui-tag-input"] .ui-tag-input__help {
  margin: 0;
  font-size: var(--text-sm, 0.85rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-tag-input"].is-disabled .ui-tag-input__zone {
  opacity: 0.6;
  cursor: not-allowed;
}`
}
