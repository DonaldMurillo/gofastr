package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Picker ────────────────────────────────────────────────────────
//
// A form field that picks one record out of many: a search input over
// a server-searched listbox, with a hidden input that submits the
// picked value (headless.Combobox in Pick mode). It sits inside a host
// form; the search input is detached from that form, so only the value
// submits. Endpoint answers each search with PickerRows.

// PickerOption is one row of a picker.
type PickerOption struct {
	// Value is what the field submits when the row is picked. Required.
	Value string
	// Label is the row's text, and the input's text once picked.
	Label string
	// Meta is secondary text beside the label.
	Meta string
}

// PickerConfig configures a Picker.
type PickerConfig struct {
	// Name is the submitted field name; ID the search input's id
	// (default "pick-"+Name). Label names the field. Required: Name,
	// Label, Endpoint.
	Name, ID, Label string
	// Help and Error describe the field, as on every form field.
	Help, Error string
	Required    bool
	// Value is the picked value and ValueLabel its text; both empty
	// when nothing is picked.
	Value, ValueLabel string
	Placeholder       string
	// Options are the listbox's first rows, shown on focus before any
	// search; More is a note row after them ("Showing 20 of 312").
	Options []PickerOption
	More    string
	// Endpoint is the same-origin path a search POSTs {"q": …} to; it
	// answers with PickerRows for this picker's ID.
	Endpoint string
	// Action sits after the input on its row: an open or a new button.
	Action render.HTML
	Class  string
	// ExtraAttrs forwards attributes to the combobox's root; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// Picker renders the field.
func Picker(cfg PickerConfig) render.HTML {
	if cfg.Name == "" || cfg.Label == "" || cfg.Endpoint == "" {
		panic("ui: Picker requires Name, Label and Endpoint")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id := cfg.ID
	if id == "" {
		id = "pick-" + cfg.Name
	}
	classes := headless.Classes{
		headless.PartComboboxForm:    "fui-combobox__form",
		headless.PartComboboxInput:   "fui-combobox__input",
		headless.PartComboboxListbox: "fui-combobox__listbox",
		headless.PartComboboxOption:  "fui-combobox__option",
		headless.PartComboboxStatus:  "fui-visually-hidden",
		headless.PartText:            "fui-combobox__option-label",
	}
	control := func(c headless.FieldControl) render.HTML {
		box := headless.Own(headless.Combobox(headless.ComboboxProps{
			ID:          id,
			Name:        "q",
			Label:       cfg.Label,
			Placeholder: cfg.Placeholder,
			Island:      &headless.Island{Endpoint: cfg.Endpoint, Signal: id},
			Pick:        &headless.ComboboxPick{Name: cfg.Name, Value: cfg.Value, Label: cfg.ValueLabel},
			Options:     pickerOptions(cfg.Options, cfg.More),
			Control:     &c,
			ExtraAttrs:  headless.Safe(cfg.ExtraAttrs),
			Strings:     StringsFor(ctx),
		}, classes))
		box = headless.Own(comboboxStyle.WrapHTML(render.Tag("div", map[string]string{"class": "fui-combobox fui-combobox--fill"}, box)))
		if cfg.Action == "" {
			return box
		}
		return pickerStyle.WrapHTML(render.Tag("div", map[string]string{"class": "fui-picker"}, box, cfg.Action))
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

// PickerRows renders a picker endpoint's answer for the picker whose ID
// is id: one row per option, then the More note when set.
func PickerRows(id string, opts []PickerOption, more string) render.HTML {
	return headless.ComboboxRows(id+"-listbox", pickerOptions(opts, more), headless.Classes{
		headless.PartComboboxOption: "fui-combobox__option",
		headless.PartText:           "fui-combobox__option-label",
	})
}

func pickerOptions(opts []PickerOption, more string) []headless.ComboboxOption {
	out := make([]headless.ComboboxOption, 0, len(opts)+1)
	for _, o := range opts {
		out = append(out, headless.ComboboxOption{Value: o.Value, Label: o.Label, Meta: o.Meta})
	}
	if more != "" {
		out = append(out, headless.ComboboxOption{Label: more, Disabled: true})
	}
	return out
}

var pickerStyle = registry.RegisterStyle("ui-picker", pickerCSS)

// pickerCSS lays the input and its Action on one row, the input taking
// the rest of it.
func pickerCSS(_ style.Theme) string {
	return `.fui-picker {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
.fui-picker > :where(.fui-combobox) { flex: 1; min-inline-size: 0; }`
}
