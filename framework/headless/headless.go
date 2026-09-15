// Package headless is the structure and accessibility half of a design
// system: the tags, the roles, the labelling relationships, the state
// attributes and the hooks a runtime binds to. It renders no classes
// and ships no CSS.
//
// The split exists because structure and styling have different
// lifetimes and different reviewers. Whether a password field's reveal
// button announces what it will do next, whether a field's error is
// tied to its input by aria-describedby, whether a pager says which
// page is current — none of that changes when the palette does, and
// all of it is testable without rendering a pixel (a11y_test.go,
// harness_test.go). A skin is then free to be redrawn, or replaced
// entirely, without putting a single accessibility guarantee back at
// risk.
//
// A component here is a pure function from its props and a Skin to
// HTML. The Skin decides what class each named Part carries; a nil
// Skin renders the same markup with no classes at all, which is what
// "headless" means and what the goldens pin. Seven things are named in
// a component's contract, and the harness checks each: its Parts, its
// runtime hooks (data-hui-*), and the seams a caller reaches in through
// — Slots, Overrides, Binds, Words, and for a component that changes
// in-page state, an Island. See box.go, words.go and island.go.
//
// The package is SSR-first and hydrates incrementally, the same model
// as the rest of the framework (core-ui/ARCHITECTURE.md): first paint
// is the full markup, behaviour is armed by hook on arrival, and a
// state change is an island RPC on the element that keeps its href or
// action for a reader with no script. Its dependencies are
// core-ui/html, core/render and core-ui/interactive (the signal
// attribute allow-list a Bind is checked against), plus the agents
// inventory registration every framework subpackage carries.
//
// No runtime module binds the data-hui-* hooks in this repository yet,
// and no skin dresses the parts: the hooks are the contract that
// module will be written to, and every component renders markup that
// is correct and usable without it. framework/ui is today's styled
// layer and does not render through this package; the skin, the
// stylesheet, the runtime module and that adoption follow in their
// own changes.
package headless

import (
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Part names an element inside a component. A skin styles parts; the
// structure names them. Adding a part is a change to both layers, which
// is the point: a skin cannot invent a hook the markup does not offer,
// and the markup cannot quietly drop one a skin is using.
type Part string

// The shared vocabulary. Component-specific parts live beside their
// component.
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
	// PartVisuallyHidden is text that must be read and must not be
	// seen. It exists as a shared part because more than one component
	// needs it and every one of them must hide it the same way —
	// clipped, never display: none, which would take it out of the
	// accessibility tree along with the view.
	PartVisuallyHidden Part = "visually-hidden"
)

// Skin maps parts to class names. Nil is valid and renders unstyled.
type Skin map[Part]string

// Class returns the class for a part, or "" when the skin has none.
func (s Skin) Class(p Part) string { return s[p] }

// Variant returns the class a skin uses for a named variant of a part,
// looked up as "<part>--<variant>". Empty when unstyled or unknown.
func (s Skin) Variant(p Part, variant string) string {
	if variant == "" {
		return ""
	}
	return s[Part(string(p)+"--"+variant)]
}

// El builds one element: the part's class, then the caller's attrs,
// then children. Attrs the component owns always win over ExtraAttrs,
// which is why they are passed separately.
func El(tag string, s Skin, p Part, own html.Attrs, children ...render.HTML) render.HTML {
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

// Attrs is a small builder: it drops empty values, so a component can
// declare every attribute it might set in one place and let the zero
// value mean "absent" rather than "present and empty".
//
// Boolean attributes are the exception — an empty string IS the value
// for disabled, required and friends — so those go through Flag.
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

// Mark sets attributes whose PRESENCE is the value: the data-hui-*
// hooks a runtime binds to, and the HTML attributes that work the same
// way — hidden, popover, open, inert.
//
// It exists because Attrs drops empty values, which is right for
// "absent means unset" and catastrophic here: the component renders
// with every option set and the attribute missing, so it is styled,
// labelled, announced, and either wired to nothing or visible when it
// should not be. That shipped five times in the package this one grew
// from — a popover attribute, a copy hook, a drop list, repeater rows,
// and hidden on an empty message — every one of them built through
// Attrs, and the fifth AFTER this helper existed, because the helper
// was thought of as being for hooks. It is not: it is for any
// attribute whose empty string is meaningful.
func Mark(a html.Attrs, names ...string) html.Attrs {
	for _, n := range names {
		a[n] = ""
	}
	return a
}

// Flag sets a boolean attribute when on.
func Flag(a html.Attrs, name string, on bool) html.Attrs {
	if on {
		a[name] = ""
	}
	return a
}

// refused reports whether a caller-supplied attribute may never reach
// the markup, whatever seam it came through. Attribute names are
// case-insensitive in HTML — the parser lowercases them, so
// DATA-FUI-RPC is data-fui-rpc by the time the runtime looks — which
// is why the key is folded before every check, and why a sanitiser
// that compared the spelling as written let the request through.
//
// Refused, in three families: style, which a host serving no
// unsafe-inline drops (the framework's default posture), so it is a
// rule that vanishes in production; every data-fui-* key, the
// framework runtime's own contract, so decoration can never become a
// request; and the runtime's privileged unprefixed keys — data-behavior
// (a script-loading sink), data-island (the SSE swap target),
// data-widget and data-component (island roots), data-bind (two-way
// state), data-action and data-param-* (a compiled server action and
// its arguments) and data-kiln-* (the legacy tool delegators). The
// runtime re-checks some of their values; the seam's job is that they
// never arrive. And every data-hui-* key, this package's own hooks: a
// forged one binds behaviour to an element that was never built for
// it. A component sets its own hooks after the seam, so nothing it
// renders is refused here.
func refused(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "style", "data-behavior", "data-island", "data-widget", "data-component", "data-bind", "data-action":
		return true
	}
	for _, prefix := range []string{"data-hui-", "data-fui-", "data-action-", "data-param-", "data-kiln-"} {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// Safe copies caller-supplied extras, dropping the keys a component
// owns so no caller can break its structure or its labelling, and the
// keys refused on every seam (see refused). Keys are stored folded,
// the way the browser reads them: NAME and name are one attribute, and
// stored as written a caller's NAME sorted ahead of the component's
// name, so the browser kept the caller's. One key under two spellings
// is refused rather than left to map order.
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

// Merge folds b into a, b winning. Used to layer owned attrs over
// caller extras.
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

// Describe wires an input to its hint and error by id, returning the
// aria-describedby value. This is the whole reason a field is a
// component and not three elements in a row: the relationship has to
// be built from the same ids the elements are given, in one place, or
// it silently rots.
func Describe(id, hint, errText string) (describedBy, hintID, errID string) {
	if id == "" {
		return "", "", ""
	}
	var ids []string
	if errText != "" {
		errID = id + "-error"
		ids = append(ids, errID)
	} else if hint != "" {
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
