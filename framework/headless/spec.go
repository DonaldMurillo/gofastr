package headless

import (
	"sort"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Case is one rendering of a component and the reason it exists.
type Case struct {
	Name string
	Why  string
	HTML render.HTML
}

// Spec declares one component to the harness: its parts, hooks and fixtures.
type Spec struct {
	// Name is the exported component function's name, exactly.
	Name string
	// Anatomy lists every part the component draws.
	Anatomy []Part
	// Fillable lists the parts a Slot or a text or html Bind may replace.
	Fillable []Part
	// Hooks lists the data-hui-* attributes the component renders.
	Hooks []string
	// WithParts renders the component with a caller's Parts; required when it offers them.
	WithParts func(s Classes, parts Parts) render.HTML
	// Cases renders the fixtures; a nil Classes renders them unstyled.
	Cases func(k Kit) []Case
}

// Kit is a fixture's own class map and a resolver for the class maps of the
// components it composes.
type Kit struct {
	Classes Classes
	of      func(component, variant string) Classes
}

// For returns the class map of the named component.
func (k Kit) For(component string) Classes { return k.Variant(component, "") }

// Variant returns the named component's class map for a tone or kind; nil
// without a resolver.
func (k Kit) Variant(component, variant string) Classes {
	if k.of == nil {
		return nil
	}
	return k.of(component, variant)
}

// NewKit builds a Kit; a nil resolver dresses every child unstyled.
func NewKit(own Classes, of func(component, variant string) Classes) Kit {
	return Kit{Classes: own, of: of}
}

var registry = map[string]Spec{}

// Register adds a spec to the harness, panicking on an empty or repeated name.
func Register(sp Spec) {
	if sp.Name == "" {
		panic("headless: a spec with no name")
	}
	if _, dup := registry[sp.Name]; dup {
		panic("headless: two specs named " + sp.Name)
	}
	registry[sp.Name] = sp
}

// Specs returns every registered spec in name order.
func Specs() []Spec {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Spec, 0, len(names))
	for _, n := range names {
		out = append(out, registry[n])
	}
	return out
}

// SpecOf returns one spec by component name.
func SpecOf(name string) (Spec, bool) {
	sp, ok := registry[name]
	return sp, ok
}
