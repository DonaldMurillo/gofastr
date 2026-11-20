package html

import (
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// Attrs is a type alias for map[string]string, matching [render.Attrs].
type Attrs = map[string]string

// MergeAttrs merges zero or more attribute maps.
func MergeAttrs(attrsList ...Attrs) Attrs {
	if len(attrsList) == 0 {
		return nil
	}
	merged := make(Attrs)
	for _, a := range attrsList {
		maps.Copy(merged, a)
	}
	return merged
}

// Classes converts a map of class names to booleans into an Attrs containing a single "class"
// attribute.
func Classes(classes map[string]bool) Attrs {
	var active []string
	for _, cls := range slices.Sorted(maps.Keys(classes)) {
		include := classes[cls]
		if include {
			active = append(active, cls)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return Attrs{"class": strings.Join(active, " ")}
}

// SafeExtraAttrs returns a copy of a component config's ExtraAttrs with the keys the component owns
// removed, ready to forward to the component's root element.
func SafeExtraAttrs(attrs Attrs, protected ...string) Attrs {
	if len(attrs) == 0 {
		return nil
	}
	out := make(Attrs, len(attrs))
	for k, v := range attrs {
		if strings.EqualFold(k, "class") || strings.EqualFold(k, "id") {
			continue
		}
		//gofastr:allow(layerprefix) the refusal filter names the framework prefix to bar it from extra attrs
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "data-cui-") || strings.HasPrefix(lk, "data-fui-") {
			continue
		}
		drop := false
		for _, p := range protected {
			if strings.EqualFold(k, p) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SafeCarrierAttrs is SafeExtraAttrs for wiring carriers: components whose ExtraAttrs are the
// documented attachment point for runtime wiring (interactive.Action.Attrs(), data-cui-open, …) and
// that emit no data-cui-* wiring of their own.
func SafeCarrierAttrs(attrs Attrs, protected ...string) Attrs {
	if len(attrs) == 0 {
		return nil
	}
	out := make(Attrs, len(attrs))
	for k, v := range attrs {
		if strings.EqualFold(k, "class") || strings.EqualFold(k, "id") ||
			strings.EqualFold(k, "data-cui-comp") {
			continue
		}
		drop := false
		for _, p := range protected {
			if strings.EqualFold(k, p) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DataAttrs converts a map of key-value pairs into data-* attributes.
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
func Aria(key, value string) Attrs {
	return Attrs{"aria-" + key: value}
}

// OnClick returns Attrs with data-action set to the given action name.
func OnClick(action string) Attrs {
	return Attrs{"data-action": action}
}

// OnSubmit returns Attrs with data-action set and data-action-type="submit".
func OnSubmit(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "submit"}
}

// OnInput returns Attrs with data-action set and data-action-type="input".
func OnInput(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "input"}
}

// OnChange returns Attrs with data-action set and data-action-type="change".
func OnChange(action string) Attrs {
	return Attrs{"data-action": action, "data-action-type": "change"}
}

// Bind returns Attrs that create a two-way binding between an input element and a named state key.
func Bind(key string) Attrs {
	return Attrs{"data-bind": key}
}

func buildAttrs(base Attrs, id, class string) Attrs {
	attrs := make(Attrs)
	maps.Copy(attrs, base)
	if id != "" {
		attrs["id"] = id
	}
	if class != "" {
		attrs["class"] = class
	}
	return attrs
}

func setAttr(attrs Attrs, key, value string) Attrs {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	attrs[key] = value
	return attrs
}

func setURLAttr(attrs Attrs, key, value string, policy urlsafe.Policy) Attrs {
	if !urlsafe.OK(value, policy) {
		return attrs
	}
	return setAttr(attrs, key, value)
}
