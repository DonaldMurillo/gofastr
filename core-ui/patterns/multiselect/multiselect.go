// Package multiselect renders a checkbox-group inside a disclosure
// with chip rendering of the selected values above the trigger.
//
// Output structure:
//
//	<div data-fui-comp="ui-multiselect">
//	  <div class="ui-multiselect__chips" data-fui-multiselect-chips>
//	    <!-- runtime fills with chips for each :checked option -->
//	  </div>
//	  <details class="ui-multiselect__disclosure">
//	    <summary>Pick languages…</summary>
//	    <fieldset role="group">
//	      <label for="<id>-opt-0"><input id="<id>-opt-0" type="checkbox" name="…" value="…"> Go</label>
//	      …
//	    </fieldset>
//	  </details>
//	</div>
//
// Server gets standard checkbox-group submission semantics (Name
// repeats for each checked option). No RPC dance — apps that need
// server-fetched options should compose this with a partial render.
package multiselect

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Option is a single checkbox option.
type Option struct {
	Value    string // form-submit value (required)
	Label    string // visible label (required)
	Selected bool   // initial checked state
	Disabled bool
}

// Config configures a MultiSelect.
type Config struct {
	// Name is the form-field name (required). All checkboxes share
	// this name; the form receives the repeated key for each checked
	// option.
	Name string
	// Label is the accessible label (required).
	Label string
	// Placeholder is the disclosure-summary text shown when no
	// options are selected. Defaults to "Choose…".
	Placeholder string
	// Options are the choices (≥1).
	Options []Option
	// Open opts the disclosure into rendering open by default.
	Open bool
	// ID / Class / Attrs are passed through to the wrapper.
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// Render renders the MultiSelect.
func Render(cfg Config) render.HTML {
	if cfg.Name == "" {
		panic("multiselect: Name required")
	}
	if cfg.Label == "" {
		panic("multiselect: Label required")
	}
	if len(cfg.Options) == 0 {
		panic("multiselect: ≥1 Option required")
	}
	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = "Choose…"
	}
	id := cfg.ID
	if id == "" {
		id = cfg.Name
	}

	cls := "ui-multiselect"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	rows := make([]render.HTML, 0, len(cfg.Options))
	for i, opt := range cfg.Options {
		if opt.Value == "" || opt.Label == "" {
			panic("multiselect: each Option needs Value + Label")
		}
		// Option ids are <instance id>-opt-<index>: the index keeps
		// symbol-heavy values ("C++" vs "C#") collision-free, and the
		// instance id (cfg.ID, falling back to Name — must be unique
		// per page) scopes them across multiselect instances.
		optID := id + "-opt-" + itoa(i)
		inputAttrs := map[string]string{
			"type":  "checkbox",
			"name":  cfg.Name,
			"id":    optID,
			"value": opt.Value,
			"class": "ui-multiselect__check",
		}
		if opt.Selected {
			inputAttrs["checked"] = ""
		}
		if opt.Disabled {
			inputAttrs["disabled"] = ""
		}
		// The label wraps the input AND carries for= — the runtime's
		// chip renderer resolves the chip text via
		// label[for="<checkbox id>"] .ui-multiselect__row-label.
		rows = append(rows, render.Tag("label",
			map[string]string{"class": "ui-multiselect__row", "for": optID},
			render.Tag("input", inputAttrs),
			html.Span(html.TextConfig{Class: "ui-multiselect__row-label"}, render.Text(opt.Label)),
		))
	}

	detailsAttrs := map[string]string{
		"class":                     "ui-multiselect__disclosure",
		"data-fui-multiselect":      "true",
		"data-fui-multiselect-name": cfg.Name,
	}
	if cfg.Open {
		detailsAttrs["open"] = ""
	}

	chips := render.Tag("div", map[string]string{
		"class":                            "ui-multiselect__chips",
		"data-fui-multiselect-chips":       "true",
		"data-fui-multiselect-placeholder": placeholder,
		"aria-live":                        "polite",
	})

	summary := render.Tag("summary",
		map[string]string{"class": "ui-multiselect__summary"},
		render.Text(cfg.Label))

	fieldset := render.Tag("fieldset",
		map[string]string{"class": "ui-multiselect__group", "role": "group", "aria-label": cfg.Label},
		rows...)

	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	return multiSelectStyle.WrapHTML(render.Tag("div", attrs,
		chips,
		render.Tag("details", detailsAttrs, summary, fieldset),
	))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	buf := make([]byte, 0, 4)
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

var multiSelectStyle = registry.RegisterStyle("ui-multiselect", multiSelectCSS)

func multiSelectCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-multiselect"] {
  display: grid;
  gap: var(--spacing-xs, 4px);
  max-inline-size: 32rem;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__chips {
  display: flex;
  gap: var(--spacing-xs, 6px);
  flex-wrap: wrap;
  min-block-size: 28px;
  align-items: center;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__chips:empty::before {
  content: attr(data-fui-multiselect-placeholder);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.9rem);
  font-style: italic;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__chip {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 6px);
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px) var(--spacing-sm, 4px) 10px;
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  border-radius: 999px;
  font-size: var(--text-sm, 0.85rem);
  font-weight: 500;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__chip-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--text-lg, 1.1rem);
  line-height: 1;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__chip-remove:hover {
  background: color-mix(in srgb, var(--color-primary-fg, #FFFFFF) 25%, transparent);
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__disclosure {
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__summary {
  display: flex;
  align-items: center;
  min-block-size: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 12px);
  font-weight: 500;
  color: var(--color-text, #18181B);
  cursor: pointer;
  user-select: none;
  list-style: none;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__summary::-webkit-details-marker {
  display: none;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__summary::before {
  content: "▾";
  margin-inline-end: var(--spacing-sm, 8px);
  font-size: var(--text-xs, 0.7rem);
  color: var(--color-text-muted, #52525B);
  transition: transform 120ms ease;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__disclosure[open] .ui-multiselect__summary::before {
  transform: rotate(180deg);
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__group {
  display: grid;
  gap: 0;
  border: 0;
  padding: 0;
  border-top: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  min-block-size: var(--spacing-touch-target, 44px);
  cursor: pointer;
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__row:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-multiselect"] .ui-multiselect__check:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}`
}
