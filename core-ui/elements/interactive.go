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

// Button produces a <button> element with type="button" and aria-label
// set from label when provided.
func Button(label string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	if _, ok := attrs["type"]; !ok {
		attrs["type"] = "button"
	}
	if label != "" {
		if _, ok := attrs["aria-label"]; !ok {
			attrs["aria-label"] = label
		}
	}
	return render.Tag("button", attrs, render.Text(label))
}

// Link produces an <a> element with the given href and text content.
func Link(href, text string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs["href"] = href
	return render.Tag("a", attrs, render.Text(text))
}

// Form produces a <form> element with method and action attributes.
func Form(method, action string, attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	attrs["method"] = method
	if action != "" {
		attrs["action"] = action
	}
	return render.Tag("form", attrs, children...)
}

// Input produces a void <input> element with type and name attributes.
func Input(inputType, name string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	attrs["type"] = inputType
	attrs["name"] = name
	return render.VoidTag("input", attrs)
}

// Label produces a <label> element with a for attribute linking it to
// the form control with the given ID.
func Label(forID, text string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs["for"] = forID
	return render.Tag("label", attrs, render.Text(text))
}

// Select produces a <select> element containing the given options.
func Select(name string, options []SelectOption, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs["name"] = name

	children := make([]render.HTML, len(options))
	for i, opt := range options {
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

// TextArea produces a <textarea> element with the given name.
func TextArea(name string, attrs Attrs) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs["name"] = name
	return render.Tag("textarea", attrs)
}

// ButtonGroup produces a <div> with role="group" containing buttons.
// This provides an accessible grouping for related buttons.
func ButtonGroup(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = "group"
	}
	return render.Tag("div", attrs, children...)
}

// FieldSet produces a <fieldset> element with a <legend> derived from
// the legend parameter.
func FieldSet(legend string, attrs Attrs, children ...render.HTML) render.HTML {
	legendHTML := Legend(nil, render.Text(legend))
	all := append([]render.HTML{legendHTML}, children...)
	return render.Tag("fieldset", attrs, all...)
}

// Legend produces a <legend> element.
func Legend(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("legend", attrs, children...)
}
