package elements

import (
	"github.com/gofastr/gofastr/core/render"
)

// SelectOption represents a single <option> within a <select> element.
type SelectOption struct {
	Value    string
	Text     string
	Selected bool
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
		panic("elements: Link requires Href")
	}
	if cfg.Text == "" {
		panic("elements: Link requires Text")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "href", cfg.Href)
	return render.Tag("a", attrs, render.Text(cfg.Text))
}

// LinkHTML produces an <a> element with raw HTML content (not escaped).
// Required: Href and Content.
func LinkHTML(cfg LinkHTMLConfig) render.HTML {
	if cfg.Href == "" {
		panic("elements: LinkHTML requires Href")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "href", cfg.Href)
	return render.Tag("a", attrs, cfg.Content)
}

// Form produces a <form> element.
// Required: Method.
func Form(cfg FormConfig, children ...render.HTML) render.HTML {
	if cfg.Method == "" {
		panic("elements: Form requires Method")
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
		panic("elements: Input requires Type")
	}
	if cfg.Name == "" {
		panic("elements: Input requires Name")
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
		panic("elements: Label requires For")
	}
	if cfg.Text == "" {
		panic("elements: Label requires Text")
	}
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "for", cfg.For)
	return render.Tag("label", attrs, render.Text(cfg.Text))
}

// Select produces a <select> element containing the given options.
// Required: Name.
func Select(cfg SelectConfig) render.HTML {
	if cfg.Name == "" {
		panic("elements: Select requires Name")
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
		panic("elements: TextArea requires Name")
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
		panic("elements: FieldSet requires Legend")
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
