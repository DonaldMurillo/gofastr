package elements

import (
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// Attrs is a type alias for map[string]string, matching [render.Attrs].
type Attrs = map[string]string

// MergeAttrs merges zero or more attribute maps. Later maps overwrite
// earlier ones for the same key. Returns nil if no maps are provided.
func MergeAttrs(attrsList ...Attrs) Attrs {
	if len(attrsList) == 0 {
		return nil
	}
	merged := make(Attrs)
	for _, a := range attrsList {
		for k, v := range a {
			merged[k] = v
		}
	}
	return merged
}

// Classes converts a map of class names to booleans into an Attrs containing
// a single "class" attribute. Only classes with a true value are included.
func Classes(classes map[string]bool) Attrs {
	var active []string
	for cls, include := range classes {
		if include {
			active = append(active, cls)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return Attrs{"class": strings.Join(active, " ")}
}

// DataAttrs converts a map of key-value pairs into data-* attributes.
// Keys should not include the "data-" prefix; it is added automatically.
func DataAttrs(data map[string]string) Attrs {
	if len(data) == 0 {
		return nil
	}
	attrs := make(Attrs, len(data))
	for k, v := range data {
		attrs["data-"+k] = v
	}
	return attrs
}

// ID returns an Attrs containing only the given id attribute.
func ID(id string) Attrs {
	return Attrs{"id": id}
}

// Aria returns an Attrs containing a single aria-* attribute.
// The key should not include the "aria-" prefix; it is added automatically.
func Aria(key, value string) Attrs {
	return Attrs{"aria-" + key: value}
}

// OnClick returns Attrs with data-action set to the given action name.
// Used to wire up click handlers on elements.
// Example: Button("Save", OnClick("save"))
func OnClick(action string) Attrs {
	return Attrs{"data-action": action}
}

// OnSubmit returns Attrs with data-action set and data-action-type="submit".
// Used on form elements.
func OnSubmit(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "submit"}
}

// OnInput returns Attrs with data-action set and data-action-type="input".
// Used on input/textarea elements for real-time input handling.
func OnInput(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "input"}
}

// OnChange returns Attrs with data-action set and data-action-type="change".
// Used on select/input elements for change handling.
func OnChange(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "change"}
}

// Bind returns Attrs that create a two-way binding between an input element
// and a named state key. The runtime.js listens for input events on elements
// with data-bind and dispatches actions with the new value.
//
// Usage:
//
//	search := signal.New("")
//	elements.Input(elements.Text, "search",
//	    elements.Bind("search"),
//	    elements.Placeholder("Search..."),
//	)
func Bind(key string) Attrs {
	return Attrs{"data-bind": key}
}

// ContainerType returns Attrs that declare an element as a CSS container query context.
// The containerType is typically "inline-size" (respond to width) or "size" (width + height).
// Use this on parent elements whose children should respond to the parent's size.
//
//	elements.Div(
//		elements.ContainerType("inline-size", "product-grid"),
//		productCards...,
//	)
func ContainerType(containerType string, name string) Attrs {
	return Attrs{
		"container-type": containerType,
		"container-name": name,
	}
}

// setAttr is a helper that sets a key in the attrs map, creating it if nil.
func setAttr(attrs Attrs, key, value string) Attrs {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs[key] = value
	return attrs
}

// renderChildren is a helper that joins children into a single HTML fragment.
func renderChildren(children []render.HTML) render.HTML {
	return render.Join(children...)
}
