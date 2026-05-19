package html

import (
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SelectOption represents a single <option> within a <select> element.
type SelectOption struct {
	Value    string
	Text     string
	Selected bool
}

// ButtonConfig configures a <button> element.
// Required: Label (used as both visible text and aria-label).
type ButtonConfig struct {
	Label string // required → text content AND aria-label
	Type  string // defaults to "button"
	Class string
	ID    string
	Attrs Attrs
}

// LinkConfig configures an <a> element.
// Required: Href and Text.
type LinkConfig struct {
	Href  string // required
	Text  string // required (visible text content)
	Class string
	ID    string
	Attrs Attrs
}

// LinkHTMLConfig configures an <a> element with raw HTML content.
// Required: Href and Content.
type LinkHTMLConfig struct {
	Href    string      // required
	Content render.HTML // required (raw HTML content)
	Class   string
	ID      string
	Attrs   Attrs
}

// FormConfig configures a <form> element.
// Required: Method.
type FormConfig struct {
	Method string // required: "GET" or "POST"
	Action string // optional: form action URL
	Class  string
	ID     string
	Attrs  Attrs
}

// InputConfig configures a void <input> element.
// Required: Type and Name.
type InputConfig struct {
	Type  string // required: "text", "email", "password", etc.
	Name  string // required: form field name
	Class string
	ID    string
	Attrs Attrs
}

// LabelConfig configures a <label> element.
// Required: For (the ID of the form control) and Text.
type LabelConfig struct {
	For   string // required: ID of the associated form control
	Text  string // required: label text
	Class string
	ID    string
	Attrs Attrs
}

// SelectConfig configures a <select> element.
// Required: Name and Options.
type SelectConfig struct {
	Name    string         // required: form field name
	Options []SelectOption // required: at least one option
	Class   string
	ID      string
	Attrs   Attrs
}

// TextAreaConfig configures a <textarea> element.
// Required: Name.
type TextAreaConfig struct {
	Name  string // required: form field name
	Class string
	ID    string
	Attrs Attrs
}

// FieldSetConfig configures a <fieldset> element.
// Required: Legend.
type FieldSetConfig struct {
	Legend string // required: becomes <legend> text
	Class  string
	ID     string
	Attrs  Attrs
}

// ButtonGroupConfig configures a <div> with role="group" containing buttons.
// No required fields.
type ButtonGroupConfig struct {
	AriaLabel string
	Class     string
	ID        string
	Attrs     Attrs
}

// Button produces a <button> element.
// Required: Label (used as both visible text and aria-label).
func Button(cfg ButtonConfig) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	btnType := cfg.Type
	if btnType == "" {
		btnType = "button"
	}
	setAttr(attrs, "type", btnType)
	if cfg.Label != "" {
		setAttr(attrs, "aria-label", cfg.Label)
	}
	return render.Tag("button", attrs, render.Text(cfg.Label))
}

// Link produces an <a> element with the given href and text content.
// Required: Href and Text.
func Link(cfg LinkConfig) render.HTML {
	if cfg.Href == "" {
		panic("html: Link requires Href")
	}
	if cfg.Text == "" {
		panic("html: Link requires Text")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "href", cfg.Href)
	return render.Tag("a", attrs, render.Text(cfg.Text))
}

// LinkHTML produces an <a> element with raw HTML content (not escaped).
// Required: Href and Content.
func LinkHTML(cfg LinkHTMLConfig) render.HTML {
	if cfg.Href == "" {
		panic("html: LinkHTML requires Href")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "href", cfg.Href)
	return render.Tag("a", attrs, cfg.Content)
}

// Form produces a <form> element.
// Required: Method.
func Form(cfg FormConfig, children ...render.HTML) render.HTML {
	if cfg.Method == "" {
		panic("html: Form requires Method")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "method", cfg.Method)
	if cfg.Action != "" {
		setAttr(attrs, "action", cfg.Action)
	}
	return render.Tag("form", attrs, children...)
}

// Input produces a void <input> element.
// Required: Type and Name.
func Input(cfg InputConfig) render.HTML {
	if cfg.Type == "" {
		panic("html: Input requires Type")
	}
	if cfg.Name == "" {
		panic("html: Input requires Name")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "type", cfg.Type)
	setAttr(attrs, "name", cfg.Name)
	return render.VoidTag("input", attrs)
}

// Label produces a <label> element with a for attribute linking it to
// the form control with the given ID.
// Required: For and Text.
func Label(cfg LabelConfig) render.HTML {
	if cfg.For == "" {
		panic("html: Label requires For")
	}
	if cfg.Text == "" {
		panic("html: Label requires Text")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "for", cfg.For)
	return render.Tag("label", attrs, render.Text(cfg.Text))
}

// Select produces a <select> element containing the given options.
// Required: Name.
func Select(cfg SelectConfig) render.HTML {
	if cfg.Name == "" {
		panic("html: Select requires Name")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "name", cfg.Name)

	children := make([]render.HTML, len(cfg.Options))
	for i, opt := range cfg.Options {
		children[i] = Option(opt.Value, opt.Text, opt.Selected)
	}
	return render.Tag("select", attrs, children...)
}

// Option produces an <option> element with value and selected state.
func Option(value, text string, selected bool) render.HTML {
	attrs := Attrs{"value": value}
	if selected {
		attrs["selected"] = "selected"
	}
	return render.Tag("option", attrs, render.Text(text))
}

// TextArea produces a <textarea> element.
// Required: Name.
func TextArea(cfg TextAreaConfig) render.HTML {
	if cfg.Name == "" {
		panic("html: TextArea requires Name")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "name", cfg.Name)
	return render.Tag("textarea", attrs)
}

// ButtonGroup produces a <div> with role="group" containing buttons.
func ButtonGroup(cfg ButtonGroupConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", "group")
	if cfg.AriaLabel != "" {
		setAttr(attrs, "aria-label", cfg.AriaLabel)
	}
	return render.Tag("div", attrs, children...)
}

// FieldSet produces a <fieldset> element with a <legend> derived from
// cfg.Legend.
// Required: Legend.
func FieldSet(cfg FieldSetConfig, children ...render.HTML) render.HTML {
	if cfg.Legend == "" {
		panic("html: FieldSet requires Legend")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	legendHTML := Legend(TextConfig{}, render.Text(cfg.Legend))
	all := append([]render.HTML{legendHTML}, children...)
	return render.Tag("fieldset", attrs, all...)
}

// Legend produces a <legend> element.
func Legend(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("legend", attrs, children...)
}

// CheckboxConfig configures a void <input type="checkbox"> element.
// Required: Name.
type CheckboxConfig struct {
	Name    string // required
	Value   string // optional
	ID      string // optional (but strongly recommended for label association)
	Checked bool   // optional
	Class   string
	Attrs   Attrs
}

// Checkbox produces an <input type="checkbox"> element.
// Required: Name.
func Checkbox(cfg CheckboxConfig) render.HTML {
	if cfg.Name == "" {
		panic("html: Checkbox requires Name")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "type", "checkbox")
	setAttr(attrs, "name", cfg.Name)
	if cfg.Value != "" {
		setAttr(attrs, "value", cfg.Value)
	}
	if cfg.Checked {
		setAttr(attrs, "checked", "checked")
	}
	return render.VoidTag("input", attrs)
}

// RadioConfig configures a void <input type="radio"> element.
// Required: Name and Value.
type RadioConfig struct {
	Name    string // required
	Value   string // required
	ID      string // optional (but strongly recommended)
	Checked bool   // optional
	Class   string
	Attrs   Attrs
}

// Radio produces an <input type="radio"> element.
// Required: Name and Value.
func Radio(cfg RadioConfig) render.HTML {
	if cfg.Name == "" {
		panic("html: Radio requires Name")
	}
	if cfg.Value == "" {
		panic("html: Radio requires Value")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "type", "radio")
	setAttr(attrs, "name", cfg.Name)
	setAttr(attrs, "value", cfg.Value)
	if cfg.Checked {
		setAttr(attrs, "checked", "checked")
	}
	return render.VoidTag("input", attrs)
}
