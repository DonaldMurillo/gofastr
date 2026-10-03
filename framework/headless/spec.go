package headless

// The component harness: what each component IS, declared once, in
// data, next to nothing else.
//
// Until now every component was checked by four things written by
// hand and separately: a contract test, a stylesheet test, a golden,
// and a specimen in a gallery page. Four places to remember, so a component
// added in a hurry got one or two of them, and the ones it got were
// the ones whose absence nobody notices — a missing golden is silent.
//
// A Spec is the fixture, once. It says what the component is called,
// which parts it draws, which of those a caller may fill, and what
// runtime hooks it publishes; and it renders itself at any class map, so
// the SAME fixture drives the nil-Classes contract sweep, the goldens,
// and the parts tests below. A component with a spec cannot be half
// tested, and one without a spec fails the build.
//
// What a Spec deliberately does not carry is the component's own
// assertions — that a field wires its hint to its control, that a
// pager says which page is current. Those are specific, they read as
// prose, and they belong in the contract test where a reviewer will
// find them.
// The harness is for what must hold for EVERYTHING.

import (
	"sort"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Case is one rendering of a component, with a reason for existing.
// The reason is not documentation: a case with nothing to show is a
// case that will be updated to match whatever the code does next.
type Case struct {
	Name string
	Why  string
	HTML render.HTML
}

// Spec describes one component and how to render it.
type Spec struct {
	// Name is the exported function's name, exactly. The coverage
	// gate matches on it.
	Name string
	// Anatomy lists the parts this component draws. A class map styles these
	// and only these; a part listed here and never rendered is a
	// class in the stylesheet with nothing to land on.
	Anatomy []Part
	// Fillable are the parts a caller may replace through Slots. The
	// empty set is the correct answer for most components: a slot is
	// offered where the component's own content carries no guarantee.
	Fillable []Part
	// Hooks are the data-hui-* attributes this component publishes for
	// the runtime. Naming them here is what lets a test prove the
	// runtime is not bound to an attribute nothing renders.
	Hooks []string
	// WithParts renders the component with a caller's Parts applied.
	// A component that offers its parts must provide it: it is how the
	// harness proves that filling a slot or adding an attribute cannot
	// break the contract, and a part nothing tests is a part that will.
	WithParts func(s Classes, parts Parts) render.HTML
	// Cases renders the component at a given Classes. A nil Classes means
	// unstyled, which is what the contract is asserted against.
	Cases func(k Kit) []Case
}

// Kit is what a fixture is handed: this component's class map, and a way to
// reach any other component's.
//
// It exists because a fixture composes. A Form fixture needs a Button
// and an Input inside it, and before this existed there was only one
// class map in scope — the Form's — so every fixture either passed the
// parent's class map to the child, which dresses an <input> in .ds-form and
// leaves it otherwise naked, or gave up and wrote the child as a raw
// HTML string, which no part check, no golden and no audit can see.
// Thirty-one components did one or the other.
//
// The lookup is a function rather than a map because the class maps live in
// the styled layer, which imports this one. Inverting that would put
// class names in the headless half, and the whole split is that they
// are not there.
type Kit struct {
	// Classes is this component's own classes.
	Classes Classes
	// of resolves another component's class map by name and variant. Nil
	// means every lookup is unstyled, which is what the contract
	// suite wants and what a caller who supplies nothing gets.
	of func(component, variant string) Classes
}

// For is the class map a child component should wear. The name is the
// component's, exactly as it is registered.
func (k Kit) For(component string) Classes { return k.Variant(component, "") }

// Variant is For, for a component whose class map depends on a tone or
// kind: an alert is danger or info, a badge neutral or warning. The
// catalogue drew every alert in the same blue until this existed,
// because one component mapped to one class map and a tone could not be
// asked for.
func (k Kit) Variant(component, variant string) Classes {
	if k.of == nil {
		return nil
	}
	return k.of(component, variant)
}

// NewKit builds a Kit with a resolver. The package that owns the class maps calls this to
// render the fixtures dressed; the contract suite passes nil and gets
// an unstyled system.
func NewKit(own Classes, of func(component, variant string) Classes) Kit {
	return Kit{Classes: own, of: of}
}

var registry = map[string]Spec{}

// Register adds a component to the harness. Called from each
// component's own file, so the fixture lives beside the thing it
// describes and moves when it moves.
func Register(sp Spec) {
	if sp.Name == "" {
		panic("headless: a spec with no name")
	}
	if _, dup := registry[sp.Name]; dup {
		panic("headless: two specs named " + sp.Name)
	}
	registry[sp.Name] = sp
}

// Specs returns every registered spec, in name order so that anything
// built from them — a test's output, a gallery page — is stable.
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
