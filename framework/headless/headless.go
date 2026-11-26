// Package headless renders the structure and accessibility half of a
// design system: tags, roles, labelling, state attributes and data-hui-*
// hooks, with no classes and no CSS. A component is a function from its
// props and a Classes to HTML, and a nil Classes renders no class attribute.
package headless

import (
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Part names an element inside a component, the unit a class map styles.
type Part string

// The parts shared across components.
const (
	PartRoot    Part = "root"
	PartLabel   Part = "label"
	PartControl Part = "control"
	PartHint    Part = "hint"
	PartError   Part = "error"
	PartIcon    Part = "icon"
	PartText    Part = "text"
	PartFooter  Part = "footer"
	PartHeader  Part = "header"
	PartTitle   Part = "title"
	PartDesc    Part = "desc"
	PartBody    Part = "body"
	PartMarker  Part = "marker"
	PartActions Part = "actions"
	PartStatus  Part = "status"
	// PartVisuallyHidden is text read aloud and clipped from view, never display:none.
	PartVisuallyHidden Part = "visually-hidden"
	// PartDefaultAction is a form's hidden default submit, before every other submit control.
	PartDefaultAction Part = "default-action"
)

// Classes maps parts to class names. Nil is valid and renders unstyled.
type Classes map[Part]string

// Class returns the class for a part, or "" when the class map has none.
func (s Classes) Class(p Part) string { return s[p] }

// Variant returns the class for "<part>--<variant>", or "" for an empty variant.
func (s Classes) Variant(p Part, variant string) string {
	if variant == "" {
		return ""
	}
	return s[Part(string(p)+"--"+variant)]
}

// El renders one element carrying the part's class joined to own's class.
func El(tag string, s Classes, p Part, own html.Attrs, children ...render.HTML) render.HTML {
	attrs := html.Attrs{}
	for k, v := range own {
		attrs[k] = v
	}
	if cls := joinClasses(s.Class(p), attrs["class"]); cls != "" {
		attrs["class"] = cls
	}
	if isVoid(tag) {
		return render.VoidTag(tag, attrs)
	}
	return render.Tag(tag, attrs, children...)
}

// Attrs copies pairs in key order, dropping empty values; set a presence
// attribute with Mark or Flag.
func Attrs(pairs map[string]string) html.Attrs {
	out := html.Attrs{}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if pairs[k] != "" {
			out[k] = pairs[k]
		}
	}
	return out
}

// Mark sets each named attribute present with an empty value.
func Mark(a html.Attrs, names ...string) html.Attrs {
	if a == nil {
		a = html.Attrs{}
	}
	for _, n := range names {
		a[n] = ""
	}
	return a
}

// Internal returns a copy of own with data-cui-internal set, the mark an
// owned style's @scope stops at.
func Internal(own html.Attrs) html.Attrs {
	out := make(html.Attrs, len(own)+1)
	for k, v := range own {
		out[k] = v
	}
	out["data-cui-internal"] = ""
	return out
}

// Flag sets a boolean attribute when on.
func Flag(a html.Attrs, name string, on bool) html.Attrs {
	if on {
		if a == nil {
			a = html.Attrs{}
		}
		a[name] = ""
	}
	return a
}

func refused(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "style", "data-behavior", "data-island", "data-widget", "data-component", "data-bind", "data-action":
		return true
	}
	if strings.HasPrefix(k, "on") {
		return true
	}
	//gofastr:allow(layerprefix) Safe names every reserved prefix to refuse it from ExtraAttrs
	for _, prefix := range []string{"data-hui-", "data-cui-", "data-fui-", "data-action-", "data-param-", "data-kiln-"} {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// Safe copies caller extras with keys lowercased, dropping class, id, the
// owned keys and every refused key; one key under two spellings panics.
func Safe(extra html.Attrs, owned ...string) html.Attrs {
	blocked := map[string]bool{"class": true, "id": true}
	for _, k := range owned {
		blocked[strings.ToLower(k)] = true
	}
	out := html.Attrs{}
	for k, v := range extra {
		key := strings.ToLower(k)
		if blocked[key] || refused(key) {
			continue
		}
		if _, twice := out[key]; twice {
			panic("headless: extra attrs repeat " + key + " under two spellings")
		}
		out[key] = v
	}
	return out
}

// Merge returns a copy of a with b written over it.
func Merge(a, b html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// Describe returns an input's aria-describedby value, the error id before
// the hint id, and the two ids; an empty id yields nothing.
func Describe(id, hint, errText string) (describedBy, hintID, errID string) {
	if id == "" {
		return "", "", ""
	}
	var ids []string
	if errText != "" {
		errID = id + "-error"
		ids = append(ids, errID)
	}
	if hint != "" {
		hintID = id + "-hint"
		ids = append(ids, hintID)
	}
	return strings.Join(ids, " "), hintID, errID
}

func joinClasses(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func isVoid(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr":
		return true
	}
	return false
}
